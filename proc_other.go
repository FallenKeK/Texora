//go:build !windows

package main

import "syscall"

// processAlive — есть ли процесс с таким pid.
func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}
