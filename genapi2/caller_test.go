package genapi

import (
	"errors"
	"net/http/httptest"
	"net/url"
	. "testing"

	"golang.org/x/net/context"

	"github.com/levenlabs/lrpc"
	"github.com/levenlabs/lrpc/lrpchttp"
	"github.com/levenlabs/lrpc/lrpchttp/json2"
)

var callerTestHandler = lrpchttp.HTTPHandler(json2.Codec{}, lrpc.ServeMux{}.
	HandleFunc("Echo", func(c lrpc.Call) interface{} {
		var i interface{}
		c.UnmarshalArgs(&i)
		return i
	}).
	HandleFunc("Error", func(lrpc.Call) interface{} {
		return errors.New("some error")
	}),
)

var callerTestServer = httptest.NewServer(callerTestHandler)
var callerTestServerAddr = func() string {
	u, _ := url.Parse(callerTestServer.URL)
	return u.Host
}()

func TestCaller(t *T) {
	r := NewRemoter()
	r.Add("test", callerTestServerAddr)
	c := r.Caller("test")
	ctx := context.Background()
}
