package genapi

import (
	"net/http"
	"sync"
)

// TODO test this noise

type countHandler struct {
	http.Handler
	sync.Mutex
	count uint64
}

func (c *countHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.Lock()
	c.count++
	c.Unlock()
	c.Handler.ServeHTTP(w, r)
}

// Count returns the number of requests which have been handled so far
func (c *countHandler) Count() uint64 {
	c.Lock()
	defer c.Unlock()
	return c.count
}
