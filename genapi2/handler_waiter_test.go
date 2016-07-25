package genapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	. "testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testWaiter = httpWaiter{ch: make(chan struct{})}
var testWaiterHandler = func() http.Handler {
	m := http.NewServeMux()

	m.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "OHAI")
	})

	m.HandleFunc("/sleep", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(1 * time.Second)
		fmt.Fprintln(w, "OHAI")
	})

	m.HandleFunc("/panic", func(w http.ResponseWriter, r *http.Request) {
		panic("oh snap")
	})

	return testWaiter.handler(m)
}()

func TestHTTPWaiter(t *T) {
	for _, u := range []string{"/", "/sleep", "/panic"} {
		go func(u string) {
			r, err := http.NewRequest("GET", u, nil)
			require.Nil(t, err)
			w := httptest.NewRecorder()
			t.Logf("returned from %s", u)

			defer func() {
				if recover() != nil {
					assert.Equal(t, "/panic", u)
				}
			}()

			testWaiterHandler.ServeHTTP(w, r)
			if u == "/panic" {
				assert.Equal(t, 500, w.Code)
			} else {
				assert.Equal(t, "OHAI\n", w.Body.String())
			}
		}(u)
	}

	start := time.Now()
	// give the goroutines time to actually make their requests
	time.Sleep(100 * time.Millisecond)
	go func() {
		time.Sleep(2 * time.Second)
		t.Fatal("timeout waiting for httpWaiter")
	}()

	testWaiter.wait()
	since := time.Since(start)
	t.Logf("since: %v", since)
	assert.True(t, time.Since(start) > 1*time.Second)
}
