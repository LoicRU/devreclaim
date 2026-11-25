//go:build windows

package winfile

import (
	"path/filepath"
	"syscall"
	"unsafe"
)

const fileAttributeSparseFile = 0x200

var (
	shell32  = syscall.NewLazyDLL("shell32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procIsUserAnAdmin     = shell32.NewProc("IsUserAnAdmin")
	procGetDiskFreeSpaceX = kernel32.NewProc("GetDiskFreeSpaceExW")
)

// IsAdmin reports whether the process runs with an elevated administrator token.
func IsAdmin() bool {
	r, _, _ := procIsUserAnAdmin.Call()

	return r != 0
}

// IsSparse reports whether the file has the NTFS sparse attribute.
func IsSparse(path string) bool {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return false
	}

	attrs, err := syscall.GetFileAttributes(p)
	if err != nil {
		return false
	}

	return attrs&fileAttributeSparseFile != 0
}

// FreeSpace returns the bytes available to the user on the volume holding path.
func FreeSpace(path string) (int64, error) {
	dir, err := syscall.UTF16PtrFromString(filepath.Dir(path))
	if err != nil {
		return 0, err
	}

	var free, total, totalFree uint64

	//nolint:gosec // calling the Windows API requires passing pointers
	r, _, callErr := procGetDiskFreeSpaceX.Call(
		uintptr(unsafe.Pointer(dir)),
		uintptr(unsafe.Pointer(&free)),
		uintptr(unsafe.Pointer(&total)),
		uintptr(unsafe.Pointer(&totalFree)),
	)
	if r == 0 {
		return 0, callErr
	}

	return int64(free), nil //nolint:gosec // a disk size always fits in an int64
}
