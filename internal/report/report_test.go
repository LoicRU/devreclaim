package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/LoicRU/devreclaim/internal/core"
)

const gib = int64(1) << 30

var now = time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)

func sample() []core.Candidate {
	return []core.Candidate{
		{Label: "small", SizeBytes: 2 * gib, ReclaimBytes: gib},
		{Label: "kept", SizeBytes: gib, ReclaimNote: "project active 2 days ago: not counted"},
		{Label: "big", SizeBytes: 100 * gib, ReclaimBytes: 60 * gib, Warnings: []string{"careful"}},
	}
}

func TestNewSortsAndSums(t *testing.T) {
	r := New("1.2.3", now, sample(), nil)

	if r.Candidates[0].Label != "big" {
		t.Fatalf("expected the biggest reclaimable first, got %q", r.Candidates[0].Label)
	}

	if r.Summary != (Summary{TotalBytes: 103 * gib, ReclaimableBytes: 61 * gib, Candidates: 3}) {
		t.Fatalf("unexpected summary: %+v", r.Summary)
	}

	if r.SchemaVersion != 1 || r.Tool != "devreclaim" || r.Version != "1.2.3" {
		t.Fatalf("unexpected header: %+v", r)
	}
}

func TestNewDoesNotModifyItsInput(t *testing.T) {
	in := sample()
	New("v", now, in, nil)

	if in[0].Label != "small" {
		t.Fatal("input slice was reordered")
	}
}

func TestWriteJSONContract(t *testing.T) {
	var buf bytes.Buffer
	if err := New("1.2.3", now, nil, nil).WriteJSON(&buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	for _, key := range []string{"schemaVersion", "tool", "version", "generatedAt", "summary", "candidates", "warnings"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("missing key %q in %s", key, buf.String())
		}
	}

	if got["schemaVersion"] != float64(1) {
		t.Fatalf("unexpected schemaVersion: %v", got["schemaVersion"])
	}

	if list, ok := got["candidates"].([]any); !ok || len(list) != 0 {
		t.Fatalf("expected an empty candidates array, got %v", got["candidates"])
	}
}

func TestWriteText(t *testing.T) {
	var buf bytes.Buffer
	if err := New("v", now, sample(), []string{"global note"}).WriteText(&buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()

	for _, want := range []string{
		"NAME", "RECLAIMABLE",
		"100.0 GB", "60.0 GB",
		"! big: careful",
		"! global note",
		"Total: 103.0 GB, reclaimable (estimate): 61.0 GB",
		"NOTE", "project active 2 days ago: not counted",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output is missing %q:\n%s", want, out)
		}
	}

	if strings.Index(out, "big") > strings.Index(out, "small") {
		t.Fatalf("expected big before small:\n%s", out)
	}
}

func TestHumanSize(t *testing.T) {
	tests := map[int64]string{
		0:                 "0 B",
		1023:              "1023 B",
		1024:              "1.0 KB",
		1536:              "1.5 KB",
		5 * 1024 * 1024:   "5.0 MB",
		gib:               "1.0 GB",
		120*gib + gib/2:   "120.5 GB",
		3 * 1024 * gib:    "3.0 TB",
		2048 * 1024 * gib: "2048.0 TB",
	}

	for in, want := range tests {
		if got := HumanSize(in); got != want {
			t.Fatalf("HumanSize(%d) = %q, want %q", in, got, want)
		}
	}
}
