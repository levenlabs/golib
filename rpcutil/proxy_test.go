package rpcutil

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	. "testing"

	"github.com/stretchr/testify/assert"
)

func bodyBufCopy(bodyBuf *bytes.Buffer) *bytes.Buffer {
	cp := new(bytes.Buffer)
	cp.Write(bodyBuf.Bytes())
	return cp
}

func TestBufferedResponseWriter(t *T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Foo", "foo")
		w.WriteHeader(201)
		fmt.Fprint(w, "foo")
	})
	rw := httptest.NewRecorder()
	brw := NewBufferedResponseWriter(rw)
	h.ServeHTTP(brw, nil)

	bodyBuf, err := brw.GetBody()
	assert.Nil(t, err)
	bodyBufCp := bodyBufCopy(bodyBuf)

	// Make sure brw caught writes, and the recorder didn't get any of them
	assert.Equal(t, []byte("foo"), bodyBuf.Bytes())
	assert.Equal(t, "foo", brw.Header().Get("X-Foo"))
	assert.Equal(t, 201, brw.code)
	assert.NotEqual(t, 201, rw.Code)
	assert.Equal(t, 0, rw.Body.Len())

	bodyBufCp.WriteString("bar")
	brw.SetBody(bodyBufCp)

	written, err := brw.ActuallyWrite()
	assert.Nil(t, err)
	assert.Equal(t, int64(6), written)

	assert.Equal(t, 201, rw.Code)
	assert.Equal(t, []byte("foobar"), rw.Body.Bytes())
	assert.Equal(t, "foo", rw.HeaderMap.Get("X-Foo"))
	assert.Equal(t, "6", rw.HeaderMap.Get("Content-Length"))
}

func gzipped(s string) []byte {
	gbuf := new(bytes.Buffer)
	gw := gzip.NewWriter(gbuf)
	gw.Write([]byte(s))
	gw.Close()
	return gbuf.Bytes()
}

func TestBufferedResponseWriterGZip(t *T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Foo", "foo")
		w.Header().Set("Content-Encoding", "gzip")
		w.WriteHeader(201)
		w.Write(gzipped("foo"))
	})
	rw := httptest.NewRecorder()
	brw := NewBufferedResponseWriter(rw)
	h.ServeHTTP(brw, nil)

	bodyBuf, err := brw.GetBody()
	assert.Nil(t, err)
	bodyBufCp := bodyBufCopy(bodyBuf)

	// Make sure brw caught writes, and decompressed the body, and the recorder
	// didn't get any changes
	assert.Equal(t, []byte("foo"), bodyBufCp.Bytes())
	assert.Equal(t, "foo", brw.Header().Get("X-Foo"))
	assert.Equal(t, 201, brw.code)
	assert.NotEqual(t, 201, rw.Code)
	assert.Equal(t, 0, rw.Body.Len())

	bodyBufCp.WriteString("bar")
	brw.SetBody(bodyBufCp)
	expected := gzipped("foobar")

	written, err := brw.ActuallyWrite()
	assert.Nil(t, err)
	assert.Equal(t, int64(len(expected)), written)

	assert.Equal(t, 201, rw.Code)
	assert.Equal(t, expected, rw.Body.Bytes())
	assert.Equal(t, "foo", rw.HeaderMap.Get("X-Foo"))
	assert.Equal(t, strconv.Itoa(len(expected)), rw.HeaderMap.Get("Content-Length"))
}
