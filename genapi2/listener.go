package genapi

import (
	"crypto/tls"
	"fmt"
	"net"

	"github.com/levenlabs/go-llog"
	"github.com/mediocregopher/lever"
)

// TODO write some damn tests

// TLSLoader loads a set of tls certificates to use when listening on a TLS
// enabled connection
type TLSLoader interface {
	Configurator
	Load() ([]tls.Certificate, error)
}

// FileTLSLoader is a TLSLoader which will load one or more cert/key file
// combinations based on configuration
type FileTLSLoader struct {
	ConfigCommon

	certFiles []string
	keyFiles  []string
}

// Params implements the method for Configurator
func (f *FileTLSLoader) Params() []lever.Param {
	return []lever.Param{
		f.Param(lever.Param{
			Name:        "--tls-cert-file",
			Description: "Certificate file to use for TLS. Maybe be specified more than once. Each cert file must correspond with a matching key file",
		}),
		f.Param(lever.Param{
			Name:        "--tls-key-file",
			Description: "Key file to use for TLS. Maybe be specified more than once. Each key file must correspond with a matching cert file",
		}),
	}
}

// WithParams implements the method for Configurator
func (f *FileTLSLoader) WithParams(l *lever.Lever) {
	f.certFiles, _ = l.ParamStrs(f.ParamName("--tls-cert-file"))
	f.keyFiles, _ = l.ParamStrs(f.ParamName("--tls-key-file"))
	if len(f.certFiles) != len(f.keyFiles) {
		llog.Fatal("number of --tls-cert-file must match number of --tls-key-file")
	}
}

// Load implenets the method for TLSLoader. It will re-read the configured files
// every time it is called
func (f *FileTLSLoader) Load() ([]tls.Certificate, error) {
	var ret []tls.Certificate
	for i := range f.certFiles {
		c, err := tls.LoadX509KeyPair(f.certFiles[i], f.keyFiles[i])
		if err != nil {
			return nil, llog.ErrWithKV(err, llog.KV{
				"certFile": f.certFiles[i],
				"keyFiles": f.keyFiles[i],
			})
		}
		ret = append(ret, c)
	}
	return ret, nil
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
	ConfigCommon

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

func (l ListenerTpl) withDefaults() ListenerTpl {
	if l.Network == "" && !l.Optional {
		l.Network = "tcp"
	}
	if l.Addr == "" && !l.Optional {
		l.Addr = ":0"
	}
	return l
}

// Params implements the Configurator method. It will include the Params from
// its TLSLoader, if that is set
func (l *ListenerTpl) Params() []lever.Param {
	ll := l.withDefaults()
	params := []lever.Param{
		l.Param(lever.Param{
			Name:        "--listen-addr",
			Description: "[address]:port to listen. If port is zero a port will be chosen randomly",
			Default:     ll.Addr,
		}),
	}

	if ll.TLSLoader != nil {
		params = append(params, ll.TLSLoader.Params()...)
	}
	return params
}

// WithParams implements the Configurator method. It will also call the
// WithParams method on its TLSLoader, if that is set
func (l *ListenerTpl) WithParams(lever *lever.Lever) {
	l.Addr, _ = lever.ParamStr("--listen-addr")
	if l.TLSLoader != nil {
		l.TLSLoader.WithParams(lever)
	}
}

// Listener creates a listen socket on the Network/Addr in the template, and
// configures per the other template fields, returning the Listener
func (l ListenerTpl) Listener() (*Listener, error) {
	l = l.withDefaults()

	ln, err := net.Listen(l.Network, l.Addr)
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
