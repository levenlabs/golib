package genapi

import (
	"time"

	"github.com/levenlabs/go-llog"
	"github.com/mediocregopher/lever"
	"github.com/mediocregopher/okq-go.v2"
	"github.com/mediocregopher/radix.v2/pool"
)

// OkqTpl configures a pool of connections to an okq instance or set of
// instances
type OkqTpl struct {
	ConfigCommon

	// Defaults to 127.0.0.1:4777
	Addr string

	// Defaults to 10
	PoolSize int

	// Defaults to 30 seconds. This will be the connect/read/write timeout for
	// the network connections and the NotifyTimeout for Client.
	Timeout time.Duration

	// If set then this will be used to resolve Addr on every new connection
	// being made
	Resolver
}

func (otpl OkqTpl) withDefaults() OkqTpl {
	if otpl.Addr == "" && !otpl.Optional {
		otpl.Addr = "127.0.0.1:4777"
	}
	if otpl.PoolSize == 0 {
		otpl.PoolSize = 10
	}
	if otpl.Timeout == 0 {
		otpl.Timeout = 30 * time.Second
	}
	return otpl
}

// Params implements the Configurator method
func (otpl *OkqTpl) Params() []lever.Param {
	oo := otpl.withDefaults()
	return []lever.Param{
		oo.Param(lever.Param{
			Name:        "--okq-addr",
			Description: "Address of okq instance to use",
			Default:     "127.0.0.1:4777",
		}),
		oo.Param(lever.Param{
			Name:        "--okq-pool-size",
			Description: "Number of connections to okq to make",
			Default:     "10",
		}),
	}
}

// WithParams implements the Configurator method
func (otpl *OkqTpl) WithParams(lever *lever.Lever) {
	otpl.Addr, _ = lever.ParamStr(otpl.ParamName("--okq-addr"))
	otpl.PoolSize, _ = lever.ParamInt(otpl.ParamName("--okq-pool-size"))
}

// Okq contains an okq.Client (and may one day add some additional methods to
// it)
type Okq struct {
	*okq.Client
}

// Connect actually connects to okq and returns the Okq instance for it. The
// returned Client may be nil if the OkqTpl is set Optional
func (otpl OkqTpl) Connect() Okq {
	otpl = otpl.withDefaults()
	if otpl.Addr == "" && otpl.Optional {
		return Okq{}
	}

	kv := llog.KV{
		"addr":     otpl.Addr,
		"poolSize": otpl.PoolSize,
	}
	llog.Info("connecting to okq", kv)

	// conveniently we can just reuse RedsTpl for this part, and assume that it
	// creates a pool

	c, err := RedisTpl{
		Addr:     otpl.Addr,
		PoolSize: otpl.PoolSize,
		Timeout:  otpl.Timeout,
		Resolver: otpl.Resolver,
	}.connect()
	if err != nil {
		llog.Fatal("error connecting to okq", kv, llog.ErrKV(err))
	}

	return Okq{
		Client: okq.NewWithOpts(okq.Opts{
			RedisPool:     c.(*pool.Pool),
			NotifyTimeout: otpl.Timeout,
		}),
	}
}
