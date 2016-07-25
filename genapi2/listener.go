package genapi

import (
	"crypto/tls"
	"fmt"
	"net"
)

// TODO write some damn tests

// TLSLoader loads a set of tls certificates to use when listening on a TLS
// enabled connection
type TLSLoader interface {
	Load() ([]tls.Certificate, error)
}

// Listener is what is returned by ListenerTpl
type Listener struct {
	net.Listener
	lr *listenerReloader
}

// Reload will reload the socket's configuration without closing or remaking it.
// If TLSLoader was set in the original ListenerTpl then that loader will be
// re-called and the TLS config replaced.
func (l *Listener) Reload() error {
	if l.lr != nil {
		return l.lr.Reload()
	}
	return nil
}

// ListenerTpl describes information needed to create a net.Listener which will
// process tcp connections
type ListenerTpl struct {
	// Defaults to "tcp"
	Network string

	// Defaults to ":0"
	Addr string

	// Defaults to empty. Only ips matching a CIDR found in this set will be
	// allowed to use the PROXY protocol
	AllowedProxyCIDRs []string

	// If not nil the connection will be TLS enabled using the certificates
	// returned by this loader
	TLSLoader
}

// Listener creates a listen socket on the Network/Addr in the template, and
// configures per the other template fields, returning the Listener
func (l ListenerTpl) Listener() (*Listener, error) {
	if l.Addr == "" {
		l.Addr = ":0"
	}

	ln, err := net.Listen("tcp", l.Addr)
	if err != nil {
		return nil, fmt.Errorf("creating listen addr on %s: %s", l.Addr, err)
	}

	if tcpln, ok := ln.(*net.TCPListener); ok {
		ln = net.Listener(tcpKeepAliveListener{tcpln})
	}

	if ln, err = newProxyListener(ln, l.AllowedProxyCIDRs); err != nil {
		ln.Close()
		return nil, err
	}

	var lr *listenerReloader
	if l.TLSLoader != nil {
		lr, err = newListenerReloader(ln, tlsMaker(l.TLSLoader))
		if err != nil {
			ln.Close()
			return nil, err
		}
		ln = lr
	}

	return &Listener{Listener: ln, lr: lr}, nil

}

func tlsMaker(tl TLSLoader) func(net.Listener) (net.Listener, error) {
	return func(netln net.Listener) (net.Listener, error) {
		certs, err := tl.Load()
		if err != nil {
			return nil, err
		}
		tf := &tls.Config{Certificates: certs}
		tf.BuildNameToCertificate()
		return tls.NewListener(netln, tf), nil
	}
}
