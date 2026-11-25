package clean

import (
	"testing"
	"time"

	"github.com/LoicRU/devreclaim/internal/core"
)

var now = time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)

func cand(label string, reclaim int64) core.Candidate {
	return core.Candidate{
		Label: label, SizeBytes: reclaim, ReclaimBytes: reclaim, Regenerable: true,
		Risk: core.RiskLow, Action: core.ActionSpec{Type: "delete-dir"},
	}
}

func labels(items []core.Candidate) []string {
	var out []string
	for _, c := range items {
		out = append(out, c.Label)
	}

	return out
}

func TestPlanSortsBySizeAndSumsReclaim(t *testing.T) {
	plan := BuildPlan([]core.Candidate{cand("a", 10), cand("b", 30), cand("c", 20)}, PlanOptions{Now: now})

	got := labels(plan.Items)
	if len(got) != 3 || got[0] != "b" || got[1] != "c" || got[2] != "a" {
		t.Fatalf("unexpected order %v", got)
	}

	if plan.ReclaimBytes != 60 {
		t.Fatalf("reclaim = %d", plan.ReclaimBytes)
	}
}

func TestPlanSkipsWhatIsNotReclaimable(t *testing.T) {
	notRebuildable := cand("no-lock", 100)
	notRebuildable.Regenerable = false

	pnpm := cand("pnpm", 0)
	pnpm.ReclaimNote = "shared store"

	wsl := cand("Ubuntu", 100)
	wsl.Action = core.ActionSpec{Type: "wsl-compact"}

	plan := BuildPlan([]core.Candidate{notRebuildable, pnpm, wsl, cand("ok", 5)}, PlanOptions{Now: now})

	if len(plan.Items) != 1 || plan.Items[0].Label != "ok" {
		t.Fatalf("unexpected items %v", labels(plan.Items))
	}

	if len(plan.Skipped) != 3 {
		t.Fatalf("expected 3 skipped, got %+v", plan.Skipped)
	}

	if plan.Skipped[1].Reason != "shared store" {
		t.Fatalf("expected the note as reason, got %q", plan.Skipped[1].Reason)
	}
}

func TestPlanRiskNeedsOptIn(t *testing.T) {
	gradle := cand("Gradle caches", 50)
	gradle.Risk = core.RiskMedium

	if plan := BuildPlan([]core.Candidate{gradle}, PlanOptions{Now: now}); len(plan.Items) != 0 || len(plan.Skipped) != 1 {
		t.Fatalf("medium risk must be skipped by default: %+v", plan)
	}

	if plan := BuildPlan([]core.Candidate{gradle}, PlanOptions{Now: now, IncludeMedium: true}); len(plan.Items) != 1 {
		t.Fatalf("medium risk must be kept with the opt-in: %+v", plan)
	}
}

func TestPlanOlderThan(t *testing.T) {
	old := cand("old", 10)
	old.LastModified = now.Add(-60 * 24 * time.Hour)

	recent := cand("recent", 10)
	recent.LastModified = now.Add(-2 * 24 * time.Hour)

	cache := cand("cache", 10) // no date: not filtered by age

	plan := BuildPlan([]core.Candidate{old, recent, cache}, PlanOptions{Now: now, OlderThan: 30 * 24 * time.Hour})

	got := labels(plan.Items)
	if len(got) != 2 || got[0] != "cache" || got[1] != "old" {
		t.Fatalf("unexpected items %v", got)
	}
}
