package genapi

import (
	"net/http"
	"net/http/httptest"
	. "testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddCORSHeaders(t *T) {
	o := "example.com"
	r, err := http.NewRequest("GET", "/", nil)
	require.Nil(t, err)
	r.Header.Set("Origin", o)
	r.Host = o
	w := httptest.NewRecorder()
	pass := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	//also verify that double cors doesn't send twice
	AddCORSHeaders(AddCORSHeaders(pass)).ServeHTTP(w, r)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Len(t, w.HeaderMap["Access-Control-Allow-Methods"], 1)
	assert.Equal(t, "POST, OPTIONS", w.Header().Get("Access-Control-Allow-Methods"))
	assert.Len(t, w.HeaderMap["Access-Control-Allow-Headers"], 1)
	assert.Equal(t, "DNT,Keep-Alive,User-Agent,X-Requested-With,If-Modified-Since,Cache-Control,Content-Type", w.Header().Get("Access-Control-Allow-Headers"))
	assert.Len(t, w.HeaderMap["Access-Control-Allow-Origin"], 1)
	assert.Equal(t, o, w.Header().Get("Access-Control-Allow-Origin"))

	fail := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fail()
	})

	r, err = http.NewRequest("OPTIONS", "/", nil)
	require.Nil(t, err)
	r.Header.Set("Origin", o)
	r.Host = o
	w = httptest.NewRecorder()
	AddCORSHeaders(fail).ServeHTTP(w, r)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Len(t, w.HeaderMap["Access-Control-Allow-Methods"], 1)
	assert.Len(t, w.HeaderMap["Access-Control-Allow-Headers"], 1)
	assert.Len(t, w.HeaderMap["Access-Control-Allow-Origin"], 1)
}
