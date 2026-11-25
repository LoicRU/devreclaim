// Package report turns scan results into text or JSON.
package report

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"text/tabwriter"
	"time"

	"github.com/LoicRU/reclaim/internal/core"
)

// SchemaVersion is the version of the JSON contract. Never change the shape of
// the output without bumping it (docs/PLAN.md §4.3).
const SchemaVersion = 1

// Summary holds the totals of a report.
type Summary struct {
	TotalBytes       int64 `json:"totalBytes"`
	ReclaimableBytes int64 `json:"reclaimableBytes"`
	Candidates       int   `json:"candidates"`
}

// Report is the public, versioned result of a scan.
type Report struct {
	SchemaVersion int              `json:"schemaVersion"`
	Tool          string           `json:"tool"`
	Version       string           `json:"version"`
	GeneratedAt   time.Time        `json:"generatedAt"`
	Summary       Summary          `json:"summary"`
	Candidates    []core.Candidate `json:"candidates"`
	Warnings      []string         `json:"warnings"`
}

// New builds a report. Candidates are sorted by reclaimable size, biggest
// first, so the most useful lines come on top.
func New(version string, now time.Time, candidates []core.Candidate, warnings []string) Report {
	sorted := slices.Clone(candidates)
	if sorted == nil {
		sorted = []core.Candidate{}
	}

	slices.SortStableFunc(sorted, func(a, b core.Candidate) int {
		return cmp.Or(
			cmp.Compare(b.ReclaimBytes, a.ReclaimBytes),
			cmp.Compare(a.Label, b.Label),
		)
	})

	if warnings == nil {
		warnings = []string{}
	}

	r := Report{
		SchemaVersion: SchemaVersion,
		Tool:          "reclaim",
		Version:       version,
		GeneratedAt:   now,
		Candidates:    sorted,
		Warnings:      warnings,
	}

	r.Summary.Candidates = len(sorted)

	for _, c := range sorted {
		r.Summary.TotalBytes += c.SizeBytes
		r.Summary.ReclaimableBytes += c.ReclaimBytes
	}

	return r
}

// WriteJSON writes the report as indented JSON.
func (r Report) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")

	return enc.Encode(r)
}

// WriteText writes the report as a table followed by warnings and totals.
func (r Report) WriteText(w io.Writer) error {
	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)

	fmt.Fprintln(tw, "NAME\tSIZE\tRECLAIMABLE (est.)")

	for _, c := range r.Candidates {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", c.Label, HumanSize(c.SizeBytes), HumanSize(c.ReclaimBytes))
	}

	if err := tw.Flush(); err != nil {
		return err
	}

	for _, c := range r.Candidates {
		for _, msg := range c.Warnings {
			fmt.Fprintf(w, "! %s: %s\n", c.Label, msg)
		}
	}

	for _, msg := range r.Warnings {
		fmt.Fprintf(w, "! %s\n", msg)
	}

	_, err := fmt.Fprintf(w, "\nTotal: %s, reclaimable (estimate): %s\n",
		HumanSize(r.Summary.TotalBytes), HumanSize(r.Summary.ReclaimableBytes))

	return err
}

// HumanSize formats a byte count the way Windows does (1024-based units).
func HumanSize(b int64) string {
	const unit = 1024

	if b < unit {
		return fmt.Sprintf("%d B", b)
	}

	size := float64(b)

	for _, suffix := range []string{"KB", "MB", "GB", "TB"} {
		size /= unit
		if size < unit || suffix == "TB" {
			return fmt.Sprintf("%.1f %s", size, suffix)
		}
	}

	return fmt.Sprintf("%d B", b) // unreachable
}
