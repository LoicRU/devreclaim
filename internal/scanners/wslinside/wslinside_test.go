package wslinside

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/LoicRU/devreclaim/internal/core"
	"github.com/LoicRU/devreclaim/internal/scanners/wsl"
)

const snapOut = `Name      Version   Rev    Tracking       Publisher   Notes
core24    20260824  2124   latest/stable  canonical✓  base
ollama    v0.34.0   137    latest/stable  mz2         disabled
ollama    v0.40.2   141    latest/stable  mz2         disabled
seclists  2025.2    1214   latest/stable  jitpatro    -
snapd     2.76.3    27738  latest/stable  canonical✓  snapd,disabled
snapd     2.77.1    28254  latest/stable  canonical✓  snapd
localapp  1.0       x1     -              -           disabled
`

func TestParseSnapList(t *testing.T) {
	revs := ParseSnapList(snapOut)

	if len(revs) != 6 {
		t.Fatalf("expected 6 revisions (the local one is ignored), got %d: %+v", len(revs), revs)
	}

	if !revs[2].Disabled || revs[2].Name != "ollama" || revs[2].Rev != "141" {
		t.Fatalf("unexpected revision: %+v", revs[2])
	}

	if revs[4].Name != "snapd" || !revs[4].Disabled {
		t.Fatalf("snapd,disabled must be read as disabled: %+v", revs[4])
	}
}

func TestNeverRemovesTheLastRevisionOfASnap(t *testing.T) {
	got := removable(ParseSnapList(snapOut))

	if len(got) != 1 || got[0].Name != "snapd" || got[0].Rev != "27738" {
		t.Fatalf("only the old snapd revision may be removed (ollama has no active revision): %+v", got)
	}
}

type fakeWSL struct {
	mu      sync.Mutex
	answers map[string]string
	ran     []string
}

func (f *fakeWSL) Run(_ context.Context, name string, args ...string) ([]byte, []byte, error) {
	line := strings.Join(append([]string{name}, args...), " ")

	f.mu.Lock()
	f.ran = append(f.ran, line)
	f.mu.Unlock()

	best := ""

	for k := range f.answers {
		if strings.HasSuffix(line, k) && len(k) > len(best) {
			best = k
		}
	}

	if best == "" {
		return nil, nil, errors.New("not found")
	}

	return []byte(f.answers[best]), nil, nil
}

func newScanner(f *fakeWSL) *Scanner {
	return &Scanner{
		Runner: f,
		Distros: func(context.Context) ([]wsl.Distro, error) {
			return []wsl.Distro{
				{Name: "Ubuntu", BasePath: `C:\u`, Version: 2},
				{Name: "docker-desktop", BasePath: `C:\d`, Version: 2},
			}, nil
		},
	}
}

func scanAll(t *testing.T, s *Scanner, minSize int64) map[string]core.Candidate {
	t.Helper()

	ch, err := s.Scan(context.Background(), core.ScanOptions{MinSize: minSize})
	if err != nil {
		t.Fatal(err)
	}

	got := map[string]core.Candidate{}
	for _, c := range core.Collect(ch) {
		got[c.Label] = c
	}

	return got
}

func TestScansCachesAndSnapRevisionsInsideTheDistro(t *testing.T) {
	f := &fakeWSL{answers: map[string]string{
		`sh -c echo "$HOME"; echo "$CARGO_HOME"; echo "$GRADLE_USER_HOME"`: "/home/me\n\n\n",
		"npm config get cache":                             "/home/me/.npm\n",
		"test -d /home/me/.npm":                            "",
		"du -sb /home/me/.npm":                             "5400000000\t/home/me/.npm\n",
		"snap list --all":                                  snapOut,
		"stat -c %s /var/lib/snapd/snaps/snapd_27738.snap": "60000000\n",
	}}

	got := scanAll(t, newScanner(f), 0)

	npm, ok := got["Ubuntu: npm"]
	if !ok || npm.SizeBytes != 5400000000 {
		t.Fatalf("npm cache missing: %+v", got)
	}

	if cmd := strings.Join(npm.Action.Command, " "); cmd != "wsl.exe -d Ubuntu -- npm cache clean --force" {
		t.Fatalf("unexpected clean command: %q", cmd)
	}

	snap, ok := got["Ubuntu: snap snapd r27738"]
	if !ok || snap.SizeBytes != 60000000 {
		t.Fatalf("snap revision missing: %+v", got)
	}

	if cmd := strings.Join(snap.Action.Command, " "); cmd != "wsl.exe -d Ubuntu -u root -- snap remove snapd --revision=27738" {
		t.Fatalf("unexpected snap command: %q", cmd)
	}

	for label := range got {
		if strings.Contains(label, "ollama") || strings.HasPrefix(label, "docker-desktop") {
			t.Fatalf("unexpected candidate %q", label)
		}
	}
}

func TestDistroThatDoesNotAnswerIsSkipped(t *testing.T) {
	if got := scanAll(t, newScanner(&fakeWSL{}), 0); len(got) != 0 {
		t.Fatalf("expected nothing, got %+v", got)
	}
}

func TestListingFailureIsReturnedAsAnError(t *testing.T) {
	s := &Scanner{Distros: func(context.Context) ([]wsl.Distro, error) { return nil, errors.New("no WSL") }}

	if _, err := s.Scan(context.Background(), core.ScanOptions{}); err == nil {
		t.Fatal("expected an error")
	}
}
