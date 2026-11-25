package core

import "testing"

func TestNewIDIsStable(t *testing.T) {
	a := NewID(CategoryWSL, "Ubuntu")
	b := NewID(CategoryWSL, "Ubuntu")

	if a != b {
		t.Fatalf("expected a stable ID, got %q and %q", a, b)
	}

	if len(a) != 12 {
		t.Fatalf("expected a 12 character ID, got %q", a)
	}
}

func TestNewIDDependsOnCategoryAndKey(t *testing.T) {
	base := NewID(CategoryWSL, "Ubuntu")

	if base == NewID(CategoryDocker, "Ubuntu") {
		t.Fatal("expected a different ID for another category")
	}

	if base == NewID(CategoryWSL, "Debian") {
		t.Fatal("expected a different ID for another key")
	}
}

func TestCollect(t *testing.T) {
	ch := make(chan Candidate, 2)
	ch <- Candidate{Label: "a"}
	ch <- Candidate{Label: "b"}
	close(ch)

	got := Collect(ch)
	if len(got) != 2 || got[0].Label != "a" || got[1].Label != "b" {
		t.Fatalf("unexpected result: %+v", got)
	}
}
