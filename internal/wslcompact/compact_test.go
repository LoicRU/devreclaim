package wslcompact

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/LoicRU/devreclaim/internal/scanners/wsl"
)

const gib = int64(1 << 30)

type fakeRunner struct {
	ran  []string
	fail map[string]error // substring of the command line -> error
	out  map[string]string
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, []byte, error) {
	line := strings.Join(append([]string{name}, args...), " ")
	f.ran = append(f.ran, line)

	for sub, err := range f.fail {
		if strings.Contains(line, sub) {
			return nil, []byte("failed"), err
		}
	}

	for sub, out := range f.out {
		if strings.Contains(line, sub) {
			return []byte(out), nil, nil
		}
	}

	return nil, nil, nil
}

func (f *fakeRunner) ranContaining(sub string) int {
	n := 0

	for _, l := range f.ran {
		if strings.Contains(l, sub) {
			n++
		}
	}

	return n
}

type fixture struct {
	c       *Compactor
	r       *fakeRunner
	sizes   []int64 // successive answers of FileSize (the last one repeats)
	scripts []string
	cleaned int
}

func newFixture() *fixture {
	f := &fixture{r: &fakeRunner{}, sizes: []int64{10 * gib}}

	f.c = &Compactor{
		Runner: f.r,
		FileSize: func(string) (int64, error) {
			v := f.sizes[0]
			if len(f.sizes) > 1 {
				f.sizes = f.sizes[1:]
			}

			return v, nil
		},
		FreeSpace: func(string) (int64, error) { return 100 * gib, nil },
		IsAdmin:   func() bool { return true },
		IsSparse:  func(string) bool { return false },
		WriteScript: func(content string) (string, func(), error) {
			f.scripts = append(f.scripts, content)

			return `C:\tmp\script.txt`, func() { f.cleaned++ }, nil
		},
		Sleep: func(time.Duration) {},
	}

	return f
}

var ubuntu = wsl.Distro{Name: "Ubuntu", BasePath: `C:\wsl\ubuntu`, Version: 2}

func TestPrepareRefusesWhatCannotBeCompacted(t *testing.T) {
	f := newFixture()
	ctx := context.Background()

	if _, err := f.c.Prepare(ctx, wsl.Distro{Name: "docker-desktop", BasePath: `C:\d`, Version: 2}, MethodAuto); err == nil {
		t.Fatal("docker-desktop must be refused")
	}

	if _, err := f.c.Prepare(ctx, wsl.Distro{Name: "Old", BasePath: `C:\o`, Version: 1}, MethodAuto); err == nil {
		t.Fatal("a WSL1 distro must be refused")
	}

	f.c.FreeSpace = func(string) (int64, error) { return gib / 2, nil }

	if _, err := f.c.Prepare(ctx, ubuntu, MethodAuto); err == nil || !strings.Contains(err.Error(), "free disk space") {
		t.Fatalf("expected a free space error, got %v", err)
	}

	f.c.FreeSpace = func(string) (int64, error) { return 100 * gib, nil }
	f.c.IsSparse = func(string) bool { return true }

	if _, err := f.c.Prepare(ctx, ubuntu, MethodAuto); err == nil || !strings.Contains(err.Error(), "sparse") {
		t.Fatalf("expected a sparse explanation, got %v", err)
	}

	if len(f.r.ran) != 0 {
		t.Fatalf("Prepare must not run anything for refused distros: %v", f.r.ran)
	}
}

func TestPrepareNeedsRoomRelativeToDiskSize(t *testing.T) {
	f := newFixture()
	f.sizes = []int64{200 * gib}
	f.c.FreeSpace = func(string) (int64, error) { return 5 * gib, nil } // 10 % of 200 GiB = 20 GiB

	if _, err := f.c.Prepare(context.Background(), ubuntu, MethodOptimize); err == nil {
		t.Fatal("expected a free space error for a big disk")
	}
}

