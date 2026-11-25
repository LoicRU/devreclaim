package wsl

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/LoicRU/reclaim/internal/core"
)

const regSample = "\r\n" +
	`HKEY_CURRENT_USER\Software\Microsoft\Windows\CurrentVersion\Lxss` + "\r\n" +
	`    DefaultDistribution    REG_SZ    {aaaa}` + "\r\n" +
	"\r\n" +
	`HKEY_CURRENT_USER\Software\Microsoft\Windows\CurrentVersion\Lxss\{aaaa}` + "\r\n" +
	`    State    REG_DWORD    0x1` + "\r\n" +
	`    DistributionName    REG_SZ    Ubuntu 22.04` + "\r\n" +
	`    Version    REG_DWORD    0x2` + "\r\n" +
	`    BasePath    REG_SZ    \\?\C:\Users\loicr\AppData\Local\Packages\Ubuntu\LocalState` + "\r\n" +
	"\r\n" +
	`HKEY_CURRENT_USER\Software\Microsoft\Windows\CurrentVersion\Lxss\{aaaa}\Nested` + "\r\n" +
	`    Something    REG_SZ    ignored` + "\r\n" +
	"\r\n" +
	`HKEY_CURRENT_USER\Software\Microsoft\Windows\CurrentVersion\Lxss\{bbbb}` + "\r\n" +
	`    DistributionName    REG_SZ    Old1` + "\r\n" +
	`    Version    REG_DWORD    0x1` + "\r\n" +
	`    BasePath    REG_SZ    C:\wsl\old1\` + "\r\n"

func TestParseRegQuery(t *testing.T) {
	got := ParseRegQuery(regSample)

	want := []Distro{
		{Name: "Ubuntu 22.04", BasePath: `C:\Users\loicr\AppData\Local\Packages\Ubuntu\LocalState`, Version: 2},
		{Name: "Old1", BasePath: `C:\wsl\old1\`, Version: 1},
	}

	if len(got) != len(want) {
		t.Fatalf("got %d distros, want %d: %+v", len(got), len(want), got)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("distro %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParseRegQueryEmpty(t *testing.T) {
	if got := ParseRegQuery(""); len(got) != 0 {
		t.Fatalf("expected no distro, got %+v", got)
	}
}

func TestVHDXPath(t *testing.T) {
	tests := map[string]string{
		`C:\wsl\ubuntu`:  `C:\wsl\ubuntu\ext4.vhdx`,
		`C:\wsl\ubuntu\`: `C:\wsl\ubuntu\ext4.vhdx`,
	}

	for base, want := range tests {
		if got := (Distro{BasePath: base}).VHDXPath(); got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	}
}

func TestParseDfUsed(t *testing.T) {
	out := "Filesystem          1-blocks        Used   Available Capacity Mounted on\n" +
		"/dev/sdc       1081101176832 51619905536 975029272576       6% /\n"

	got, err := ParseDfUsed(out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got != 51619905536 {
		t.Fatalf("got %d, want 51619905536", got)
	}
}

func TestParseDfUsedErrors(t *testing.T) {
	for _, out := range []string{"", "Filesystem 1-blocks Used\n", "h\n/dev/sdc 1 x 3 4% /\n", "h\n/dev/sdc 1\n"} {
		if _, err := ParseDfUsed(out); err == nil {
			t.Fatalf("expected an error for %q", out)
		}
	}
}

// fakeRunner answers commands from a table keyed by the joined command line.
type fakeRunner struct {
	answers map[string]fakeAnswer
}

type fakeAnswer struct {
	stdout string
	stderr string
	err    error
}

func (f fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, []byte, error) {
	key := name + " " + strings.Join(args, " ")

	a, ok := f.answers[key]
	if !ok {
		return nil, nil, errors.New("unexpected command: " + key)
	}

	return []byte(a.stdout), []byte(a.stderr), a.err
}

const (
	regCmd    = `reg.exe query ` + lxssKey + ` /s`
	dfUbuntu  = `wsl.exe -d Ubuntu 22.04 -u root -- df -P -B1 /`
	dfBroken  = `wsl.exe -d Broken -u root -- df -P -B1 /`
	dfDocker  = `wsl.exe -d docker-desktop -u root -- df -P -B1 /`
	dfHeader  = "Filesystem 1-blocks Used Available Capacity Mounted on\n"
	gib       = int64(1) << 30
	ubuntuVHD = `C:\u\ext4.vhdx`
)

func reg(entries ...string) string {
	out := ""

	for _, e := range entries {
		out += `HKEY_CURRENT_USER\x\Lxss\{id}` + "\n" + e
	}

	return out
}

func entry(name, base string, version string) string {
	return "    DistributionName    REG_SZ    " + name + "\n" +
		"    BasePath    REG_SZ    " + base + "\n" +
		"    Version    REG_DWORD    " + version + "\n"
}

func scan(t *testing.T, s *Scanner) []core.Candidate {
	t.Helper()

	ch, err := s.Scan(context.Background(), core.ScanOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return core.Collect(ch)
}

func TestScanEstimatesReclaimableSpace(t *testing.T) {
	r := fakeRunner{answers: map[string]fakeAnswer{
		regCmd:   {stdout: reg(entry("Ubuntu 22.04", `\\?\C:\u`, "0x2"))},
		dfUbuntu: {stdout: dfHeader + "/dev/sdc 1000 " + "53687091200" + " 1 6% /\n"}, // 50 GiB used
	}}

	s := &Scanner{Runner: r, FileSize: func(path string) (int64, error) {
		if path != ubuntuVHD {
			t.Fatalf("unexpected path %q", path)
		}

		return 120 * gib, nil
	}}

	got := scan(t, s)
	if len(got) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(got))
	}

	c := got[0]
	if c.SizeBytes != 120*gib || c.ReclaimBytes != 70*gib {
		t.Fatalf("size %d / reclaim %d, want %d / %d", c.SizeBytes, c.ReclaimBytes, 120*gib, 70*gib)
	}

	if c.Category != core.CategoryWSL || c.Kind != "wsl-vhdx" || c.Label != "Ubuntu 22.04" || c.Path != ubuntuVHD {
		t.Fatalf("unexpected candidate: %+v", c)
	}

	if c.ReclaimNote == "" || !c.Action.NeedsAdmin || len(c.Warnings) != 0 {
		t.Fatalf("unexpected note/action/warnings: %+v", c)
	}
}

func TestScanNeverReportsNegativeReclaim(t *testing.T) {
	r := fakeRunner{answers: map[string]fakeAnswer{
		regCmd:   {stdout: reg(entry("Ubuntu 22.04", `C:\u`, "0x2"))},
		dfUbuntu: {stdout: dfHeader + "/dev/sdc 1000 " + "214748364800" + " 1 6% /\n"}, // 200 GiB used
	}}

	s := &Scanner{Runner: r, FileSize: func(string) (int64, error) { return 100 * gib, nil }}

	if got := scan(t, s); got[0].ReclaimBytes != 0 {
		t.Fatalf("expected 0 reclaimable bytes, got %d", got[0].ReclaimBytes)
	}
}

func TestScanWSL1HasNothingToCompact(t *testing.T) {
	r := fakeRunner{answers: map[string]fakeAnswer{
		regCmd: {stdout: reg(entry("Old1", `C:\wsl\old1`, "0x1"))},
	}}

	s := &Scanner{Runner: r, FileSize: func(string) (int64, error) { t.Fatal("must not stat a WSL1 distro"); return 0, nil }}

	got := scan(t, s)
	if got[0].Kind != "wsl1-distro" || got[0].ReclaimBytes != 0 || len(got[0].Warnings) != 1 {
		t.Fatalf("unexpected candidate: %+v", got[0])
	}
}

func TestScanKeepsGoingWhenOneDistroFails(t *testing.T) {
	r := fakeRunner{answers: map[string]fakeAnswer{
		regCmd: {stdout: reg(
			entry("Broken", `C:\b`, "0x2"),
			entry("Ubuntu 22.04", `C:\u`, "0x2"),
		)},
		dfBroken: {err: errors.New("exit status 1"), stderr: "There is no distribution"},
		dfUbuntu: {stdout: dfHeader + "/dev/sdc 1000 " + "1073741824" + " 1 6% /\n"},
	}}

	s := &Scanner{Runner: r, FileSize: func(string) (int64, error) { return 10 * gib, nil }}

	got := scan(t, s)
	if len(got) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(got))
	}

	if len(got[0].Warnings) != 1 || !strings.Contains(got[0].Warnings[0], "There is no distribution") {
		t.Fatalf("expected a warning for the broken distro, got %+v", got[0].Warnings)
	}

	if got[0].ReclaimBytes != 0 || got[1].ReclaimBytes != 9*gib {
		t.Fatalf("reclaim bytes: %d and %d", got[0].ReclaimBytes, got[1].ReclaimBytes)
	}
}

func TestScanWarnsAboutDockerDesktop(t *testing.T) {
	r := fakeRunner{answers: map[string]fakeAnswer{
		regCmd:   {stdout: reg(entry("docker-desktop", `C:\d`, "0x2"))},
		dfDocker: {stdout: dfHeader + "/dev/sdc 1000 1000 1 6% /\n"},
	}}

	s := &Scanner{Runner: r, FileSize: func(string) (int64, error) { return gib, nil }}

	got := scan(t, s)
	if len(got[0].Warnings) != 1 || !strings.Contains(got[0].Warnings[0], "Docker Desktop") {
		t.Fatalf("expected a Docker Desktop warning, got %+v", got[0].Warnings)
	}
}

func TestScanFailsWhenRegistryIsUnreadable(t *testing.T) {
	r := fakeRunner{answers: map[string]fakeAnswer{
		regCmd: {err: errors.New("exit status 1"), stderr: "ERROR: key not found"},
	}}

	_, err := (&Scanner{Runner: r}).Scan(context.Background(), core.ScanOptions{})
	if err == nil || !strings.Contains(err.Error(), "key not found") {
		t.Fatalf("expected a registry error, got %v", err)
	}
}

func TestScannerMetadata(t *testing.T) {
	s := New(fakeRunner{})

	if s.Name() != "wsl" || s.Category() != core.CategoryWSL {
		t.Fatalf("unexpected metadata: %q %q", s.Name(), s.Category())
	}
}
