//go:build !(windows && (amd64 || arm64))

package trash

// Move is not available on this system.
func Move(string) error { return ErrUnsupported }
