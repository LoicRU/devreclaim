package clean

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestJournalWritesOneJSONLinePerEntry(t *testing.T) {
	var buf bytes.Buffer

	j := NewJournal(&buf)

	for _, label := range []string{"a", "b"} {
		if err := j.Write(Entry{Time: now, Label: label, Result: "done"}); err != nil {
			t.Fatal(err)
		}
	}

	sc := bufio.NewScanner(&buf)
	n := 0

	for sc.Scan() {
		var e Entry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatalf("invalid line %q: %v", sc.Text(), err)
		}

		n++
	}

	if n != 2 {
		t.Fatalf("expected 2 lines, got %d", n)
	}
}

func TestOpenJournalCreatesDatedFileAndAppends(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	day := time.Date(2026, 11, 25, 9, 0, 0, 0, time.UTC)

	for range 2 {
		j, err := OpenJournal(dir, day)
		if err != nil {
			t.Fatal(err)
		}

		if err = j.Write(Entry{Time: day, Label: "x", Result: "done"}); err != nil {
			t.Fatal(err)
		}

		if err = j.Close(); err != nil {
			t.Fatal(err)
		}
	}

	data, err := os.ReadFile(filepath.Join(dir, "2026-11-25.jsonl"))
	if err != nil {
		t.Fatal(err)
	}

	if bytes.Count(data, []byte("\n")) != 2 {
		t.Fatalf("expected 2 appended lines, got %q", data)
	}
}

func TestNilJournalIsHarmless(t *testing.T) {
	var j *Journal

	if err := j.Write(Entry{}); err != nil || j.Close() != nil || j.Path() != "" {
		t.Fatal("a nil journal must do nothing")
	}
}
