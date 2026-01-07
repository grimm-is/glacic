//go:build linux

package ntp

import (
	"syscall"
	"time"
	"unsafe"
)

// setSystemTime sets the system time using settimeofday syscall.
// Only works on Linux with appropriate privileges (CAP_SYS_TIME).
func setSystemTime(t time.Time) error {
	tv := syscall.Timeval{
		Sec:  t.Unix(),
		Usec: t.UnixMicro() % 1000000,
	}
	_, _, errno := syscall.Syscall(syscall.SYS_SETTIMEOFDAY, uintptr(unsafe.Pointer(&tv)), 0, 0)
	if errno != 0 {
		return errno
	}
	return nil
}
