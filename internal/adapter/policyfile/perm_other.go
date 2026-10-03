//go:build !unix

package policyfile

import "errors"

// writableByMe cannot be determined on this platform; fail closed. Native
// Windows is out of scope for outlook.
func writableByMe(string) (bool, error) {
	return false, errors.New("policy file permission check is not supported on this platform")
}
