package genapi2

import (
	"crypto/sha1"
	"encoding/hex"
	"strings"
	"time"

	"github.com/levenlabs/go-llog"
	"github.com/levenlabs/go-srvclient"
	"github.com/mediocregopher/lever"
	"github.com/mediocregopher/skyapi/client"
	"github.com/miekg/dns"
)

func dcPrefix(dc string) string {
	dcHash := sha1.Sum([]byte(ll.Datacenter))
	return hex.EncodeToString(dcHash[:])[:20]
}

// TODO not super sold on this name

// LLServiceDiscovery deals with both advertising a service it's running in so
// other services can discover it, as well as resolving other outside services.
// Its logic is rather specific to Leven Labs projects, but it could be used in
// another similarly set up environment
type LLServiceDiscovery struct {
	ConfigCommon

	// Required if Advertise is called. Address of skyapi instance to advertise
	// to.
	Addr string

	// Optional name of the datacenter to advertise from. If set then this will
	// advertise as being part of this datacenter, and when resolving will
	// prefer other services in this datacenter
	Datacenter string

	srv *srvclient.SRVClient
}

// NewLLServiceDiscovery returns an initialized struct
func NewLLServiceDiscovery() *LLServiceDiscovery {
	s := &srvclient.SRVClient{}
	s.EnableCacheLast()
	ll := &LLServiceDiscovery{SRVClient: s}

	ll.srv.Preprocess = func(m *dns.Msg) {
		if ll.Datacenter == "" {
			return
		}

		dc := dcPrefix(ll.Datacenter)
		for i := range m.Answer {
			if ansSRV, ok := m.Answer[i].(*dns.SRV); ok {
				tar := ansSRV.Target
				if strings.HasPrefix(tar, dcHash+"-") {
					if ansSRV.Priority < 2 {
						ansSRV.Priority = uint16(0)
					} else {
						ansSRV.Priority = ansSRV.Priority - 1
					}
				}
			}
		}
	}

	return ll
}

// Params implements the Configurator method
func (ll *LLServiceDiscovery) Params() []lever.Param {
	return []lever.Param{
		ll.Param(lever.Param{
			Name:        "--skyapi-addr",
			Description: "Address of skyapi instance to connect to. Leave blank to prevent connecting",
		}),
	}
}

func (ll *LLServiceDiscovery) WithParams(lever *lever.Lever) {
	ll.Addr, _ = lever.ParamStr(ll.ParamName("--skyapi-addr"))
}

// Resolve implements the method for Resolver
func (ll *LLServiceDiscovery) Resolve(a string) (string, error) {
	return ll.srv.MaybeSRV(a), nil
}

// Advertise makes a connection to skyapi and advertises that the service with
// the given name is listening on the given address.  stopCh, if not nil, may be
// closed to close the connection. This call will block until stopCh is closed.
//
// If the Addr field on the struct is empty then this will return immediately
func (ll *LLServiceDiscovery) Advertise(name, listenAddr string, stopCh chan struct{}) {
	if ll.Addr == "" {
		return
	}

	kv := llog.KV{
		"serviceName": name,
		"listenAddr":  listenAddr,
	}

	var prefix string
	if ll.Datacenter != "" {
		prefix = dcPrefix(ll.Datacenter)
		kv["prefix"] = prefix
	}

	for {
		addr, err := ll.Resolve(ll.Addr)
		if err != nil {
			llog.Warn("could not resolve skyapi address", kv, llog.ErrKV(err))
			sleep(1 * time.Sleep)
			continue
		}
		kv := kv.Set("skyapiAddr", addr)

		llog.Info("connecting to skyapi", kv)
		err := client.ProvideOpts(client.Opts{
			SkyAPIAddr:        addr,
			Service:           name,
			ThisAddr:          listenAddr,
			ReconnectAttempts: 0, // do not attempt to reconnect, we'll do that here
			StopCh:            stopCh,
			Prefix:            prefix,
		})
		if err != nil {
			llog.Warn("skyapi error", kv, llog.ErrKV(err))
			time.Sleep(1 * time.Second)
			continue
		} else {
			// If there wasn't an error but skyapi stopped, it's because the
			// stopCh was closed
			return
		}
	}
}
