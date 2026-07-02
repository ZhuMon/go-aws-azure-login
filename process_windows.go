//go:build windows

package main

import "golang.org/x/sys/windows"

// processAlive reports whether a process with the given pid currently exists.
// Windows has no signal-0 probe, so open the process with minimal rights: a
// successful open means it exists. In practice this is rarely reached on
// Windows because Chromium there does not use the hostname-pid symlink lock
// that removeStaleSingletonLock inspects.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	windows.CloseHandle(h)
	return true
}
