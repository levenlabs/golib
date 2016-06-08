package genapi

import (
	"fmt"
	"net"

	"github.com/armon/go-proxyproto"
)

type proxyListener struct {
	net.Listener
	allowed []*net.IPNet
}

func newProxyListener(l net.Listener, allowed []string) (net.Listener, error) {
	var err error
	a := make([]*net.IPNet, len(allowed))
	for i := range allowed {
		if _, a[i], err = net.ParseCIDR(allowed[i]); err != nil {
			return proxyListener{}, err
		}
	}
	return proxyListener{
		Listener: l,
		allowed:  a,
	}, nil
}

func (p proxyListener) Accept() (net.Conn, error) {
	c, err := p.Listener.Accept()
	if err != nil {
		return c, err
	}

	// The next two steps should never error. If they do something is crazy
	// wrong

	ipStr, _, err := net.SplitHostPort(c.RemoteAddr().String())
	if err != nil {
		return nil, fmt.Errorf("invalid RemoteAddr on accepted conn: %s", err)
	}

	ip := net.ParseIP(ipStr)
	if ip == nil {
		return nil, fmt.Errorf("invalid ip on accepted conn: %q", ipStr)
	}

	for _, cidr := range p.allowed {
		if cidr.Contains(ip) {
			return proxyproto.NewConn(c), nil
		}
	}
	return c, nil
}
