// Package usage marks errors that are the caller's mistake: a bad argument,
// configuration or selection. The command line exits with status 2 for them.
package usage

import (
	"errors"
	"fmt"
)

// Error is a usage error.
type Error struct{ msg string }

func (e *Error) Error() string { return e.msg }

// Errorf formats a usage error.
func Errorf(format string, a ...any) error { return &Error{msg: fmt.Sprintf(format, a...)} }

// Is reports whether err is, or wraps, a usage error.
func Is(err error) bool {
	var u *Error
	return errors.As(err, &u)
}
