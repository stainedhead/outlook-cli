//go:build !unix

package policyfile

import (
	"errors"
	"os"
)

var errUnsupported = errors.New("policy ownership check is not supported on this platform")

func realEUID() uint32                        { return ^uint32(0) }
func realLstat(string) (statInfo, error)      { return statInfo{}, errUnsupported }
func realFstat(*os.File) (statInfo, error)    { return statInfo{}, errUnsupported }
func openNoFollow(p string) (*os.File, error) { return nil, errUnsupported }
