//go:build windows && (amd64 || arm64)

package trash

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"
)

const (
	foDelete = 3

	fofSilent          = 0x0004
	fofNoConfirmation  = 0x0010
	fofAllowUndo       = 0x0040
	fofNoErrorUI       = 0x0400
	fofWantNukeWarning = 0x4000
)

// shFileOpStruct mirrors SHFILEOPSTRUCTW for 64-bit Windows.
type shFileOpStruct struct {
	Hwnd         uintptr
	Func         uint32
	From         *uint16
	To           *uint16
	Flags        uint16
	Aborted      int32
	NameMappings uintptr
	Title        *uint16
}

var procSHFileOperation = syscall.NewLazyDLL("shell32.dll").NewProc("SHFileOperationW")

// Move sends path to the Recycle Bin. FOF_WANTNUKEWARNING keeps Windows from
// silently deleting for good a folder that is too big for the bin: it asks
// instead, and declining makes Move return an error.
func Move(path string) error {
	from, err := syscall.UTF16FromString(path)
	if err != nil {
		return err
	}

	from = append(from, 0) // the list of paths ends with a double NUL

	op := shFileOpStruct{
		Func:  foDelete,
		From:  &from[0],
		Flags: fofAllowUndo | fofNoConfirmation | fofNoErrorUI | fofSilent | fofWantNukeWarning,
	}

	//nolint:gosec // calling the Windows shell API requires passing a pointer
	r, _, _ := procSHFileOperation.Call(uintptr(unsafe.Pointer(&op)))
	if r != 0 {
		return fmt.Errorf("recycle bin refused %q (code 0x%x)", path, r)
	}

	if op.Aborted != 0 {
		return errors.New("the operation was cancelled")
	}

	return nil
}
