//go:build !windows

package winfile

// IsAdmin is always false outside Windows.
func IsAdmin() bool { return false }

// IsSparse is always false outside Windows.
func IsSparse(string) bool { return false }

// FreeSpace is not available outside Windows.
func FreeSpace(string) (int64, error) { return 0, ErrUnsupported }
