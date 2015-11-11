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
