// Code copied from github.com/lbryio/lbry.go/v2 - MIT License (c) 2016-2020 LBRY Inc.
//
// Copied for liblbry integration without functional changes:
//   - Exact copy of upstream error handling and stack trace functionality
//   - Maintained full compatibility with go-errors library
//   - Preserved all error wrapping and unwrapping behavior
//   - No modifications to core error handling logic

package errors

import (
	stderrors "errors"
	"fmt"

	goerrors "github.com/go-errors/errors"
)

// interop with pkg/errors
type causer interface {
	Cause() error
}

// Err intelligently creates/handles errors, while preserving the stack trace.
// It works with errors from github.com/pkg/errors too.
func Err(err any, fmtParams ...any) error {
	if err == nil {
		return nil
	}

	if _, ok := err.(causer); ok {
		err = fmt.Errorf("%+v", err)
	} else if errString, ok := err.(string); ok && len(fmtParams) > 0 {
		err = fmt.Errorf(errString, fmtParams...)
	}

	return goerrors.Wrap(err, 1)
}

// Wrap calls goerrors.Wrap, in case you want to skip a different amount
func Wrap(err any, skip int) *goerrors.Error {
	if err == nil {
		return nil
	}

	if _, ok := err.(causer); ok {
		err = fmt.Errorf("%+v", err)
	}

	return goerrors.Wrap(err, skip+1)
}

// Unwrap returns the original error that was wrapped
func Unwrap(err error) error {
	if err == nil {
		return nil
	}

	deeper := true
	for deeper {
		deeper = false
		if e, ok := err.(*goerrors.Error); ok {
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
// It fully unwraps both errors (handling both causer and go-errors.Error types)
// before delegating to stdlib errors.Is for the final comparison.
func Is(e error, target error) bool {
	// Fully unwrap both errors before comparison
	e = Unwrap(e)
	target = Unwrap(target)

	// Use standard library errors.Is for the comparison
	return stderrors.Is(e, target)
}

// Prefix prefixes the message of the error with the given string
func Prefix(prefix string, err any) error {
	if err == nil {
		return nil
	}
	return goerrors.WrapPrefix(Err(err), prefix, 0)
}

// Trace returns the stack trace
func Trace(err error) string {
	if err == nil {
		return ""
	}
	return string(Err(err).(*goerrors.Error).Stack())
}

// FullTrace returns the error type, message, and stack trace
func FullTrace(err error) string {
	if err == nil {
		return ""
	}
	return Err(err).(*goerrors.Error).ErrorStack()
}

// Base returns a simple error with no stack trace attached
func Base(format string, a ...any) error {
	return fmt.Errorf(format, a...)
}

// HasTrace checks if error has a trace attached
func HasTrace(err error) bool {
	_, ok := err.(*goerrors.Error)
	return ok
}