func TestPrepareAutoPicksOptimizeWhenHyperVExists(t *testing.T) {
	f := newFixture()
	f.r.out = map[string]string{"Get-Command Optimize-VHD": "yes\r\n"}

	p, err := f.c.Prepare(context.Background(), ubuntu, MethodAuto)
	if err != nil || p.Method != MethodOptimize {
		t.Fatalf("got %v / %+v", err, p)
	}

	if len(p.Steps) != 4 || p.SizeBefore != 10*gib {
		t.Fatalf("unexpected plan %+v", p)
	}
}

func TestPrepareAutoFallsBackToDiskpart(t *testing.T) {
	f := newFixture()
	f.r.out = map[string]string{"Get-Command Optimize-VHD": "no"}

	p, err := f.c.Prepare(context.Background(), ubuntu, MethodAuto)
	if err != nil || p.Method != MethodDiskpart {
		t.Fatalf("got %v / %+v", err, p)
	}
}

func TestPrepareRejectsUnknownMethodAndNonASCIIDiskpart(t *testing.T) {
	f := newFixture()

	if _, err := f.c.Prepare(context.Background(), ubuntu, "magic"); err == nil {
		t.Fatal("unknown method must fail")
	}

	accent := wsl.Distro{Name: "U", BasePath: `C:\Users\loïc\U`, Version: 2}

	if _, err := f.c.Prepare(context.Background(), accent, MethodDiskpart); err == nil {
		t.Fatal("diskpart with non-ASCII path must fail")
	}

	if _, err := f.c.Prepare(context.Background(), accent, MethodOptimize); err != nil {
		t.Fatalf("Optimize-VHD handles non-ASCII paths: %v", err)
	}
}

func prepared(t *testing.T, f *fixture, m Method) Plan {
	t.Helper()

	p, err := f.c.Prepare(context.Background(), ubuntu, m)
	if err != nil {
		t.Fatal(err)
	}

	return p
}

func TestRunOptimizeStepsInOrderAndGain(t *testing.T) {
	f := newFixture()
	p := prepared(t, f, MethodOptimize)

	f.sizes = []int64{10 * gib, 4 * gib} // before, after

	var steps []string

	res, err := f.c.Run(context.Background(), p, func(s string) { steps = append(steps, s) })
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"fstrim", "--shutdown", "Optimize-VHD"}
	for i, w := range want {
		if i >= len(f.r.ran) || !strings.Contains(f.r.ran[i], w) {
			t.Fatalf("step %d should contain %q, ran %v", i, w, f.r.ran)
		}
	}

	if res.Gain != 6*gib || res.LowGain || len(steps) != 3 {
		t.Fatalf("unexpected result %+v / steps %v", res, steps)
	}
}

func TestRunFlagsLowGain(t *testing.T) {
	f := newFixture()
	p := prepared(t, f, MethodOptimize)
	f.sizes = []int64{10 * gib, 10*gib - 100<<20} // ~1 % gained

	res, err := f.c.Run(context.Background(), p, func(string) {})
	if err != nil || !res.LowGain {
		t.Fatalf("a tiny gain must be flagged: %v / %+v", err, res)
	}
}

func TestRunWithoutAdminRunsNothing(t *testing.T) {
	f := newFixture()
	p := prepared(t, f, MethodOptimize)
	f.c.IsAdmin = func() bool { return false }

	_, err := f.c.Run(context.Background(), p, func(string) {})
	if !errors.Is(err, ErrNeedsAdmin) || len(f.r.ran) != 0 {
		t.Fatalf("got %v, ran %v", err, f.r.ran)
	}
}

func TestFstrimFailureOnlyWarns(t *testing.T) {
	f := newFixture()
	p := prepared(t, f, MethodOptimize)
	f.r.fail = map[string]error{"fstrim": errors.New("exit 1")}
	f.sizes = []int64{10 * gib, 5 * gib}

	res, err := f.c.Run(context.Background(), p, func(string) {})
	if err != nil || len(res.Warnings) != 1 || f.r.ranContaining("Optimize-VHD") != 1 {
		t.Fatalf("fstrim failure must warn and continue: %v / %+v", err, res)
	}
}

