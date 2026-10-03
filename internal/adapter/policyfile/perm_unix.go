//go:build unix

package policyfile

import (
	"errors"
	"syscall"
)

var accessFn = syscall.Access

// writableByMe reports whether the current user can write path, using
// access(2) W_OK like core's policy.Load. A path that cannot be examined is an
// error, never "not writable".
func writableByMe(path string) (bool, error) {
	err := accessFn(path, 2) // W_OK
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, syscall.EACCES), errors.Is(err, syscall.EROFS), errors.Is(err, syscall.EPERM):
		return false, nil
	}
	return false, err
}
