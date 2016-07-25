package genapi

import (
	"sync"

	"github.com/levenlabs/lrpc"
)

// TODO shouldn't actually use this?

type rpcWaiter struct {
	h  lrpc.Handler
	l  sync.Mutex
	c  int
	ch chan struct{}
}

func newRPCWaiter(h lrpc.Handler) *rpcWaiter {
	return rpcWaiter{
		ch: make(chan struct{}),
		h:  h,
	}
}

func (rw *rpcWaiter) ServeRPC(c lrpc.Call) interface{} {
	rw.l.Lock()
	rw.c++
	rw.l.Unlock()

	defer func() {
		r := recover()
		rw.l.Lock()
		rw.c--
		rw.l.Unlock()

		select {
		case hw.ch <- struct{}{}:
		default:
		}

		if r != nil {
			panic(r)
		}
	}()

	return rw.h.ServeRPC(c)
}

// waits for the number of active rpc requests to become zero
func (rw *rpcWaiter) wait() {
	for {
		rw.l.Lock()
		c := rw.c
		rw.l.Unlock()

		if c == 0 {
			return
		}

		<-hw.ch
	}
}
