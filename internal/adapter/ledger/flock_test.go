package ledger_test

import (
	"os"
	"syscall"
)

func flockEx(f *os.File) error { return syscall.Flock(int(f.Fd()), syscall.LOCK_EX) }
