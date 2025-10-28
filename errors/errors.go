// Code copied from github.com/lbryio/lbry.go/v2 - MIT License (c) 2016-2020 LBRY Inc.
//
// Copied for liblbry integration without functional changes:
//   - Exact copy of upstream error handling and stack trace functionality
//   - Maintained full compatibility with go-errors library
//   - Preserved all error wrapping and unwrapping behavior
//   - No modifications to core error handling logic

package errors

import (
	"fmt"

	"github.com/go-errors/errors"
)

// interop with pkg/errors
type causer interface {
	Cause() error
}

// Err intelligently creates/handles errors, while preserving the stack trace.
// It works with errors from github.com/pkg/errors too.
func Err(err interface{}, fmtParams ...interface{}) error {
	if err == nil {
		return nil
	}

	if _, ok := err.(causer); ok {
		err = fmt.Errorf("%+v", err)
	} else if errString, ok := err.(string); ok && len(fmtParams) > 0 {
		err = fmt.Errorf(errString, fmtParams...)
	}

	return errors.Wrap(err, 1)
}

// Wrap calls errors.Wrap, in case you want to skip a different amount
func Wrap(err interface{}, skip int) *errors.Error {
	if err == nil {
		return nil
	}

	if _, ok := err.(causer); ok {
		err = fmt.Errorf("%+v", err)
	}

	return errors.Wrap(err, skip+1)
}

// Unwrap returns the original error that was wrapped
func Unwrap(err error) error {
	if err == nil {
		return nil
	}

	deeper := true
	for deeper {
		deeper = false
		if e, ok := err.(*errors.Error); ok {
			err = e.Err
			deeper = true
		}
		if c, ok := err.(causer); ok {
			err = c.Cause()
			deeper = true
		}
	}

	return err
}

// Is reports whether any error in e's chain matches target.
// It first tries to unwrap our custom errors to get the underlying cause,
// then uses errors.Is() to check against the target error.
// This function works with both our wrapped errors and standard library errors.
func Is(e error, target error) bool {
	// First try to unwrap our custom errors
	if c, ok := e.(causer); ok {
		e = c.Cause()
	}
	if c, ok := target.(causer); ok {
		target = c.Cause()
	}
	
	// Use standard library errors.Is for the comparison
	return errors.Is(e, target)
}

// Prefix prefixes the message of the error with the given string
func Prefix(prefix string, err interface{}) error {
	if err == nil {
		return nil
	}
	return errors.WrapPrefix(Err(err), prefix, 0)
}

// Trace returns the stack trace
func Trace(err error) string {
	if err == nil {
		return ""
	}
	return string(Err(err).(*errors.Error).Stack())
}

// FullTrace returns the error type, message, and stack trace
func FullTrace(err error) string {
	if err == nil {
		return ""
	}
	return Err(err).(*errors.Error).ErrorStack()
}

// Base returns a simple error with no stack trace attached
func Base(format string, a ...interface{}) error {
	return fmt.Errorf(format, a...)
}

// HasTrace checks if error has a trace attached
func HasTrace(err error) bool {
	_, ok := err.(*errors.Error)
	return ok
}
