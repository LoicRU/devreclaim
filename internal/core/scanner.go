package core

import "context"

// ScanOptions carries settings shared by all scanners.
type ScanOptions struct {
	// Roots are the folders the project scanner walks. Other scanners ignore it.
	Roots []string
	// MinSize hides candidates smaller than this many bytes (0 keeps all).
	MinSize int64
}

// Scanner finds candidates in one area. Scanners are read-only.
type Scanner interface {
	Name() string
	Category() Category
	// Scan starts the scan and returns a channel that is closed once every
	// candidate has been sent or ctx is cancelled. A non-nil error means the
	// scan could not start at all.
	Scan(ctx context.Context, opts ScanOptions) (<-chan Candidate, error)
}

// Collect drains ch into a slice.
func Collect(ch <-chan Candidate) []Candidate {
	var out []Candidate
	for c := range ch {
		out = append(out, c)
	}

	return out
}
