// Package winfile wraps the few Windows system calls devreclaim needs:
// administrator check, free disk space and the sparse file attribute.
package winfile

import "errors"

// ErrUnsupported is returned where the call only exists on Windows.
var ErrUnsupported = errors.New("only supported on Windows")
