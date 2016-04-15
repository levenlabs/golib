// Package radixutil provides various methods for working with the radix.v2
// package, implenting various oft-used behaviors
package radixutil

import (
	"time"

	"github.com/levenlabs/go-srvclient"
	"github.com/mediocregopher/radix.v2/cluster"
	"github.com/mediocregopher/radix.v2/pool"
	"github.com/mediocregopher/radix.v2/redis"
	"github.com/mediocregopher/radix.v2/util"
)

// SRVDialFunc can be used as a DialFunc for various radix packages. It takes in
// an address, and will perform a MaybeSRV on it before actually making the
// connection. The given timeout will be used as the read/write timeout on the
// new connection. A timeout of 0 can be passed in to use whatever the default
// is on the system
func SRVDialFunc(sc srvclient.SRVClient, timeout time.Duration) func(string, string) (*redis.Client, error) {
	return func(network, addr string) (*redis.Client, error) {
		addr = sc.MaybeSRV(addr)
		return redis.DialTimeout(network, addr, timeout)
	}
}

// DialMaybeCluster will do discovery on the redis instance at the given address
// to determine if it is part of a cluster or not. If so a radix Cluster is
// returned, using the given poolSize, if not a normal radix Pool is returned
// just for the given address.
func DialMaybeCluster(network, addr string, poolSize int) (util.Cmder, error) {
	df := SRVDialFunc(srvclient.DefaultSRVClient, 5*time.Second)

	dConn, err := df(network, addr)
	if err != nil {
		return nil, err
	}
	defer dConn.Close()

	if dr := dConn.Cmd("CLUSTER", "SLOTS"); dr.IsType(redis.IOErr) {
		return nil, dr.Err
	} else if dr.IsType(redis.AppErr) {
		return pool.NewCustom(network, addr, poolSize, df)
	} else {
		return cluster.NewWithOpts(cluster.Opts{
			Addr:     addr,
			PoolSize: poolSize,
			Dialer:   df,
		})
	}
}
