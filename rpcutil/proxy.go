package rpcutil

import (
	"bytes"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httputil"
	"strconv"
	"sync"

	"golang.org/x/net/context"
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
	h.ServeHTTPCtx(context.Background(), w, r)
}

// ServeHTTPCtx will do the proxying of the given request and write the response
// to the ResponseWriter. ctx can be used to cancel the request mid-way.
func (h *HTTPProxy) ServeHTTPCtx(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	h.init()
	// We only do the cancellation logic if the Cancel channel hasn't been set
	// on the Request already. If it has, then some other process is liable to
	// close it also, which would cause a panic
	if r.Cancel == nil {
		cancelCh := make(chan struct{})
		r.Cancel = cancelCh
		doneCh := make(chan struct{})
		defer close(doneCh) // so no matter what the go-routine exits
		go func() {
			select {
			case <-ctx.Done():
			case <-doneCh:
			}
			close(cancelCh)
		}()
	}
	AddProxyXForwardedFor(r, r)
	h.rp.ServeHTTP(w, r)
}

// BufferedResponseWriter is a wrapper around a real ResponseWriter which
// actually writes all data to the buffer in the struct. It also catches calls
// to WriteHeader. Once writing is done, the Buffer can be inspected and
// modified. When ActuallyWrite is called all headers will be written to the
// ResponseWriter (with corrected Content-Length if Buffer was changed),
// followed by the Buffer as the body.
type BufferedResponseWriter struct {
	http.ResponseWriter
	Buffer *bytes.Buffer

	code int
}

// NewBufferedResponseWriter returns an initialized BufferedResponseWriter,
// which will catch writes going to the given ResponseWriter
func NewBufferedResponseWriter(rw http.ResponseWriter) *BufferedResponseWriter {
	return &BufferedResponseWriter{
		ResponseWriter: rw,
		Buffer:         new(bytes.Buffer),
	}
}

// WriteHeader catches the call to WriteHeader and doesn't actually do anything,
// except store the given code for later use when ActuallyWrite is called
func (brw *BufferedResponseWriter) WriteHeader(code int) {
	brw.code = code
}

func (brw *BufferedResponseWriter) Write(b []byte) (int, error) {
	return brw.Buffer.Write(b)
}

// ActuallyWrite takes all the buffered data and actually writes to the wrapped
// ResponseWriter. Returns the number of bytes written as the body (essentially
// the length of Buffer)
func (brw *BufferedResponseWriter) ActuallyWrite() (int64, error) {
	rw := brw.ResponseWriter
	rw.Header().Set("Content-Length", strconv.Itoa(brw.Buffer.Len()))

	// only call WriteHeader if it was called on brw
	if brw.code > 0 {
		rw.WriteHeader(brw.code)
	}

	return io.Copy(rw, brw.Buffer)
}
