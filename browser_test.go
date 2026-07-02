package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// deadPID returns a pid that is almost certainly not running. Kernel pids are
// assigned low and sequentially; a value near the max is safe for a unit test.
func deadPID(t *testing.T) int {
	t.Helper()
	// 99999 is above the default macOS/Linux pid ceiling for normal assignment.
	pid := 99999
	if processAlive(pid) {
		t.Skipf("pid %d unexpectedly alive, skipping stale-lock test", pid)
	}
	return pid
}

func writeSingletonLock(t *testing.T, dir, target string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, "SingletonLock")
	if err := os.Symlink(target, path); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	return path
}

func TestRemoveStaleSingletonLock_RemovesDeadPidLock(t *testing.T) {
	dir := t.TempDir()
	host, _ := os.Hostname()
	lock := writeSingletonLock(t, dir, host+"-"+strconv.Itoa(deadPID(t)))

	removeStaleSingletonLock(dir)

	if _, err := os.Lstat(lock); !os.IsNotExist(err) {
		t.Errorf("stale lock should be removed, lstat err = %v", err)
	}
}

func TestRemoveStaleSingletonLock_KeepsLiveLocalLock(t *testing.T) {
	dir := t.TempDir()
	host, _ := os.Hostname()
	// Our own pid is guaranteed alive and local — the lock must survive.
	lock := writeSingletonLock(t, dir, host+"-"+strconv.Itoa(os.Getpid()))

	removeStaleSingletonLock(dir)

	if _, err := os.Lstat(lock); err != nil {
		t.Errorf("live lock should be kept, lstat err = %v", err)
	}
}

func TestRemoveStaleSingletonLock_RemovesOtherHostLock(t *testing.T) {
	dir := t.TempDir()
	// A pid that happens to be alive here but under a different host name is
	// meaningless locally and must be treated as stale.
	lock := writeSingletonLock(t, dir, "some-other-host-"+strconv.Itoa(os.Getpid()))

	removeStaleSingletonLock(dir)

	if _, err := os.Lstat(lock); !os.IsNotExist(err) {
		t.Errorf("other-host lock should be removed, lstat err = %v", err)
	}
}

func TestRemoveStaleSingletonLock_MissingLockIsNoop(t *testing.T) {
	dir := t.TempDir()
	// No SingletonLock present: must not panic or error.
	removeStaleSingletonLock(dir)
}

// writePrefs writes a minimal Chromium Preferences file with the given exit_type
// under <dir>/Default/Preferences and returns the file path.
func writePrefs(t *testing.T, dir, exitType string) string {
	t.Helper()
	defaultDir := filepath.Join(dir, "Default")
	if err := os.MkdirAll(defaultDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	prefs := map[string]any{
		"profile": map[string]any{"exit_type": exitType},
		"other":   "keep me",
	}
	data, err := json.Marshal(prefs)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	path := filepath.Join(defaultDir, "Preferences")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write prefs: %v", err)
	}
	return path
}

// writeSessionFile creates <dir>/Default/Sessions/<name> with dummy content.
func writeSessionFile(t *testing.T, dir, name string) string {
	t.Helper()
	sessionsDir := filepath.Join(dir, "Default", "Sessions")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	path := filepath.Join(sessionsDir, name)
	if err := os.WriteFile(path, []byte("dummy"), 0o600); err != nil {
		t.Fatalf("write session file: %v", err)
	}
	return path
}

func TestClearChromiumCrashState_RemovesSessionAndTabFiles(t *testing.T) {
	dir := t.TempDir()
	session := writeSessionFile(t, dir, "Session_123")
	tabs := writeSessionFile(t, dir, "Tabs_123")
	// A non-session file in the same directory must be left untouched.
	keep := writeSessionFile(t, dir, "Apps_123")
	writePrefs(t, dir, "Crashed")

	clearChromiumCrashState(dir)

	if _, err := os.Stat(session); !os.IsNotExist(err) {
		t.Errorf("Session_123 should be removed, stat err = %v", err)
	}
	if _, err := os.Stat(tabs); !os.IsNotExist(err) {
		t.Errorf("Tabs_123 should be removed, stat err = %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("Apps_123 should be kept, stat err = %v", err)
	}
}

func TestClearChromiumCrashState_ResetsExitType(t *testing.T) {
	dir := t.TempDir()
	prefsPath := writePrefs(t, dir, "Crashed")

	clearChromiumCrashState(dir)

	var prefs map[string]any
	data, err := os.ReadFile(prefsPath)
	if err != nil {
		t.Fatalf("read prefs: %v", err)
	}
	if err := json.Unmarshal(data, &prefs); err != nil {
		t.Fatalf("unmarshal prefs: %v", err)
	}

	profile := prefs["profile"].(map[string]any)
	if profile["exit_type"] != "Normal" {
		t.Errorf("exit_type = %v, want Normal", profile["exit_type"])
	}
	if profile["exited_cleanly"] != true {
		t.Errorf("exited_cleanly = %v, want true", profile["exited_cleanly"])
	}
	// Unrelated keys must survive the rewrite.
	if prefs["other"] != "keep me" {
		t.Errorf("unrelated pref lost: other = %v", prefs["other"])
	}
}

func TestClearChromiumCrashState_MissingProfileIsNoop(t *testing.T) {
	// A fresh profile (no Default dir at all) must not panic or error.
	dir := t.TempDir()
	clearChromiumCrashState(dir)
}

func TestClearChromiumCrashState_UnparsablePrefsIsNoop(t *testing.T) {
	dir := t.TempDir()
	defaultDir := filepath.Join(dir, "Default")
	if err := os.MkdirAll(defaultDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	prefsPath := filepath.Join(defaultDir, "Preferences")
	if err := os.WriteFile(prefsPath, []byte("not json{"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Must not panic; the malformed file is left as-is.
	clearChromiumCrashState(dir)

	data, err := os.ReadFile(prefsPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "not json{" {
		t.Errorf("malformed prefs should be untouched, got %q", string(data))
	}
}
