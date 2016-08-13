package genapi

import (
	"errors"
	"strconv"
	"time"

	"github.com/levenlabs/go-llog"
	"github.com/levenlabs/go-srvclient"
	"github.com/mediocregopher/lever"
	"github.com/mediocregopher/radix.v2/cluster"
	"github.com/mediocregopher/radix.v2/pool"
	"github.com/mediocregopher/radix.v2/redis"
	"github.com/mediocregopher/radix.v2/util"
)

// NOTE the functionality in here makes radixutil redundant. We might just get
// rid of it

// RedisTpl configures a pool of connections to a redis instance or cluster
type RedisTpl struct {
	ConfigCommon

	// Defaults to 127.0.0.1:6379, or "" if Optional
	Addr string

	// Defaults to 10
	PoolSize int

	// Defaults to 5 seconds. Used as both the connection timeout and the
	// read/write timeout
	Timeout time.Duration

	// If set then this will be used to resolve Addr (by way of MaybeSrv) on
	// every new connection being made
	SRVClient *srvclient.SRVClient
}

func (rtpl RedisTpl) withDefaults() RedisTpl {
	if rtpl.Addr == "" && !rtpl.Optional {
		rtpl.Addr = "127.0.0.1:6379"
	}
	if rtpl.PoolSize == 0 {
		rtpl.PoolSize = 10
	}
	if rtpl.Timeout == 0 {
		rtpl.Timeout = 5 * time.Second
	}
	return rtpl
}

// Params implements the Configurator method
func (rtpl *RedisTpl) Params() []lever.Param {
	rr := rtpl.withDefaults()
	return []lever.Param{
		rtpl.Param(lever.Param{
			Name:        "--redis-addr",
			Description: "Address of redis instance to use. May be a single member of a cluster",
			Default:     rr.Addr,
		}),
		rtpl.Param(lever.Param{
			Name:        "--redis-pool-size",
			Description: "Number of connections to a single redis instance to use. If a cluster is being used, this many connections will be made to each member of the cluster",
			Default:     strconv.Itoa(rr.PoolSize),
		}),
	}
}

// WithParams implements the Configurator method
func (rtpl *RedisTpl) WithParams(lever *lever.Lever) {
	rtpl.Addr, _ = lever.ParamStr(rtpl.ParamName("--redis-addr"))
	rtpl.PoolSize, _ = lever.ParamInt(rtpl.ParamName("--redis-pool-size"))
}

// Redis contains a util.Cmder which is either a cluster or pool, and adds some
// additional methods to it
type Redis struct {
	util.Cmder
}

// Connect actually connects to redis and returns the Redis instance for it. The
// returned Cmder may be nil if the RedisTpl is set Optional.
func (rtpl RedisTpl) Connect() Redis {
	rtpl = rtpl.withDefaults()
	if rtpl.Addr == "" && rtpl.Optional {
		return Redis{}
	}

	kv := llog.KV{
		"addr":     rtpl.Addr,
		"poolSize": rtpl.PoolSize,
	}
	llog.Info("connecting to redis", kv)

	cmder, err := rtpl.connect()
	if err != nil {
		llog.Fatal("error connecting to redis", kv, llog.ErrKV(err))
	}
	return Redis{cmder}
}

func (rtpl RedisTpl) connect() (util.Cmder, error) {
	df := func(network, addr string) (*redis.Client, error) {
		if rtpl.SRVClient != nil {
			addr = rtpl.SRVClient.MaybeSRV(addr)
		}
		return redis.DialTimeout(network, addr, rtpl.Timeout)
	}

	dConn, err := df("tcp", rtpl.Addr)
	if err != nil {
		return nil, err
	}
	defer dConn.Close()

	if dr := dConn.Cmd("CLUSTER", "SLOTS"); dr.IsType(redis.IOErr) {
		return nil, dr.Err
	} else if dr.IsType(redis.AppErr) {
		return pool.NewCustom("tcp", rtpl.Addr, rtpl.PoolSize, df)
	} else {
		return cluster.NewWithOpts(cluster.Opts{
			Addr:     rtpl.Addr,
			PoolSize: rtpl.PoolSize,
			Dialer:   df,
		})
	}
}

// WithClient retrieves a client which can handle the given key, and calls the
// given function on it. An error is only returned if the client couldn't be
// retrieved
func (r Redis) WithClient(key string, fn func(*redis.Client)) error {
	var client *redis.Client
	var err error
	if c, ok := r.Cmder.(*cluster.Cluster); ok {
		if client, err = c.GetForKey(key); err != nil {
			return err
		}
		defer c.Put(client)
	} else if p := r.Cmder.(*pool.Pool); ok {
		if client, err = p.Get(); err != nil {
			return err
		}
		defer p.Put(client)
	} else {
		return errors.New("Redis.Cmder isn't a cluster or pool")
	}

	fn(client)
	return nil
}
