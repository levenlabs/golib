package genapi

import (
	"errors"

	"github.com/levenlabs/go-llog"
	"github.com/mediocregopher/lever"
)

// TODO it should be a goal that all Configurators implemented in this package
// can be used independently, i.e. that they don't actually need Configure
// called, they can just have their fields changed directly or whatever

// Configurator is an entity which can take in parameters from the command-line,
// evironment variables, or a config file, before running
type Configurator interface {

	// Params should return the parameters this Configurator needs in order to
	// operate
	Params() []lever.Param

	// WithParams is called once lever has been used to parse the params
	// returned by Params. That lever instance is passed in, and from it the
	// Configurator can retrieve whatever params it wants and configure itself.
	//
	// NOTE it's not intended that WithParams is used to actually "kick off"
	// execution of some entity, e.g. actually start an rpc server listening on
	// a port. It should merely change some fields on the entity or something
	// along those lines.
	WithParams(*lever.Lever)
}

// NoConfig implements Configurator, but doesn't actually do anything. Useful
// for things which will be added to Config but which don't need any
// configuration
type NoConfig struct{}

// Params always returns nil
func (NoConfig) Params() []lever.Param {
	return nil
}

// WithParams does nothing
func (NoConfig) WithParams(*lever.Lever) {}

// TODO having the fields in Config be public is inconsistent with CallerConfig

// Config wraps a set of Configurators together and attempts to resolve and
// parse their configuration, subsequently calling WithParams on each of them
type Config struct {
	// The name of the overal config, generally the project name
	Name          string
	Configurators []Configurator
}

// used to determine if a new parameter conflicts in any way with any previous
// Param
func hasParamConflict(p lever.Param, existing []lever.Param) bool {
	conflicts := func(name string) bool {
		for i := range existing {
			if existing[i].Name == name {
				return true
			}
			for _, alias := range existing[i].Aliases {
				if alias == name {
					return true
				}
			}
		}
		return false
	}

	if conflicts(p.Name) {
		return true
	}
	for _, alias := range p.Aliases {
		if conflicts(alias) {
			return true
		}
	}

	return false
}

// Configurate will create a lever instance, pull in all the Params for it,
// Parse, then, call WithParams on each Configurator.
//
// Configurate will also deal with making sure different Configurators don't
// have conflicting options, and will fatal if so.
//
// TODO so we care about ^ that? lever will just overwrite a previous Param with
// if another is given with the same Name. Remains to be seen
func (c Config) Configurate() {
	if err := c.configurate(); err != nil {
		llog.Fatal("error configurating", llog.ErrKV(err))
	}
}

// Add takes the given Configurators and adds them to the list. Mostly just a
// convenience around appending to the slice
func (c *Config) Add(cc ...Configurator) {
	c.Configurators = append(c.Configurators, cc...)
}

// do all the actual work in here, returning an error instead of fataling makes
// testing easier
func (c Config) configurate() error {
	var pp []lever.Param
	l := lever.New(c.Name, nil)
	for _, cc := range c.Configurators {
		for _, p := range cc.Params() {
			if hasParamConflict(p, pp) {
				return llog.ErrWithKV(
					errors.New("parameter conflicts with existing one"),
					llog.KV{"paramName": p.Name, "paramAliases": p.Aliases},
				)
			}
			pp = append(pp, p)
			l.Add(p)
		}
	}

	l.Parse()
	for _, cc := range c.Configurators {
		cc.WithParams(l)
	}
	return nil
}
