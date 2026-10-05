package ui

import "syscall"

func detached() *syscall.SysProcAttr { return nil }
