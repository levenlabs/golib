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

// WithGroupedKeys is used primarily when dealing with a Cluster. It will take
// all the given keys, and group them by which instance in the cluster they
// belong to. It then calls the given function with a client for that instance
// and the set of keys for it. If the given util.Cmder is not a Cluster then it
// must be a Pool, and the given function will only be called once, with all the
// given keys.
//
// This function is useful primarily if you have a large set of keys you want to
// MGET, MSET, DEL, etc.... but you're doing so in a clustered environment.
//
// Note that this operation is not atomic in anyway. If the cluster topology
// changes at any point during using this function you will most likely see an
// error during each invocation of the callback. It's also possible, in this
// case, that the callback won't even be called on every single key given. Make
// sure your application is ok with these conditions.
//
// An error is only returned during the retrieval of a client from the Pool or
// Cluster if an error is seen. Errors which occur during calling of the
// callback itself must be caught and handled outside this function.
func WithGroupedKeys(cmder util.Cmder, fn func(*redis.Client, []string), keys ...string) error {
	if p, ok := cmder.(*pool.Pool); ok {
		c, err := p.Get()
		if err != nil {
			return err
		}
		fn(c, keys)
		p.Put(c)
		return nil
	}

	cl := cmder.(*cluster.Cluster)
	m := map[string][]string{}
	for _, k := range keys {
		addr := cl.GetAddrForKey(k)
		m[addr] = append(m[addr], k)
	}

	cc, err := cl.GetEvery()
	if err != nil {
		return err
	}

	for addr, kk := range m {
		c, ok := cc[addr]
		if !ok {
			// topology changed halfway through, not much we can do
			continue
		}
		fn(c, kk)
	}
	for _, c := range cc {
		cl.Put(c)
	}

	return nil
}
