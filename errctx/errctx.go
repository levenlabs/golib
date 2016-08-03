// Package errctx allows for setting and retrieving contextual information on
// error objects
package errctx

type errctx struct {
	err error
	ctx map[interface{}]interface{}
}

func (ec errctx) Error() string {
	return ec.err.Error()
}

// Base returns the underlying error object that was prevoiusly wrapped in a
// call to Set. If the error did not come from Set it is returned as-is.
func Base(err error) error {
	if ec, ok := err.(errctx); ok {
		return ec.err
	}
	return err
}

// Set takes in an error and one or more key/value pairs. It returns an error
// instance which can have Get called on it with one of those passed in keys to
// retrieve the associated value later.
//
// Errors returned from Set are immutable. For example:
//
//	err := errors.New("ERR")
//	fmt.Println(errctx.Get(err, "foo")) // ""
//
//	err2 := errctx.Set(err, "foo", "a")
//	fmt.Println(errctx.Get(err2, "foo")) // "a"
//
//	err3 := errctx.Set(err2, "foo", "b")
//	fmt.Println(errctx.Get(err2, "foo")) // "a"
//	fmt.Println(errctx.Get(err3, "foo")) // "b"
//
func Set(err error, kvs ...interface{}) error {
	ec := errctx{
		err: Base(err),
		ctx: map[interface{}]interface{}{},
	}

	if ecinner, ok := err.(errctx); ok {
		for k, v := range ecinner.ctx {
			ec.ctx[k] = v
		}
	}
	for i := 0; i < len(kvs); i += 2 {
		ec.ctx[kvs[i]] = kvs[i+1]
	}
	return ec
}

// Get retrieves the value associated with the key by a previous call to Set,
// which this error should have been returned from. Returns nil if the key isn't
// set, or if the error wasn't previously wrapped by Set at all.
func Get(err error, k interface{}) interface{} {
	ec, ok := err.(errctx)
	if !ok {
		return nil
	}
	return ec.ctx[k]
}
