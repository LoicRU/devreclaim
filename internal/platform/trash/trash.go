// Package trash sends folders to the Windows Recycle Bin.
package trash

import "errors"

// ErrUnsupported is returned on systems without a supported Recycle Bin.
var ErrUnsupported = errors.New("moving to the Recycle Bin is only supported on 64-bit Windows: use --delete")
