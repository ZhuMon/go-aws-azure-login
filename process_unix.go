//go:build !windows

package main

import (
	"errors"
	"syscall"
)

// processAlive reports whether a process with the given pid currently exists.
// signal 0 performs error checking without delivering a signal: nil or EPERM
// (exists but not ours) means alive; ESRCH means no such process.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
