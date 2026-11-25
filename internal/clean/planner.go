// Package clean turns scan results into a plan and runs it with safeguards.
package clean

import (
	"fmt"
	"sort"
	"time"

	"github.com/LoicRU/devreclaim/internal/core"
)

// PlanOptions selects what the plan keeps.
type PlanOptions struct {
	// OlderThan keeps only candidates last modified at least this long ago.
	// Candidates with no modification date (caches) are not filtered by age.
	OlderThan time.Duration
	// IncludeMedium also keeps candidates whose risk is medium or high.
	IncludeMedium bool
	Now           time.Time
}

// Skipped is a candidate left out of the plan, with the reason.
type Skipped struct {
	Label  string `json:"label"`
	Reason string `json:"reason"`
}

// Plan lists what clean would reclaim.
type Plan struct {
	Items        []core.Candidate `json:"items"`
	Skipped      []Skipped        `json:"skipped"`
	ReclaimBytes int64            `json:"reclaimBytes"`
}

// BuildPlan keeps the candidates that can be reclaimed safely, biggest first.
func BuildPlan(candidates []core.Candidate, opts PlanOptions) Plan {
	plan := Plan{Items: []core.Candidate{}, Skipped: []Skipped{}}

	for _, c := range candidates {
		if reason := skipReason(c, opts); reason != "" {
			plan.Skipped = append(plan.Skipped, Skipped{Label: c.Label, Reason: reason})

			continue
		}

		plan.Items = append(plan.Items, c)
		plan.ReclaimBytes += c.ReclaimBytes
	}

	sort.SliceStable(plan.Items, func(i, j int) bool {
		if plan.Items[i].ReclaimBytes != plan.Items[j].ReclaimBytes {
			return plan.Items[i].ReclaimBytes > plan.Items[j].ReclaimBytes
		}

		return plan.Items[i].Label < plan.Items[j].Label
	})

	return plan
}

func skipReason(c core.Candidate, opts PlanOptions) string {
	switch c.Action.Type {
	case "delete-dir", "run-command":
	case "wsl-compact":
		return "WSL compaction is not available in clean yet"
	default:
		return "no cleaning action for this item"
	}

	if !c.Regenerable || c.ReclaimBytes <= 0 {
		if c.ReclaimNote != "" {
			return c.ReclaimNote
		}

		return "not counted as reclaimable"
	}

	if (c.Risk == core.RiskMedium || c.Risk == core.RiskHigh) && !opts.IncludeMedium {
		return fmt.Sprintf("risk %s: add --include-medium-risk to clean it", c.Risk)
	}

	if opts.OlderThan > 0 && !c.LastModified.IsZero() && opts.Now.Sub(c.LastModified) < opts.OlderThan {
		return "modified more recently than --older-than"
	}

	return ""
}
