package rpcutil

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"

	"gopkg.in/validator.v2"
)

var vRegexes = map[string]*regexp.Regexp{}

// RegisterRegex registers a regex with a given name, so it can be used as an
// argument to various custom validators installed by this package
func RegisterRegex(name, regex string) {
	vRegexes[name] = regexp.MustCompile(regex)
}

// InstallCustomValidators will set up our set of custom validators for the
// gopkg.in/validator.v2 package. You can see available validators which are
// installed by looking for the Validate* functions in this package
func InstallCustomValidators() {
	validator.SetValidationFunc("preRegex", ValidatePreRegex)
	validator.SetValidationFunc("arrMap", ValidateArrMap)
}

// ValidatePreRegex will run a regex named by regexName on a string. This is
// registered as the "preRegex" validator, and can be used like
// `validate:"preRegex=regexName"` (see gopkg.in/validator.v2) in conjunction
// with RegisterRegex
//
// When validating this will allow through an empty string
func ValidatePreRegex(v interface{}, regexName string) error {
	r, ok := vRegexes[regexName]
	if !ok {
		panic(fmt.Sprintf("unknown regex: %q", regexName))
	}

	vs, ok := v.(string)
	if !ok {
		return errors.New("not a string")
	}

	// We allow blank strings no matter what, if a blank string is not wanted to
	// be allowed then the "nonzero" validator should be added
	if vs == "" {
		return nil
	}

	if !r.Match([]byte(vs)) {
		return errors.New("badly formatted")
	}

	return nil
}

// ValidateArrMap maps over an array and validates each element in it using the
// passed in tag. For example
//
//	validator:"min=1,arrMap=min=1,arrMap=max=5,arrMap=regexp=[A-Z]+"
func ValidateArrMap(v interface{}, param string) error {
	vv := reflect.ValueOf(v)
	if vv.Kind() == reflect.Ptr {
		vv = vv.Elem()
	}

	if k := vv.Kind(); k != reflect.Slice && k != reflect.Array {
		return fmt.Errorf("non-array type: %s", k)
	}

	for i := 0; i < vv.Len(); i++ {
		vi := vv.Index(i).Interface()
		if err := validator.Valid(vi, param); err != nil {
			return fmt.Errorf("invalid index %d: %s", i, err)
		}
	}
	return nil
}
