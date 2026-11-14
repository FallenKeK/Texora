//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

var (
	pOpenProcess        = kernel32.NewProc("OpenProcess")
	pGetExitCodeProcess = kernel32.NewProc("GetExitCodeProcess")
	pCloseHandle        = kernel32.NewProc("CloseHandle")
)

// processAlive — есть ли процесс с таким pid (для автосохранения: чужие копии живой Texora не трогаем).
func processAlive(pid int) bool {
	h, _, err := pOpenProcess.Call(0x1000, 0, uintptr(pid)) // PROCESS_QUERY_LIMITED_INFORMATION
	if h == 0 {
		return err == syscall.Errno(5) // ERROR_ACCESS_DENIED: процесс есть, но открыть нельзя
	}
	defer pCloseHandle.Call(h)
	var code uint32
	if r, _, _ := pGetExitCodeProcess.Call(h, uintptr(unsafe.Pointer(&code))); r == 0 {
		return true
	}
	return code == 259 // STILL_ACTIVE
}
