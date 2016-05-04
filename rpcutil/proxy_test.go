package rpcutil

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	. "testing"

	"github.com/stretchr/testify/assert"
)

func TestBufferedResponseWriter(t *T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(201)
		w.Header().Set("X-Foo", "foo")
		fmt.Fprint(w, "foo")
	})
	rw := httptest.NewRecorder()
	brw := NewBufferedResponseWriter(rw)
	h.ServeHTTP(brw, nil)

	// Make sure brw caught writes, and the recorder didn't get any of them
	assert.Equal(t, []byte("foo"), brw.Buffer.Bytes())
	assert.Equal(t, "foo", brw.Header().Get("X-Foo"))
	assert.Equal(t, 201, brw.code)
	assert.NotEqual(t, 201, rw.Code)
	assert.Equal(t, 0, rw.Body.Len())

	brw.Buffer.WriteString("bar")
	written, err := brw.ActuallyWrite()
	assert.Nil(t, err)
	assert.Equal(t, int64(6), written)

	assert.Equal(t, 201, rw.Code)
	assert.Equal(t, []byte("foobar"), rw.Body.Bytes())
	assert.Equal(t, "foo", rw.HeaderMap.Get("X-Foo"))
	assert.Equal(t, "6", rw.HeaderMap.Get("Content-Length"))
}
