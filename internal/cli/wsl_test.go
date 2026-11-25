package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/LoicRU/reclaim/internal/core"
)

type fakeScanner struct {
	candidates []core.Candidate
	err        error
}

func (fakeScanner) Name() string            { return "fake" }
func (fakeScanner) Category() core.Category { return core.CategoryWSL }

func (f fakeScanner) Scan(context.Context, core.ScanOptions) (<-chan core.Candidate, error) {
	if f.err != nil {
		return nil, f.err
	}

	ch := make(chan core.Candidate, len(f.candidates))
	for _, c := range f.candidates {
		ch <- c
	}

	close(ch)

	return ch, nil
}

// runWSL executes `reclaim <args>` with a fake scanner and returns stdout.
func runWSL(t *testing.T, s fakeScanner, args ...string) (string, error) {
	t.Helper()

	root := &cobra.Command{Use: "reclaim"}
	root.AddCommand(newWSLCmd(func() core.Scanner { return s }))

	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs(args)

	err := root.Execute()

	return out.String(), err
}

var ubuntu = core.Candidate{
	Label:        "Ubuntu",
	SizeBytes:    120 << 30,
	ReclaimBytes: 70 << 30,
}

func TestWSLStatusText(t *testing.T) {
	out, err := runWSL(t, fakeScanner{candidates: []core.Candidate{ubuntu}}, "wsl", "status")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, want := range []string{"Ubuntu", "120.0 GB", "70.0 GB", "Read-only"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output is missing %q:\n%s", want, out)
		}
	}
}

func TestWSLStatusJSON(t *testing.T) {
	out, err := runWSL(t, fakeScanner{candidates: []core.Candidate{ubuntu}}, "wsl", "status", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got struct {
		SchemaVersion int `json:"schemaVersion"`
		Summary       struct {
			ReclaimableBytes int64 `json:"reclaimableBytes"`
		} `json:"summary"`
	}

	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}

	if got.SchemaVersion != 1 || got.Summary.ReclaimableBytes != 70<<30 {
		t.Fatalf("unexpected report: %+v", got)
	}
}

func TestWSLStatusWithoutDistro(t *testing.T) {
	out, err := runWSL(t, fakeScanner{}, "wsl", "status")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(out, "No WSL distribution found") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestWSLStatusReturnsScannerError(t *testing.T) {
	_, err := runWSL(t, fakeScanner{err: errors.New("boom")}, "wsl", "status")
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected the scanner error, got %v", err)
	}
}

func TestWSLStatusRejectsArguments(t *testing.T) {
	if _, err := runWSL(t, fakeScanner{}, "wsl", "status", "extra"); err == nil {
		t.Fatal("expected an error for an unexpected argument")
	}
}
