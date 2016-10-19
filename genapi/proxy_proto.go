package genapi

import (
	"net"

	"github.com/armon/go-proxyproto"
)

type proxyListener struct {
	net.Listener
	allowed cidrSet
}

func newProxyListener(l net.Listener, allowed []*net.IPNet) net.Listener {
	if len(allowed) == 0 {
		return l
	}
	return proxyListener{
		Listener: l,
		allowed:  allowed,
	}
}

func (p proxyListener) Accept() (net.Conn, error) {
	c, err := p.Listener.Accept()
	if err != nil {
		return c, err
	}

	// The next two steps should never error. If they do something is crazy
	// wrong
	if err := p.allowed.hasAddr(c.RemoteAddr().String()); err == nil {
		return proxyproto.NewConn(c, 0), nil
	}
	return c, nil
}
