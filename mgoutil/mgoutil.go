// Package mgoutil provides various methods for working with the mgo.v2 package,
// implementing various oft-used behaviors
package mgoutil

import (
	"github.com/levenlabs/go-llog"
	"gopkg.in/mgo.v2"
	"time"
)

// Index is used in MustEnsureIndexes to convey all the information needed to
// ensure a background index
//
// DEPRECATED: MustEnsureIndexes is deprecated, and therefore this type is as
// well. This will be removed at some point in the future
type Index struct {
	Name   string // human readable name for the index, only used for the log entry
	Keys   []string
	Coll   string
	Unique bool
	Sparse bool
}

// MustEnsureIndexes takes the given indexes and ensure's they are all in place.
// It will make the calls with Background:true. If any indexes fail a message
// will be logged and the process will exit
//
// DEPRECATED: Use the MustEnsureIndexes method on SessionHelper instead. This
// will be removed at some point in the future
//
// NOTE WHEN SWITCHING TO NEW METHOD: the new method does *not* automatically
// set Background: true like this one does, so be aware of that difference when
// switching over
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
			Unique:     i.Unique,
			Sparse:     i.Sparse,
		})
		if err != nil {
			kv["err"] = err
			llog.Fatal("could not ensure mongo index", kv)
		}
	}
}

// EnsureSession connections to the given addr with a 5 second timeout, sets the
// session to Safe, sets the mode to PrimaryPreferred, and returns the resulting
// mgo.Session. If it fails to connect it fatals.
func EnsureSession(addr string) *mgo.Session {
	kv := llog.KV{"addr": addr}
	llog.Info("dialing mongo", kv)
	s, err := mgo.DialWithTimeout(addr, 5 * time.Second)
	if err != nil {
		kv["err"] = err
		llog.Fatal("error calling mgo.DialWithTimeout", kv)
	}
	s.SetSafe(&mgo.Safe{})
	s.SetMode(mgo.PrimaryPreferred, true)

	llog.Info("done setting up mongo connection", kv)
	return s
}

// CollectionHelper is a simple helper type which makes it easy to call methods
// on a Database or Collection safely. It will handle the Copy'ing and Close'ing
// of the given Session automatically
type SessionHelper struct {
	Session *mgo.Session
	DB      string
	Coll    string
}

// WithDB handles calls the given function on an instance of the database named
// by the field in the SessionHelper. It will handle Copy'ing and Close'ing the
// Session object automatically.
func (s SessionHelper) WithDB(fn func(*mgo.Database)) {
	session := s.Session.Copy()
	defer session.Close()
	fn(session.DB(s.DB))
}

// WithColl handles calls the given function on an instance of the collection
// named by the field in the SessionHelper. It will handle Copy'ing and
// Close'ing the Session object automatically.
func (s SessionHelper) WithColl(fn func(*mgo.Collection)) {
	s.WithDB(func(db *mgo.Database) {
		fn(db.C(s.Coll))
	})
}

// MustEnsureIndexes takes the given indexes and ensure's they are all in place.
// If any indexes fail a message will be logged and the process will exit
func (s SessionHelper) MustEnsureIndexes(indexes ...mgo.Index) {
	s.WithColl(func(c *mgo.Collection) {
		for _, index := range indexes {
			kv := llog.KV{
				"key":  index.Key,
				"coll": s.Coll,
				"db":   s.DB,
			}
			llog.Info("ensuring mongo index", kv)
			if err := c.EnsureIndex(index); err != nil {
				kv["err"] = err
				llog.Fatal("could not ensure mongo index", kv)
			}
		}
	})
}
