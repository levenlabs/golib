package genapi

import (
	"errors"
	"net"
)

// listenerReloader is a net.Listener whose underlying configuration can be
// swapped in and out at any time.
//
// An underlying net.Listener is kept internally and remains constant (likely
// the raw tcp listener returned from net.Listen), and a maker function takes
// that and returns a new listener which wraps the underlying one. A new
// wrapping listener can be made and hot-swapped in at any time using Reload
type listenerReloader struct {
	inner   net.Listener
	maker   func(net.Listener) (net.Listener, error)
	lch     chan net.Listener
	newCh   chan net.Listener
	closeCh chan net.Listener
}

func newListenerReloader(inner net.Listener, maker func(net.Listener) (net.Listener, error)) (*listenerReloader, error) {
	lr := &listenerReloader{
		inner:   inner,
		maker:   maker,
		lch:     make(chan net.Listener),
		newCh:   make(chan net.Listener),
		closeCh: make(chan net.Listener),
	}
	curr, err := lr.maker(inner)
	if err != nil {
		return nil, err
	}
	go lr.spin(curr)
	return lr, nil
}

// the spinner is essentially a buffer for what the "current" listener is.
// Accept blocks forever potentially, so we use this to handle reload requests
// even when Accept might be blocking.
//
// lch constantly has the "current" listener written to it, so it's available
// whenever Accept is called. Then newCh takes in new listeners that might come
// from Reload and makes them the new "current", so they'll be given out for
// subsequent Accept calls
//
// closeCh is only read when closing, so we consistantly write to it until it
// reads and then we know to shut down. Anything that was blocked on lch or
// newCh will immediately be freed since we're closing those channels. closeCh
// is closed as well to signal to Close that we're closed.
func (lr *listenerReloader) spin(curr net.Listener) {
	for {
		select {
		case lr.lch <- curr:
		case curr = <-lr.newCh:
		case lr.closeCh <- curr:
			close(lr.lch)
			close(lr.newCh)
			close(lr.closeCh)
			return
		}
	}
}

func (lr *listenerReloader) Accept() (net.Conn, error) {
	return (<-lr.lch).Accept()
}

// Close only closes the wrapping net.Listener, it's assumed the underlying one
// will be subsequently closed down the chain
// In the event of http.Server.Shutdown, Close will be called by the defer in
// Serve and the Shutdown function. So we detect if we're already closed and
// return an error if we already are. This seems consistant with builtin.
func (lr *listenerReloader) Close() error {
	l, ok := <-lr.closeCh
	if !ok {
		return errors.New("already closed")
	}
	return l.Close()
}

func (lr *listenerReloader) Addr() net.Addr {
	return (<-lr.lch).Addr()
}

// Reload calls the maker on the underlying net.Listener, generating a new
// wrapping listenerwhich will be swapped in place of what's currently being
// used.This can be called at the same time as an Accept is running.
//
// If the maker returns an error that error is returned and no swapping is done
//
// If Accept is being called at the same time that this is called, it will not
// be interrupted and the next connection which comes in will still go to the
// previous wrapping listener. All subsequent calls will get the new wrapping
// listener.
//
// If we're already closed, this will panic.
func (lr *listenerReloader) Reload() error {
	newOuter, err := lr.maker(lr.inner)
	if err != nil {
		return err
	}
	lr.newCh <- newOuter
	return nil
}
