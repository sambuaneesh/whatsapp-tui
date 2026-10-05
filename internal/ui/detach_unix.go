//go:build !windows

package ui

import "syscall"

// detached runs a program in its own session, so it isn't tied to the
// app's terminal (and survives the window closing).
func detached() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setsid: true} }
