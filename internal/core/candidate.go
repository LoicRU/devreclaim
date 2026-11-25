// Package core defines the types shared by every scanner and report.
package core

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// Category groups candidates by the kind of space they occupy.
type Category string

// Known categories (docs/PLAN.md §4.1).
const (
	CategoryProjectArtifacts Category = "project-artifacts"
	CategoryPackageCaches    Category = "package-caches"
	CategoryWSL              Category = "wsl"
	CategoryDocker           Category = "docker"
	CategoryIDECaches        Category = "ide-caches"
	CategoryWindowsTemp      Category = "windows-temp"
)

// Risk tells how careful a user should be before reclaiming a candidate.
type Risk string

// Risk levels, from harmless to potentially destructive.
const (
	RiskSafe   Risk = "safe"
	RiskLow    Risk = "low"
	RiskMedium Risk = "medium"
	RiskHigh   Risk = "high"
)

// ActionSpec describes how a candidate would be reclaimed. Scanners only
// describe actions; nothing is executed in read-only mode.
type ActionSpec struct {
	Type       string   `json:"type"` // "delete-dir" | "run-command" | "wsl-compact"
	Command    []string `json:"command,omitempty"`
	NeedsAdmin bool     `json:"needsAdmin"`
}

// Candidate is one thing that takes disk space and could be reclaimed.
type Candidate struct {
	ID           string     `json:"id"`
	Category     Category   `json:"category"`
	Kind         string     `json:"kind"`
	Path         string     `json:"path,omitempty"`
	Label        string     `json:"label"`
	SizeBytes    int64      `json:"sizeBytes"`
	OnDiskBytes  int64      `json:"onDiskBytes,omitempty"`
	ReclaimBytes int64      `json:"reclaimBytes"`
	ReclaimNote  string     `json:"reclaimNote,omitempty"`
	LastModified time.Time  `json:"lastModified,omitzero"`
	Regenerable  bool       `json:"regenerable"`
	RegenHint    string     `json:"regenHint,omitempty"`
	Risk         Risk       `json:"risk"`
	Reasons      []string   `json:"reasons,omitempty"`
	Warnings     []string   `json:"warnings,omitempty"`
	Action       ActionSpec `json:"action"`
}

// NewID returns a stable identifier derived from the category and a key that
// is unique inside it (a path, a distro name...).
func NewID(category Category, key string) string {
	sum := sha256.Sum256([]byte(string(category) + "\x00" + key))

	return hex.EncodeToString(sum[:])[:12]
}
