//go:build windows

package zenvik

import (
	"syscall"
	"unsafe"
)

var procGetDiskFreeSpaceEx = syscall.NewLazyDLL("kernel32.dll").NewProc("GetDiskFreeSpaceExW")

// freeBytes reports the bytes available to the caller in dir.
func freeBytes(dir string) (uint64, bool) {
	p, err := syscall.UTF16PtrFromString(dir)
	if err != nil {
		return 0, false
	}
	var avail uint64
	r, _, _ := procGetDiskFreeSpaceEx.Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&avail)), 0, 0)
	return avail, r != 0
}
