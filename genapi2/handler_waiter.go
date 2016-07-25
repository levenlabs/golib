package genapi

import (
	"net/http"
	"sync"
)

type httpWaiter struct {
	http.Handler
	l  sync.Mutex
	c  int
	ch chan struct{}
}

func newHTTPWaiter(h http.Handler) *httpWaiter {
	return &httpWaiter{
		Handler: h,
		ch:      make(chan struct{}, 1),
	}
}

func (hw *httpWaiter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	hw.l.Lock()
	hw.c++
	hw.l.Unlock()

	defer func() {
		r := recover()
		hw.l.Lock()
		hw.c--
		hw.l.Unlock()

		select {
		case hw.ch <- struct{}{}:
		default:
		}

		if r != nil {
			panic(r)
		}
	}()

	hw.Handler.ServeHTTP(w, r)
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
