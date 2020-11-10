package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"

	"github.com/golang/glog"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	gw "github.com/rephus/grpc-gateway-example/template"
	"golang.org/x/net/context"
)

const (
	grpcPort = "10000"
)

var (
	getEndpoint  = flag.String("get", "localhost:"+grpcPort, "endpoint of YourService")
	postEndpoint = flag.String("post", "localhost:"+grpcPort, "endpoint of YourService")

	swaggerDir = flag.String("swagger_dir", "template", "path to the directory which contains swagger definitions")
)

var endpoints = []string{
	"localhost:10000",
	"localhost:10001",
}

func getRandEndpoint(interceptors ...grpc.UnaryClientInterceptor) (*grpc.ClientConn, error) {
	endpoint := endpoints[rand.Intn(len(endpoints))]

	conn, err := grpc.Dial(endpoint, grpc.WithInsecure(), grpc.WithUnaryInterceptor(newRefreshClientConn()))
	if err != nil {
		fmt.Printf("dial:%s error:%s, get rand endpoint continue...", endpoint, err.Error())
		time.Sleep(time.Second)
	}
	fmt.Println("connection:", endpoint)

	return conn, nil
}

func newRefreshClientConn() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		var err error
		cc, err = getRandEndpoint()
		if err != nil {
			return err
		}
		defer cc.Close()
		fmt.Printf("refresh connection:%s", method)
		err = invoker(ctx, method, req, reply, cc, opts...)
		return err
	}
}

func newGateway(ctx context.Context, opts ...runtime.ServeMuxOption) http.Handler {
	mux := runtime.NewServeMux(opts...)
	/*	dialOpts := []grpc.DialOption{grpc.WithInsecure()}
		err := gw.RegisterGreeterHandlerFromEndpoint(ctx, mux, *getEndpoint, dialOpts)
		if err != nil {
			panic(err)
		}
	*/
	conn, err := getRandEndpoint(newRefreshClientConn())
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	err = gw.RegisterGreeterHandler(ctx, mux, conn)
	if err != nil {
		panic(err)
	}

	return mux
}

func serveSwagger(w http.ResponseWriter, r *http.Request) {
	if !strings.HasSuffix(r.URL.Path, ".swagger.json") {
		glog.Errorf("Swagger JSON not Found: %s", r.URL.Path)
		http.NotFound(w, r)
		return
	}

	glog.Infof("Serving %s", r.URL.Path)
	p := strings.TrimPrefix(r.URL.Path, "/swagger/")
	p = path.Join(*swaggerDir, p)
	http.ServeFile(w, r, p)
}

func preflightHandler(w http.ResponseWriter, r *http.Request) {
	headers := []string{"Content-Type", "Accept"}
	w.Header().Set("Access-Control-Allow-Headers", strings.Join(headers, ","))
	methods := []string{"GET", "HEAD", "POST", "PUT", "DELETE"}
	w.Header().Set("Access-Control-Allow-Methods", strings.Join(methods, ","))
	glog.Infof("preflight request for %s", r.URL.Path)
	return
}

// allowCORS allows Cross Origin Resoruce Sharing from any origin.
// Don't do this without consideration in production systems.
func allowCORS(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			if r.Method == "OPTIONS" && r.Header.Get("Access-Control-Request-Method") != "" {
				preflightHandler(w, r)
				return
			}
		}
		h.ServeHTTP(w, r)
	})
}

func GetBodyRaw(c *gin.Context) []byte {
	var bodyBytes []byte
	if c.Request.Body != nil {
		bodyBytes, _ = ioutil.ReadAll(c.Request.Body)
	}
	_ = c.Request.Body.Close()
	c.Request.Body = ioutil.NopCloser(bytes.NewBuffer(bodyBytes))
	return bodyBytes
}

func SetHeader() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Header.Set("Content-Type", "application/json")
		c.Next()
	}
}

func Run(address string) error {
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	rand.Seed(time.Now().UnixNano())

	router := gin.Default()
	router.Use(SetHeader())

	//mux := runtime.NewServeMux()
	//mux := http.NewServeMux()

	//mux.HandleFunc("/swagger/", serveSwagger)
	opts := runtime.WithMarshalerOption("application/json", &runtime.JSONBuiltin{})

	httpHandler := newGateway(ctx, opts)
	//mux.Handle("/", httpHandler)

	router.POST("/relay/*service_name", gin.WrapH(httpHandler))
	return router.Run(address)

	//return http.ListenAndServe(address, allowCORS(mux))

}

func main() {
	flag.Parse()
	defer glog.Flush()

	if err := Run(":32600"); err != nil {
		glog.Fatal(err)
	}
}
