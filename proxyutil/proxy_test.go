package proxyutil

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	. "testing"

	"github.com/levenlabs/golib/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDo(t *T) {
	b := "hello"
	ua := testutil.RandStr()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/empty":
			assert.Empty(t, r.Header.Get("User-Agent"))
		case "/":
			assert.Equal(t, ua, r.Header.Get("User-Agent"))
		case "/gzip":
			w.Header().Set("Content-Encoding", "gzip")
			ew := gzip.NewWriter(w)
			_, err := io.Copy(ew, strings.NewReader(b))
			require.Nil(t, err)
			err = ew.Close()
			require.Nil(t, err)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	ln, err := net.Listen("tcp", "127.0.0.1:")
	if err != nil {
		t.Fatalf("failed creating listen socket: %v", err)
	}
	defer ln.Close()
	srv := &http.Server{
		Handler: handler,
	}
	go srv.Serve(ln)
	rpc := ReverseProxyClient{
		Client: &http.Client{Transport: &http.Transport{}},
	}

	r := httptest.NewRequest("GET", "http://"+ln.Addr().String()+"/", nil)
	r.Header.Set("User-Agent", ua)
	resp, err := rpc.Do(r)
	require.Nil(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	r = httptest.NewRequest("GET", "http://"+ln.Addr().String()+"/empty", nil)
	resp, err = rpc.Do(r)
	require.Nil(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	r = httptest.NewRequest("GET", "http://"+ln.Addr().String()+"/gzip", nil)
	resp, err = rpc.Do(r)
	require.Nil(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	// the content-length and content-encoding headers are removed by stdlib
	assert.Equal(t, "", resp.Header.Get("Content-Encoding"))
	assert.True(t, resp.Uncompressed)

	rpc.DisableCompression()
	// make sure that the default transport pointer isn't modified
	dt := http.DefaultTransport.(*http.Transport)
	assert.False(t, dt.DisableCompression)
	r = httptest.NewRequest("GET", "http://"+ln.Addr().String()+"/gzip", nil)
	resp, err = rpc.Do(r)
	require.Nil(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "gzip", resp.Header.Get("Content-Encoding"))
	assert.False(t, resp.Uncompressed)
}

func TestWriteResponse(t *T) {
	b := "hello"
	r := &http.Response{
		StatusCode: http.StatusOK,
		Body:       ioutil.NopCloser(bytes.NewBufferString(b)),
		Header: http.Header{
			"X-Hostname": []string{"ivy", "quinn", "ivy"},
			"Foo":        []string{"bar"},
			"Connection": []string{"wazzup"},
		},
		Trailer: http.Header{
			"Here": []string{"here"},
		},
	}
	w := httptest.NewRecorder()
	w.Header().Set("X-Hostname", "ivy")
	w.Header().Set("Lorem", "ipsum")
	w.Header().Set("Upgrade", "something")

	err := WriteResponse(w, r)
	require.Nil(t, err)
	assert.Equal(t, b, w.Body.String())
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "ipsum", w.Header().Get("Lorem"))
	assert.Equal(t, "bar", w.Header().Get("Foo"))
	assert.Equal(t, "ivy,quinn,ivy", w.Header().Get("X-Hostname"))
	assert.Equal(t, "here", w.Header().Get("Here"))
	assert.Equal(t, "something", w.Header().Get("Upgrade"))
	assert.Equal(t, "", w.Header().Get("Connection"))

	// StatusCreated isn't allowed to have a body
	r = &http.Response{
		StatusCode: http.StatusCreated,
		Body:       ioutil.NopCloser(bytes.NewBufferString(b)),
	}
	w = httptest.NewRecorder()
	err = WriteResponse(w, r)
	require.Nil(t, err)
	assert.Equal(t, b, w.Body.String())
	assert.Equal(t, http.StatusCreated, w.Code)
}

type storedCloser struct {
	io.Reader
	Closed bool
}

func (c *storedCloser) Close() error {
	c.Closed = true
	return nil
}

type storedCloseWriter struct {
	http.ResponseWriter
	Closed bool
}

func (c *storedCloseWriter) Close() error {
	c.Closed = true
	return nil
}

func TestWriteResponseCompressed(t *T) {
	b := "hello"
	for _, enc := range []string{"gzip", "deflate"} {
		body := &storedCloser{bytes.NewBufferString(b), false}
		r := &http.Response{
			StatusCode: http.StatusOK,
			Body:       body,
			Header: http.Header{
				"Content-Encoding": []string{enc},
				"Content-Length":   []string{"5"},
			},
			Uncompressed:  true,
			ContentLength: 5,
		}

		w := httptest.NewRecorder()
		wc := &storedCloseWriter{w, false}
		err := WriteResponse(wc, r)
		// grab a copy of the gzip'd bytes now before gzip reads them and they're gone
		encb := w.Body.Bytes()
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, enc, w.Header().Get("Content-Encoding"))
		assert.Equal(t, "", w.Header().Get("Content-Length"))
		// the body should be gzip-encoded
		require.Nil(t, err)
		var er io.ReadCloser
		switch enc {
		case "gzip":
			er, err = gzip.NewReader(w.Body)
		case "deflate":
			er = flate.NewReader(w.Body)
		}
		require.Nil(t, err)
		bb, err := ioutil.ReadAll(er)
		require.Nil(t, err)
		require.Nil(t, er.Close())
		assert.Equal(t, b, string(bb))
		assert.True(t, body.Closed)
		assert.False(t, wc.Closed)

		// if it wasn't ever decoded, then shouldn't get encoded
		body = &storedCloser{bytes.NewBuffer(encb), false}
		r = &http.Response{
			StatusCode: http.StatusOK,
			Body:       body,
			Header: http.Header{
				"Content-Encoding": []string{enc},
			},
			Uncompressed: false,
		}
		w = httptest.NewRecorder()
		wc = &storedCloseWriter{w, false}
		err = WriteResponse(wc, r)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, enc, w.Header().Get("Content-Encoding"))
		assert.Equal(t, encb, w.Body.Bytes())
		assert.True(t, body.Closed)
		assert.False(t, wc.Closed)

		// if it has a 0 content-length, don't do anything
		// this isn't really a realistic test except for replaying a pcap/har
		body = &storedCloser{bytes.NewBufferString(""), false}
		r = &http.Response{
			StatusCode: http.StatusOK,
			Body:       body,
			Header: http.Header{
				"Content-Encoding": []string{enc},
				"Content-Length":   []string{"0"},
			},
			Uncompressed:  true,
			ContentLength: 0,
		}
		w = httptest.NewRecorder()
		wc = &storedCloseWriter{w, false}
		err = WriteResponse(wc, r)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Empty(t, w.Header().Get("Content-Encoding"))
		assert.Equal(t, "0", w.Header().Get("Content-Length"))
		assert.Empty(t, w.Body.Bytes())
		assert.True(t, body.Closed)
		assert.False(t, wc.Closed)
	}
}

func TestDecodeResponse(t *T) {
	b := "hello"
	for _, enc := range []string{"gzip", "deflate"} {
		buf := &bytes.Buffer{}
		var ew io.WriteCloser
		switch enc {
		case "gzip":
			ew = gzip.NewWriter(buf)
		case "deflate":
			ew, _ = flate.NewWriter(buf, -1)
		}
		_, err := io.Copy(ew, bytes.NewBufferString(b))
		require.Nil(t, err)
		err = ew.Close()
		require.Nil(t, err)
		body := &storedCloser{buf, false}
		r := &http.Response{
			StatusCode: http.StatusOK,
			Body:       body,
			Header: http.Header{
				"Content-Encoding": []string{enc},
			},
			ContentLength: 1,
		}

		err = DecodeResponse(r)
		require.Nil(t, err)
		assert.True(t, r.Uncompressed)
		bb, err := ioutil.ReadAll(r.Body)
		require.Nil(t, err)
		err = r.Body.Close()
		require.Nil(t, err)
		assert.Equal(t, b, string(bb))
		assert.True(t, body.Closed)
	}

}
