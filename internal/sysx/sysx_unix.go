//go:build !windows

package sysx

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// Lock takes an exclusive lock on f, blocking until it is free.
func Lock(f *os.File) error { return syscall.Flock(int(f.Fd()), syscall.LOCK_EX) }

// Unlock releases a lock taken by Lock.
func Unlock(f *os.File) error { return syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }

// Alive reports whether a process with this pid exists.
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// Detach makes cmd outlive the terminal that started it.
func Detach(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
