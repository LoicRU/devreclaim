package core

import "context"

// ScanOptions carries settings shared by all scanners. It is empty for now and
// will grow with the project scanners (roots, minimum size...).
type ScanOptions struct{}

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
