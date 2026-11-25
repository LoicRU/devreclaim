package clean

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LoicRU/devreclaim/internal/core"
	"github.com/LoicRU/devreclaim/internal/safety"
)

type calls struct{ trash, remove []string }

// fakeFS moves nothing to a real Recycle Bin: it deletes the folder so the
// executor's "folder is gone" check can pass.
func (c *calls) trashFn(p string) error  { c.trash = append(c.trash, p); return os.RemoveAll(p) }
func (c *calls) removeFn(p string) error { c.remove = append(c.remove, p); return os.RemoveAll(p) }

type fakeRunner struct {
	ran []string
	err error
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, []byte, error) {
	f.ran = append(f.ran, strings.Join(append([]string{name}, args...), " "))

	return nil, []byte("boom"), f.err
}

func tempRoot(t *testing.T) string {
	t.Helper()

	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	return dir
}

func artifact(t *testing.T, root, name string) core.Candidate {
	t.Helper()

	p := filepath.Join(root, name, "node_modules")
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(p, "f.js"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	c := cand(name, 100)
	c.Category, c.Path, c.Kind = core.CategoryProjectArtifacts, p, "node_modules"

	return c
}

func newExec(root string, cl *calls, buf *bytes.Buffer) *Executor {
	return &Executor{
		Roots: []string{root}, Mode: ModeTrash, Trash: cl.trashFn, Remove: cl.removeFn,
		Journal: NewJournal(buf), Runner: &fakeRunner{},
	}
}

func TestTrashModeMovesToTrashAndLogs(t *testing.T) {
	root := tempRoot(t)
	c := artifact(t, root, "web")

	var (
		cl  calls
		buf bytes.Buffer
	)

	res, err := newExec(root, &cl, &buf).Run(context.Background(), []core.Candidate{c})
	if err != nil {
		t.Fatal(err)
	}

	if len(cl.trash) != 1 || len(cl.remove) != 0 {
		t.Fatalf("trash=%v remove=%v", cl.trash, cl.remove)
	}

	if res[0].Status != StatusDone || res[0].Bytes != 100 {
		t.Fatalf("unexpected result %+v", res[0])
	}

	if !strings.Contains(buf.String(), `"mode":"trash"`) || !strings.Contains(buf.String(), `"result":"done"`) {
		t.Fatalf("unexpected journal %q", buf.String())
	}
}

func TestDeleteModeRemovesPermanently(t *testing.T) {
	root := tempRoot(t)
	c := artifact(t, root, "web")

	var (
		cl  calls
		buf bytes.Buffer
	)

	ex := newExec(root, &cl, &buf)
	ex.Mode = ModeDelete

	if _, err := ex.Run(context.Background(), []core.Candidate{c}); err != nil {
		t.Fatal(err)
	}

	if len(cl.remove) != 1 || len(cl.trash) != 0 {
		t.Fatalf("trash=%v remove=%v", cl.trash, cl.remove)
	}
}

func TestProtectedPathIsNeverTouched(t *testing.T) {
	root := tempRoot(t)
	c := artifact(t, root, "web")

	var (
		cl  calls
		buf bytes.Buffer
	)

	ex := newExec(root, &cl, &buf)
	ex.Guard = safety.Guard{Protected: []string{c.Path}}

	res, _ := ex.Run(context.Background(), []core.Candidate{c})

	if res[0].Status != StatusFailed || len(cl.trash)+len(cl.remove) != 0 {
		t.Fatalf("a protected path must be refused: %+v", res[0])
	}

	if _, err := os.Stat(c.Path); err != nil {
		t.Fatal("the folder must still exist")
	}
}

func TestPathOutsideScanRootIsRefused(t *testing.T) {
	root, other := tempRoot(t), tempRoot(t)
	c := artifact(t, other, "web")

	var (
		cl  calls
		buf bytes.Buffer
	)

	res, _ := newExec(root, &cl, &buf).Run(context.Background(), []core.Candidate{c})

	if res[0].Status != StatusFailed || len(cl.trash) != 0 {
		t.Fatalf("unexpected result %+v", res[0])
	}
}

func TestProjectArtifactWithoutRootsIsRefused(t *testing.T) {
	root := tempRoot(t)
	c := artifact(t, root, "web")

	var (
		cl  calls
		buf bytes.Buffer
	)

	ex := newExec(root, &cl, &buf)
	ex.Roots = nil

	if res, _ := ex.Run(context.Background(), []core.Candidate{c}); res[0].Status != StatusFailed || len(cl.trash) != 0 {
		t.Fatalf("unexpected result %+v", res[0])
	}
}

func TestChangedSinceScanIsRejected(t *testing.T) {
	root := tempRoot(t)
	c := artifact(t, root, "web")

	var (
		cl  calls
		buf bytes.Buffer
	)

	ex := newExec(root, &cl, &buf)
	ex.Verify = func(core.Candidate) error { return errors.New("package.json is gone") }

	res, _ := ex.Run(context.Background(), []core.Candidate{c})

	if res[0].Status != StatusFailed || !strings.Contains(res[0].Error, "changed since the scan") || len(cl.trash) != 0 {
		t.Fatalf("unexpected result %+v", res[0])
	}
}

func TestFolderThatSurvivesIsAFailure(t *testing.T) {
	root := tempRoot(t)
	c := artifact(t, root, "web")

	var buf bytes.Buffer

	ex := newExec(root, &calls{}, &buf)
	ex.Trash = func(string) error { return nil } // pretends to work, removes nothing

	res, _ := ex.Run(context.Background(), []core.Candidate{c})

	if res[0].Status != StatusFailed || !strings.Contains(res[0].Error, "still exists") {
		t.Fatalf("unexpected result %+v", res[0])
	}
}

func TestOneFailureDoesNotStopTheOthers(t *testing.T) {
	root := tempRoot(t)
	a, b := artifact(t, root, "a"), artifact(t, root, "b")

	var (
		cl  calls
		buf bytes.Buffer
	)

	ex := newExec(root, &cl, &buf)
	ex.Trash = func(p string) error {
		if strings.Contains(p, "a") && strings.HasSuffix(filepath.Dir(p), "a") {
			return errors.New("file in use")
		}

		return cl.trashFn(p)
	}

	res, err := ex.Run(context.Background(), []core.Candidate{a, b})
	if err != nil {
		t.Fatal(err)
	}

	if res[0].Status != StatusFailed || res[1].Status != StatusDone {
		t.Fatalf("unexpected results %+v", res)
	}
}

func TestRunCommandUsesTheRunner(t *testing.T) {
	var buf bytes.Buffer

	runner := &fakeRunner{}
	ex := &Executor{Runner: runner, Journal: NewJournal(&buf)}

	c := cand("npm", 500)
	c.Action = core.ActionSpec{Type: "run-command", Command: []string{"npm", "cache", "clean", "--force"}}

	res, _ := ex.Run(context.Background(), []core.Candidate{c})

	if res[0].Status != StatusDone || len(runner.ran) != 1 || runner.ran[0] != "npm cache clean --force" {
		t.Fatalf("unexpected result %+v / %v", res[0], runner.ran)
	}

	if !strings.Contains(buf.String(), `"mode":"command"`) {
		t.Fatalf("unexpected journal %q", buf.String())
	}
}

func TestCommandFailureIsReported(t *testing.T) {
	ex := &Executor{Runner: &fakeRunner{err: errors.New("exit 1")}}

	c := cand("npm", 500)
	c.Action = core.ActionSpec{Type: "run-command", Command: []string{"npm", "cache", "clean"}}

	res, _ := ex.Run(context.Background(), []core.Candidate{c})

	if res[0].Status != StatusFailed || !strings.Contains(res[0].Error, "boom") {
		t.Fatalf("unexpected result %+v", res[0])
	}
}

func TestUnknownCommandIsRefused(t *testing.T) {
	runner := &fakeRunner{}
	ex := &Executor{Runner: runner}

	c := cand("evil", 1)
	c.Action = core.ActionSpec{Type: "run-command", Command: []string{"powershell", "-c", "rm -r /"}}

	res, _ := ex.Run(context.Background(), []core.Candidate{c})

	if res[0].Status != StatusFailed || len(runner.ran) != 0 {
		t.Fatalf("a command outside the allow-list must not run: %+v", res[0])
	}
}

func TestCancelledContextStopsTheRun(t *testing.T) {
	root := tempRoot(t)
	c := artifact(t, root, "web")

	var (
		cl  calls
		buf bytes.Buffer
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	res, err := newExec(root, &cl, &buf).Run(ctx, []core.Candidate{c})

	if err == nil || len(res) != 0 || len(cl.trash) != 0 {
		t.Fatalf("expected a cancelled run, got %v / %v", err, res)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestJournalFailureStopsTheRun(t *testing.T) {
	root := tempRoot(t)
	a, b := artifact(t, root, "a"), artifact(t, root, "b")

	var (
		cl  calls
		buf bytes.Buffer
	)

	ex := newExec(root, &cl, &buf)
	ex.Journal = NewJournal(failingWriter{})

	res, err := ex.Run(context.Background(), []core.Candidate{a, b})

	if err == nil || len(res) != 1 || len(cl.trash) != 1 {
		t.Fatalf("the run must stop after the first unlogged item: %v / %d", err, len(res))
	}
}

func TestWSLCommandsAreValidatedStrictly(t *testing.T) {
	ok := [][]string{
		{"npm", "cache", "clean", "--force"},
		{"wsl.exe", "-d", "Ubuntu", "--", "npm", "cache", "clean", "--force"},
		{"wsl.exe", "-d", "Ubuntu", "-u", "root", "--", "snap", "remove", "snapd", "--revision=27738"},
	}

	for _, cmd := range ok {
		if err := validateCommand(cmd); err != nil {
			t.Fatalf("%v must be accepted: %v", cmd, err)
		}
	}

	bad := [][]string{
		nil,
		{"wsl.exe", "-d", "Ubuntu", "--", "rm", "-rf", "/"},
		{"wsl.exe", "-d", "Ubuntu", "-u", "root", "--", "rm", "-rf", "/"},
		{"wsl.exe", "-d", "Ubuntu", "-u", "root", "--", "npm", "cache", "clean"},
		{"wsl.exe", "-d", "Ubuntu", "-u", "root", "--", "snap", "remove", "snapd"},
		{"wsl.exe", "-d", "Ubuntu", "-u", "root", "--", "snap", "remove", "../x", "--revision=1"},
		{"wsl.exe", "-d", "Ubuntu", "-u", "root", "--", "snap", "remove", "x", "--revision=1;rm"},
		{"wsl.exe", "-d", "Ubuntu", "-u", "bob", "--", "npm", "cache", "clean"},
		{"wsl.exe", "-d", "Ubuntu", "--", "snap", "remove", "x", "--revision=1"},
		{"wsl.exe", "-d", "-x", "--", "npm", "cache", "clean"},
		{"wsl.exe", "--shutdown"},
		{"wsl.exe", "-d", "Ubuntu", "npm", "cache", "clean", "x"},
	}

	for _, cmd := range bad {
		if err := validateCommand(cmd); err == nil {
			t.Fatalf("%v must be refused", cmd)
		}
	}
}

func TestExecutorRunsWSLCommandsThroughTheRunner(t *testing.T) {
	runner := &fakeRunner{}
	ex := &Executor{Runner: runner}

	c := cand("Ubuntu: npm", 10)
	c.Action = core.ActionSpec{Type: "run-command", Command: []string{"wsl.exe", "-d", "Ubuntu", "--", "npm", "cache", "clean", "--force"}}

	rm := cand("Ubuntu: evil", 10)
	rm.Action = core.ActionSpec{Type: "run-command", Command: []string{"wsl.exe", "-d", "Ubuntu", "--", "rm", "-rf", "/"}}

	res, _ := ex.Run(context.Background(), []core.Candidate{c, rm})

	if res[0].Status != StatusDone || res[1].Status != StatusFailed || len(runner.ran) != 1 {
		t.Fatalf("unexpected results %+v / %v", res, runner.ran)
	}
}
