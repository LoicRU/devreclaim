package clean

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Entry is one line of the journal.
type Entry struct {
	Time     time.Time `json:"time"`
	Category string    `json:"category"`
	Label    string    `json:"label"`
	Path     string    `json:"path,omitempty"`
	Action   string    `json:"action"`
	Mode     string    `json:"mode"`
	Bytes    int64     `json:"bytes"`
	Result   string    `json:"result"`
	Error    string    `json:"error,omitempty"`
}

// Journal appends one JSON line per reclaimed item. A nil Journal drops
// everything, which keeps tests simple.
type Journal struct {
	mu     sync.Mutex
	w      io.Writer
	closer io.Closer
	path   string
}

// NewJournal writes entries to w.
func NewJournal(w io.Writer) *Journal { return &Journal{w: w} }

// OpenJournal appends to dir/YYYY-MM-DD.jsonl, creating the folder if needed.
func OpenJournal(dir string, now time.Time) (*Journal, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create log folder: %w", err)
	}

	path := filepath.Join(dir, now.Format("2006-01-02")+".jsonl")

	//nolint:gosec // the path is built from our own log folder and a date
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open journal: %w", err)
	}

	return &Journal{w: f, closer: f, path: path}, nil
}

// Path returns the journal file ("" when it is not a file).
func (j *Journal) Path() string {
	if j == nil {
		return ""
	}

	return j.path
}

// Write appends one entry.
func (j *Journal) Write(e Entry) error {
	if j == nil {
		return nil
	}

	line, err := json.Marshal(e)
	if err != nil {
		return err
	}

	j.mu.Lock()
	defer j.mu.Unlock()

	_, err = j.w.Write(append(line, '\n'))

	return err
}

// Close closes the underlying file, if any.
func (j *Journal) Close() error {
	if j == nil || j.closer == nil {
		return nil
	}

	return j.closer.Close()
}
