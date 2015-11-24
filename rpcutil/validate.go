package rpcutil

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"

	"gopkg.in/validator.v2"
	"net/mail"
	"strconv"
	"strings"
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
	validator.SetValidationFunc("lens", ValidateLens)
	validator.SetValidationFunc("email", ValidateEmail)
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
		return validator.ErrUnsupported
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

// equalsInts takes an int64 and makes sure it finds that value in a array
// of string int64's otherwise it returns false. It returns an error if an
// unparseable string is sent
func equalsInts(a int64, bs []string) (bool, error) {
	for _, v := range bs {
		b, err := strconv.ParseInt(v, 0, 64)
		if err != nil {
			return false, validator.ErrBadParameter
		}
		if b == a {
			return true, nil
		}
	}
	return false, nil
}

// equalsUints takes an uint64 and makes sure it finds that value in a array
// of string uint64's otherwise it returns false. It returns an error if an
// unparseable string is sent
func equalsUints(a uint64, bs []string) (bool, error) {
	for _, v := range bs {
		b, err := strconv.ParseUint(v, 0, 64)
		if err != nil {
			return false, validator.ErrBadParameter
		}
		if b == a {
			return true, nil
		}
	}
	return false, nil
}

// equalsFloats takes a float64 and makes sure it finds that value in a array
// of string float64's otherwise it returns false. It returns an error if an
// unparseable string is sent
func equalsFloats(a float64, bs []string) (bool, error) {
	for _, v := range bs {
		b, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return false, validator.ErrBadParameter
		}
		if b == a {
			return true, nil
		}
	}
	return false, nil
}

// ValidateLens validates the length of a string or slice and errors if the
// length is not equal to a passed in option. For numbers it verifies that the
// number equals one of the passed values. For example
//
//	validator:"lens=2|0"
func ValidateLens(v interface{}, param string) error {
	p := strings.Split(param, "|")
	if len(p) == 0 {
		return nil
	}
	st := reflect.ValueOf(v)
	switch st.Kind() {
	case reflect.String:
		if valid, err := equalsInts(int64(len(st.String())), p); !valid {
			if err == nil {
				err = validator.ErrLen
			}
			return err
		}
	case reflect.Slice, reflect.Map, reflect.Array:
		if valid, err := equalsInts(int64(st.Len()), p); !valid {
			if err == nil {
				err = validator.ErrLen
			}
			return err
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if valid, err := equalsInts(st.Int(), p); !valid {
			if err == nil {
				err = validator.ErrLen
			}
			return err
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		if valid, err := equalsUints(st.Uint(), p); !valid {
			if err == nil {
				err = validator.ErrLen
			}
			return err
		}
	case reflect.Float32, reflect.Float64:
		if valid, err := equalsFloats(st.Float(), p); !valid {
			if err == nil {
				err = validator.ErrLen
			}
			return err
		}
	default:
		return validator.ErrUnsupported
	}
	return nil
}

// ValidateEmail validates a string as an email address as defined by RFC 5322
// Behind the scenes it just uses ParseAddress in the src/net/mail package
func ValidateEmail(v interface{}, _ string) error {
	email, ok := v.(string)
	if !ok {
		return validator.ErrUnsupported
	}

	a, err := mail.ParseAddress(email)
	if err == nil && a.Address != email {
		err = fmt.Errorf("email interpreted as \"%s\" but was sent \"%s\"", a.Address, email)
	}
	return err
}
