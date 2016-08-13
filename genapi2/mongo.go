package genapi

import (
	"time"

	"github.com/levenlabs/go-llog"
	"github.com/mediocregopher/lever"
	"gopkg.in/mgo.v2"
)

// MongoTpl configures a Mongo instance which contains an mgo.Session. Each
// MongoTpl corresponds to a single db
type MongoTpl struct {
	ConfigCommon

	// Defaults to 127.0.0.1:27017
	Addr string

	// Required. The database to connect to.
	DBName string

	// Optional map of collection name to indexes to ensure are set on Connect
	Indexes map[string][]mgo.Index
}

func (mtpl MongoTpl) withDefaults() MongoTpl {
	if mtpl.Addr == "" && !mtpl.Optional {
		mtpl.Addr = "127.0.0.1:27017"
	}
	return mtpl
}

// Params implements the Configurator method
func (mtpl *MongoTpl) Params() []lever.Param {
	mm := mtpl.withDefaults()
	return []lever.Param{
		mtpl.Param(lever.Param{
			Name:        "--mongo-addr",
			Description: "Address of mongo instance to use",
			Default:     mm.Addr,
		}),
	}
}

// WithParams implements the Configurator method
func (mtpl *MongoTpl) WithParams(lever *lever.Lever) {
	mtpl.Addr, _ = lever.ParamStr(mtpl.ParamName("--mongo-addr"))
}

// Mongo contains an mgo.Session and adds some additional methods to it
type Mongo struct {
	dbName string
	*mgo.Session
}

// Connect actually connects to mongo and returns the Mongo instance for it. The
// returned mgo.Session may be nil if the MongoTpl is marked Optional
func (mtpl MongoTpl) Connect() Mongo {
	mtpl = mtpl.withDefaults()
	kv := llog.KV{"addr": mtpl.Addr}
	llog.Info("dialing mongo", kv)

	s, err := mgo.DialWithTimeout(mtpl.Addr, 5*time.Second)
	if err != nil {
		llog.Fatal("error calling mgo.DialWithTimeout", kv, llog.ErrKV(err))
	}
	s.SetSafe(&mgo.Safe{})
	s.SetMode(mgo.PrimaryPreferred, true)
	m := Mongo{
		dbName:  mtpl.DBName,
		Session: s,
	}

	for coll, ii := range mtpl.Indexes {
		for _, i := range ii {
			ikv := llog.KV{
				"key":  i.Key,
				"coll": coll,
				"db":   s.DB,
			}
			llog.Info("ensuring mongo index", kv, ikv)
			var err error
			m.WithColl(coll, func(c *mgo.Collection) {
				err = c.EnsureIndex(i)
			})
			if err != nil {
				llog.Fatal("could not ensure mongo index", kv, ikv, llog.ErrKV(err))
			}
		}
	}

	return m
}

// WithDB handles calls the given function on an instance of Mongo's database It
// will handle Copy'ing and Close'ing the Session object automatically.
//
// If Session is nil then fn will be called with nil
func (m Mongo) WithDB(fn func(*mgo.Database)) {
	if m.Session == nil {
		fn(nil)
		return
	}

	s := m.Session.Copy()
	defer s.Close()
	fn(s.DB(m.dbName))
}

// WithColl handles calls the given function on an instance of the given
// collection It will handle Copy'ing and Close'ing the Session object
// automatically.
//
// If Session is nil then fn will be called with nil
func (m Mongo) WithColl(collName string, fn func(*mgo.Collection)) {
	if m.Session == nil {
		fn(nil)
		return
	}
	m.WithDB(func(db *mgo.Database) {
		fn(db.C(collName))
	})
}
