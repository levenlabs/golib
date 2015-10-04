// Package mgoutil provides various methods for working with the mgo.v2 package,
// implementing various oft-used behaviors
package mgoutil

import (
	"github.com/levenlabs/go-llog"
	"gopkg.in/mgo.v2"
)

// Index is used in MustEnsureIndexes to convey all the information needed to
// ensure a background index
type Index struct {
	Name string // human readable name for the index, only used for the log entry
	Keys []string
	Coll string
}

// MustEnsureIndexes takes the given indexes and ensure's they are all in place.
// It will make the calls with Background:true. If any indexes fail a message
// will be logged and the process will exit
func MustEnsureIndexes(db *mgo.Database, indexes ...Index) {
	for _, i := range indexes {
		kv := llog.KV{
			"name": i.Name,
			"keys": i.Keys,
			"coll": i.Coll,
			"db":   db.Name,
		}
		llog.Info("ensuring mongo index", kv)
		err := db.C(i.Coll).EnsureIndex(mgo.Index{
			Key:        i.Keys,
			Background: true,
		})
		if err != nil {
			kv["err"] = err
			llog.Fatal("could not ensure mongo index", kv)
		}
	}
}
