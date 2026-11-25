package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/LoicRU/devreclaim/internal/scanners/wsl"
	"github.com/LoicRU/devreclaim/internal/wslcompact"
)

type compactEnv struct {
	ran    []string
	admin  bool
	sizes  []int64
	logDir string
	fail   string
}

type compactRunner struct{ e *compactEnv }

func (r compactRunner) Run(_ context.Context, name string, args ...string) ([]byte, []byte, error) {
	line := strings.Join(append([]string{name}, args...), " ")
	r.e.ran = append(r.e.ran, line)

	if r.e.fail != "" && strings.Contains(line, r.e.fail) {
		return nil, []byte("nope"), errors.New("exit 1")
	}

	return nil, nil, nil
}

func newCompactEnv(t *testing.T) *compactEnv {
	t.Helper()

	return &compactEnv{admin: true, sizes: []int64{20 << 30, 20 << 30, 8 << 30}, logDir: filepath.Join(t.TempDir(), "logs")}
}

func (e *compactEnv) deps() wslCompactDeps {
	return wslCompactDeps{
		distros: func(context.Context) ([]wsl.Distro, error) {
			return []wsl.Distro{
				{Name: "Ubuntu", BasePath: `C:\wsl\u`, Version: 2},
				{Name: "docker-desktop", BasePath: `C:\wsl\d`, Version: 2},
			}, nil
		},
		compactor: func() *wslcompact.Compactor {
			return &wslcompact.Compactor{
				Runner: compactRunner{e},
				FileSize: func(string) (int64, error) {
					v := e.sizes[0]
					if len(e.sizes) > 1 {
						e.sizes = e.sizes[1:]
					}

					return v, nil
				},
				FreeSpace:   func(string) (int64, error) { return 500 << 30, nil },
				IsAdmin:     func() bool { return e.admin },
				IsSparse:    func(string) bool { return false },
				WriteScript: func(string) (string, func(), error) { return "s.txt", func() {}, nil },
				Sleep:       func(time.Duration) {},
			}
		},
		logDir: func() string { return e.logDir },
		now:    func() time.Time { return time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC) },
	}
}

func runCompact(t *testing.T, d wslCompactDeps, stdin string, args ...string) (string, error) {
	t.Helper()

	root := &cobra.Command{Use: "devreclaim"}
	w := &cobra.Command{Use: "wsl"}
	w.AddCommand(newWSLCompactCmd(d))
	root.AddCommand(w)

	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(&bytes.Buffer{})
	root.SetIn(strings.NewReader(stdin))
	root.SetArgs(append([]string{"wsl", "compact"}, args...))

	err := root.Execute()

	return out.String(), err
}

func TestCompactNeedsDistroOrAll(t *testing.T) {
	e := newCompactEnv(t)

	if _, err := runCompact(t, e.deps(), "", "--method", "optimize-vhd"); err == nil {
		t.Fatal("expected an error without --distro or --all")
	}

	if _, err := runCompact(t, e.deps(), "", "--distro", "Ubuntu", "--all"); err == nil {
		t.Fatal("--distro with --all must fail")
	}
}

func TestCompactUnknownDistroListsTheOnesThatExist(t *testing.T) {
	e := newCompactEnv(t)

	_, err := runCompact(t, e.deps(), "", "--distro", "Nope")
	if err == nil || !strings.Contains(err.Error(), "Ubuntu") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompactDryRunRunsNothing(t *testing.T) {
	e := newCompactEnv(t)

	out, err := runCompact(t, e.deps(), "", "--distro", "ubuntu", "--method", "optimize-vhd")
	if err != nil {
		t.Fatal(err)
	}

	if len(e.ran) != 0 {
		t.Fatalf("a dry run must not run any command: %v", e.ran)
	}

	if _, err = os.Stat(e.logDir); err == nil {
		t.Fatal("a dry run must not create the journal")
	}

	for _, want := range []string{"Ubuntu", "optimize-vhd", "wsl --shutdown", "Dry run"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output is missing %q:\n%s", want, out)
		}
	}
}

func TestCompactWithoutAdminExitsWithCode3(t *testing.T) {
	e := newCompactEnv(t)
	e.admin = false

	_, err := runCompact(t, e.deps(), "", "--distro", "Ubuntu", "--method", "optimize-vhd", "--apply", "--yes")

	var exit *ExitError
	if !errors.As(err, &exit) || exit.Code != 3 || len(e.ran) != 0 {
		t.Fatalf("expected exit code 3 and no command, got %v / %v", err, e.ran)
	}
}

func TestCompactApplyCompactsAndLogs(t *testing.T) {
	e := newCompactEnv(t)

	out, err := runCompact(t, e.deps(), "", "--distro", "Ubuntu", "--method", "optimize-vhd", "--apply", "--yes")
	if err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out)
	}

	if !strings.Contains(out, "reclaimed 12.0 GB") {
		t.Fatalf("unexpected output:\n%s", out)
	}

	data, err := os.ReadFile(filepath.Join(e.logDir, "2026-11-25.jsonl"))
	if err != nil || !strings.Contains(string(data), `"action":"wsl-compact"`) {
		t.Fatalf("journal = %q (%v)", data, err)
	}
}

func TestCompactConfirmationCanCancel(t *testing.T) {
	e := newCompactEnv(t)

	out, err := runCompact(t, e.deps(), "n\n", "--distro", "Ubuntu", "--method", "optimize-vhd", "--apply")
	if err != nil || len(e.ran) != 0 || !strings.Contains(out, "Cancelled") {
		t.Fatalf("answering no must cancel: %v / %v\n%s", err, e.ran, out)
	}
}

func TestCompactAllSkipsDockerDesktop(t *testing.T) {
	e := newCompactEnv(t)

	out, err := runCompact(t, e.deps(), "", "--all", "--method", "optimize-vhd")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out, "skipped docker-desktop") || !strings.Contains(out, "Ubuntu") {
		t.Fatalf("unexpected output:\n%s", out)
	}
}

func TestCompactReportsFailureWithAnError(t *testing.T) {
	e := newCompactEnv(t)
	e.fail = "Optimize-VHD"

	out, err := runCompact(t, e.deps(), "", "--distro", "Ubuntu", "--method", "optimize-vhd", "--apply", "--yes")
	if err == nil || !strings.Contains(out, "! Ubuntu") {
		t.Fatalf("expected a failure, got %v\n%s", err, out)
	}
}

func TestCompactShowsLowGainMessage(t *testing.T) {
	e := newCompactEnv(t)
	e.sizes = []int64{20 << 30, 20 << 30, 20<<30 - 10<<20}

	out, err := runCompact(t, e.deps(), "", "--distro", "Ubuntu", "--method", "optimize-vhd", "--apply", "--yes")
	if err != nil || !strings.Contains(out, "little space was recovered") {
		t.Fatalf("expected the low gain message: %v\n%s", err, out)
	}
}
