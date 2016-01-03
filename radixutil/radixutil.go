// Package radixutil provides various methods for working with the radix.v2
// package, implenting various oft-used behaviors
package radixutil

import (
	"github.com/mediocregopher/radix.v2/cluster"
	"github.com/mediocregopher/radix.v2/pool"
	"github.com/mediocregopher/radix.v2/redis"
	"github.com/mediocregopher/radix.v2/util"
)

// DialMaybeCluster will do discovery on the redis instance at the given address
// to determine if it is part of a cluster or not. If so a radix Cluster is
// returned, using the given poolSize, if not a normal radix Pool is returned
// just for the given address.
func DialMaybeCluster(network, addr string, poolSize int) (util.Cmder, error) {
	dConn, err := redis.Dial(network, addr)
	if err != nil {
		return nil, err
	}
	defer dConn.Close()

	if dr := dConn.Cmd("CLUSTER", "SLOTS"); dr.IsType(redis.IOErr) {
		return nil, dr.Err
	} else if dr.IsType(redis.AppErr) {
		return pool.New(network, addr, poolSize)
	} else {
		return cluster.NewWithOpts(cluster.Opts{
			Addr:     addr,
			PoolSize: poolSize,
		})
	}
}
