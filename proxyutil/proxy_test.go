package proxyutil

import (
	"bytes"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	. "testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteResponse(t *T) {
	b := "hello"
	r := &http.Response{
		StatusCode: http.StatusCreated,
		Body:       ioutil.NopCloser(bytes.NewBufferString(b)),
		Header: http.Header{
			"X-Hostname": []string{"ivy", "quinn", "ivy"},
			"Foo":        []string{"bar"},
		},
		Trailer: http.Header{
			"Here": []string{"here"},
		},
	}
	w := httptest.NewRecorder()
	w.Header().Set("X-Hostname", "ivy")
	w.Header().Set("Lorem", "ipsum")

	err := WriteResponse(w, r)
	require.Nil(t, err)
	assert.Equal(t, b, w.Body.String())
	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Equal(t, "ipsum", w.Header().Get("Lorem"))
	assert.Equal(t, "bar", w.Header().Get("Foo"))
	assert.Equal(t, "ivy,quinn,ivy", w.Header().Get("X-Hostname"))
	assert.Equal(t, "here", w.Header().Get("Here"))
}