func TestShutdownFailureStopsBeforeCompaction(t *testing.T) {
	f := newFixture()
	p := prepared(t, f, MethodOptimize)
	f.r.fail = map[string]error{"--shutdown": errors.New("exit 1")}

	if _, err := f.c.Run(context.Background(), p, func(string) {}); err == nil || f.r.ranContaining("Optimize-VHD") != 0 {
		t.Fatalf("compaction must not start after a failed shutdown: %v", err)
	}
}

func TestOptimizeQuotesThePath(t *testing.T) {
	f := newFixture()
	d := wsl.Distro{Name: "U", BasePath: `C:\it's\U`, Version: 2}

	p, err := f.c.Prepare(context.Background(), d, MethodOptimize)
	if err != nil {
		t.Fatal(err)
	}

	f.sizes = []int64{10 * gib, 5 * gib}

	if _, err = f.c.Run(context.Background(), p, func(string) {}); err != nil {
		t.Fatal(err)
	}

	if f.r.ranContaining(`it''s`) != 1 {
		t.Fatalf("the single quote must be doubled: %v", f.r.ran)
	}
}

func TestDiskpartScriptAndGuaranteedDetach(t *testing.T) {
	f := newFixture()
	p := prepared(t, f, MethodDiskpart)
	f.sizes = []int64{10 * gib, 5 * gib}

	if _, err := f.c.Run(context.Background(), p, func(string) {}); err != nil {
		t.Fatal(err)
	}

	if len(f.scripts) != 2 || f.cleaned != 2 {
		t.Fatalf("expected the compaction script and a detach script, got %d (cleaned %d)", len(f.scripts), f.cleaned)
	}

	for _, want := range []string{`select vdisk file="C:\wsl\ubuntu\ext4.vhdx"`, "attach vdisk readonly", "compact vdisk", "detach vdisk"} {
		if !strings.Contains(f.scripts[0], want) {
			t.Fatalf("script is missing %q:\n%s", want, f.scripts[0])
		}
	}

	if strings.Contains(f.scripts[1], "compact") || !strings.Contains(f.scripts[1], "detach vdisk") {
		t.Fatalf("unexpected detach script:\n%s", f.scripts[1])
	}
}

func TestDiskpartDetachesEvenWhenCompactionFails(t *testing.T) {
	f := newFixture()
	p := prepared(t, f, MethodDiskpart)
	f.r.fail = map[string]error{`diskpart.exe`: errors.New("exit 1")}

	if _, err := f.c.Run(context.Background(), p, func(string) {}); err == nil {
		t.Fatal("expected an error")
	}

	if len(f.scripts) != 2 || !strings.Contains(f.scripts[1], "detach vdisk") {
		t.Fatalf("a detach script must run after a failure: %v", f.scripts)
	}
}

func TestRunWaitsUntilTheDiskIsUnlocked(t *testing.T) {
	f := newFixture()
	p := prepared(t, f, MethodOptimize)
	f.sizes = []int64{10 * gib, 5 * gib}

	checks := 0
	f.c.IsLocked = func(string) bool { checks++; return checks <= 3 }

	if _, err := f.c.Run(context.Background(), p, func(string) {}); err != nil {
		t.Fatal(err)
	}

	if checks != 4 || f.r.ranContaining("Optimize-VHD") != 1 {
		t.Fatalf("expected 4 checks then a compaction, got %d checks / %v", checks, f.r.ran)
	}
}

func TestRunGivesUpWhenTheDiskStaysLocked(t *testing.T) {
	f := newFixture()
	p := prepared(t, f, MethodOptimize)
	f.c.IsLocked = func(string) bool { return true }

	_, err := f.c.Run(context.Background(), p, func(string) {})

	if err == nil || !strings.Contains(err.Error(), "still in use") || f.r.ranContaining("Optimize-VHD") != 0 {
		t.Fatalf("expected a locked disk error and no compaction: %v / %v", err, f.r.ran)
	}
}
