//go:build darwin || linux

package processlock

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestLockAcrossProcesses(t *testing.T) {
	if path := os.Getenv("WASYNC_TEST_LOCK"); path != "" {
		lock, err := Acquire(path)
		if lock != nil {
			lock.Close()
		}
		if errors.Is(err, ErrBusy) {
			os.Exit(42)
		}
		os.Exit(1)
	}
	path := filepath.Join(t.TempDir(), "device.lock")
	lock, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	cmd := exec.Command(os.Args[0], "-test.run=^TestLockAcrossProcesses$")
	cmd.Env = append(os.Environ(), "WASYNC_TEST_LOCK="+path)
	var exit *exec.ExitError
	if err = cmd.Run(); !errors.As(err, &exit) || exit.ExitCode() != 42 {
		t.Fatalf("second process acquired lock: %v", err)
	}
	lock.Close()
	next, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	next.Close()
	if _, err = os.Stat(path); err != nil {
		t.Fatal("lock inode must remain after close")
	}
}
