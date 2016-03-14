package genapi

import (
	"net/http"
	"sync"
)

type httpWaiter struct {
	l  sync.Mutex
	c  int
	ch chan struct{}
}

func (hw *httpWaiter) handler(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hw.l.Lock()
		hw.c++
		hw.l.Unlock()

		h.ServeHTTP(w, r)

		hw.l.Lock()
		hw.c--
		hw.l.Unlock()

		select {
		case hw.ch <- struct{}{}:
		default:
		}
	})
}

// waits for the number of active http requests to become zero
func (hw *httpWaiter) wait() {
	for {
		hw.l.Lock()
		c := hw.c
		hw.l.Unlock()

		if c == 0 {
			return
		}

		<-hw.ch
	}
}
