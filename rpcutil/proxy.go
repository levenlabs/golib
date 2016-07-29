package rpcutil

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"strconv"

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
type HTTPProxy struct{}

type errWriter struct {
	errCh chan error
}

func (e errWriter) Write(p []byte) (n int, err error) {
	select {
	case e.errCh <- errors.New(string(p)):
	default:
	}
	return len(p), nil
}

func (h *HTTPProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	errCh := h.ServeHTTPCtx(context.Background(), w, r)
	for range errCh {
	}
}
	}
}

// ServeHTTPCtx will do the proxying of the given request and write the response
// to the ResponseWriter. ctx can be used to cancel the request mid-way.
func (h *HTTPProxy) ServeHTTPCtx(ctx context.Context, w http.ResponseWriter, r *http.Request) <-chan error {
	// We only do the cancellation logic if the Cancel channel hasn't been set
	// on the Request already. If it has, then some other process is liable to
	// close it also, which would cause a panic
	doneCh := make(chan struct{})
	if r.Cancel == nil {
		cancelCh := make(chan struct{})
		r.Cancel = cancelCh
		go func() {
			select {
			case <-ctx.Done():
			case <-doneCh:
			}
			close(cancelCh)
		}()
	}
	AddProxyXForwardedFor(r, r)

	errCh := make(chan error)
	ew := errWriter{errCh}
	go func() {
		rp := &httputil.ReverseProxy{
			Director: func(r *http.Request) {},
			ErrorLog: log.New(ew, "", 0),
		}
		rp.ServeHTTP(w, r)
		close(errCh)
		close(doneCh) // prevent the cancel go-routine from never ending
	}()
	return errCh
}

// BufferedResponseWriter is a wrapper around a real ResponseWriter which
// actually writes all data to a buffer instead of the ResponseWriter. It also catches calls
// to WriteHeader. Once writing is done, GetBody can be called to get the
// buffered body, and SetBody can be called to set a new body to be used.  When
// ActuallyWrite is called all headers will be written to the ResponseWriter
// (with corrected Content-Length if Buffer was changed), followed by the body.
//
// BufferedResponseWriter will transparently handle un-gzipping and re-gzipping
// the response body when it sees Content-Encoding: gzip.
type BufferedResponseWriter struct {
	http.ResponseWriter
	buffer  *bytes.Buffer
	newBody io.Reader

	code int
}

// NewBufferedResponseWriter returns an initialized BufferedResponseWriter,
// which will catch writes going to the given ResponseWriter
func NewBufferedResponseWriter(rw http.ResponseWriter) *BufferedResponseWriter {
	return &BufferedResponseWriter{
		ResponseWriter: rw,
		buffer:         new(bytes.Buffer),
	}
}

// WriteHeader catches the call to WriteHeader and doesn't actually do anything,
// except store the given code for later use when ActuallyWrite is called
func (brw *BufferedResponseWriter) WriteHeader(code int) {
	brw.code = code
}

func (brw *BufferedResponseWriter) Write(b []byte) (int, error) {
	return brw.buffer.Write(b)
}

// GetBody returns a buffer containing the body of the request which was
// buffered. If the response was compressed the body will be transparently
// decompressed. The contents of the returned buffer should *not* be modified.
func (brw *BufferedResponseWriter) GetBody() (*bytes.Buffer, error) {
	body := brw.buffer
	// TODO we need to support deflate and sdch
	if brw.Header().Get("Content-Encoding") == "gzip" {
		gzR, err := gzip.NewReader(brw.buffer)
		if err != nil {
			return nil, err
		}
		defer gzR.Close()
		gzBuf := bytes.NewBuffer(make([]byte, 0, brw.buffer.Len()))
		if _, err := io.Copy(gzBuf, gzR); err != nil {
			return nil, err
		}
		body = gzBuf
	}
	return body, nil
}

// SetBody sets the io.Reader from which the new body of the buffered response
// should be read from. This io.Reader will be drained once ActuallyWrite is
// called. If this is never called the original body will be used.
//
// Note: there is no need to send in compressed data here, the new response body
// will be automatically compressed in the same way the original was.
func (brw *BufferedResponseWriter) SetBody(in io.Reader) {
	brw.newBody = in
}

func (brw *BufferedResponseWriter) bodyBuf() (*bytes.Buffer, error) {
	if brw.newBody == nil {
		return brw.buffer, nil
	}

	bodyBuf := bytes.NewBuffer(make([]byte, 0, brw.buffer.Len()))
	var dst io.Writer = bodyBuf

	if brw.Header().Get("Content-Encoding") == "gzip" {
		gzW := gzip.NewWriter(bodyBuf)
		defer gzW.Close()
		dst = gzW

	} else if bodyBuf2, ok := brw.newBody.(*bytes.Buffer); ok {
		// shortcut, we don't actually have to do anything if the body we've
		// been given is already a buffer and we don't have to gzip it
		return bodyBuf2, nil
	}

	// dst will be some wrapper around bodyBuf, or bodyBuf itself
	if _, err := io.Copy(dst, brw.newBody); err != nil {
		return nil, err
	}
	return bodyBuf, nil
}

// ActuallyWrite takes all the buffered data and actually writes to the wrapped
// ResponseWriter. Returns the number of bytes written as the body
func (brw *BufferedResponseWriter) ActuallyWrite() (int64, error) {
	bodyBuf, err := brw.bodyBuf()
	if err != nil {
		return 0, err
	}

	rw := brw.ResponseWriter
	rw.Header().Set("Content-Length", strconv.Itoa(bodyBuf.Len()))
	rw.WriteHeader(brw.code)
	return io.Copy(rw, bodyBuf)
}
