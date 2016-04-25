package rpcutil

import (
	"bytes"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httputil"
	"sync"
)

// HTTPProxy implements an http reverse proxy. It is obstensibly a simple
// wrapper around httputil.ReverseProxy, except that it handles a number of
// common cases that we generally want
//
// To use, first change the URL on an incoming request to the new destination,
// as well as any other changes to the request which are wished to be made. Then
// call ServeHTTP on the HTTPProxy with that edited request and its
// ResponseWriter.
//
// Features implemented:
// * Disable built-in httputil.ReverseProxy logger
// * Automatically adding X-Forwarded-For
type HTTPProxy struct {
	once sync.Once
	rp   *httputil.ReverseProxy
}

func (h *HTTPProxy) init() {
	h.once.Do(func() {
		h.rp = &httputil.ReverseProxy{
			Director: func(r *http.Request) {},
			// This is unfortunately the only way to keep the proxy from using
			// its own log format
			ErrorLog: log.New(ioutil.Discard, "", 0),
		}
	})
}

func (h *HTTPProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.init()
	AddProxyXForwardedFor(r, r)
	h.rp.ServeHTTP(w, r)
}

// BufferedResponseWriter is a wrapper around a real ResponseWriter which
// actually writes all data to the buffer in the struct.
type BufferedResponseWriter struct {
	http.ResponseWriter
	Buffer *bytes.Buffer
}

func (brw BufferedResponseWriter) Write(b []byte) (int, error) {
	return brw.Buffer.Write(b)
}
