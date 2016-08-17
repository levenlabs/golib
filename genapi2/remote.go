package genapi

import "github.com/mediocregopher/lever"

// Remote provides for setting and configuring the address of a remote endpoint,
// as well as re-resolving it using an optional Resolver
type Remote struct {
	// Name of the remote as it will appear in the cli/config
	Name string

	// Addr is the default address of the endpoint, if any.
	Addr string

	// Resolver, if set, will be used on each call of Addr to re-resolve the
	// Addr
	Resolver
}

// Params implements the Configurator method
func (r *Remote) Params() []lever.Param {
	return []lever.Param{
		{
			Name:        "--" + r.Name,
			Description: "Address of " + r.name + ", which will be resolved if needed",
			Default:     r.Addr,
		},
	}
}

// WithParams implements the Configurator method
func (r *Remote) WithParams(lever *lever.Lever) {
	r.Addr, _ = lever.ParamStr("--" + r.Name)
}

// Addr returns a host:port address for the remote Add'd with the given name
func (r Remote) Addr() (string, error) {
	return maybeResolve(r.Resolver, r.Addr)
}
