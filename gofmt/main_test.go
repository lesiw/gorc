package main

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/rogpeppe/go-internal/diff"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"lesiw.io/command"
	"lesiw.io/command/mock"
	"lesiw.io/fs"
	"lesiw.io/fs/path"
)

func swap[T any](t *testing.T, orig *T, with T) {
	t.Helper()
	o := *orig
	t.Cleanup(func() { *orig = o })
	*orig = with
}

// swapMachine swaps the machine and its filesystem for a mock.
func swapMachine(t *testing.T) *mock.Machine {
	t.Helper()
	mm := new(mock.Machine)
	swap(t, &m, command.Machine(mm))
	swap(t, &fsys, command.FS(mm))
	swap(t, &stderr, io.Writer(io.Discard))
	return mm
}

var runTests = []struct {
	name    string
	rc      string            // go.fmt content; empty for none
	gorc    string            // GORC value; empty for unset
	args    []string          // os.Args
	stdin   string            // standard input
	files   map[string]string // files seeded under /work
	returns map[string]string // mock output keyed by first command word
	calls   []mock.Call       // commands run on the machine
	out     string            // captured stdout
	want    map[string]string // expected file contents after the run
	exec    bool              // passthrough expected
	err     string            // error returned from run
}{{
	name:    "formats standard input through the pipeline",
	rc:      "gofmt -s\ngoimports\n",
	args:    []string{"gofmt"},
	stdin:   "a",
	returns: map[string]string{"gofmt": "b", "goimports": "c"},
	calls: []mock.Call{{
		Args: []string{"gofmt", "-s"},
		Env:  map[string]string{"GORC": "1"},
		Got:  []byte("a"),
	}, {
		Args: []string{"goimports"},
		Env:  map[string]string{"GORC": "1"},
		Got:  []byte("b"),
	}},
	out: "c",
}, {
	name:    "applies command environment",
	rc:      "GOWORK=off goimports\n",
	args:    []string{"gofmt"},
	stdin:   "a",
	returns: map[string]string{"goimports": "b"},
	calls: []mock.Call{{
		Args: []string{"goimports"},
		Env: map[string]string{
			"GORC":   "1",
			"GOWORK": "off",
		},
		Got: []byte("a"),
	}},
	out: "b",
}, {
	name:    "ignores formatting flags",
	rc:      "gofmt -s\n",
	args:    []string{"gofmt", "-s", "-r", "a->b", "-l", "/work/a.go"},
	files:   map[string]string{"/work/a.go": "a"},
	returns: map[string]string{"gofmt": "b"},
	calls: []mock.Call{{
		Args: []string{"gofmt", "-s"},
		Env:  map[string]string{"GORC": "1"},
		Got:  []byte("a"),
	}},
	out: "/work/a.go\n",
}, {
	name:    "lists a changed file",
	rc:      "gofmt -s\n",
	args:    []string{"gofmt", "-l", "/work/a.go"},
	files:   map[string]string{"/work/a.go": "a"},
	returns: map[string]string{"gofmt": "b"},
	calls: []mock.Call{{
		Args: []string{"gofmt", "-s"},
		Env:  map[string]string{"GORC": "1"},
		Got:  []byte("a"),
	}},
	out: "/work/a.go\n",
}, {
	name:    "rewrites a changed file",
	rc:      "gofmt -s\n",
	args:    []string{"gofmt", "-w", "/work/a.go"},
	files:   map[string]string{"/work/a.go": "a"},
	returns: map[string]string{"gofmt": "b"},
	calls: []mock.Call{{
		Args: []string{"gofmt", "-s"},
		Env:  map[string]string{"GORC": "1"},
		Got:  []byte("a"),
	}},
	want: map[string]string{"/work/a.go": "b"},
}, {
	name:    "leaves an unchanged file alone",
	rc:      "gofmt -s\n",
	args:    []string{"gofmt", "-l", "-w", "/work/a.go"},
	files:   map[string]string{"/work/a.go": "a"},
	returns: map[string]string{"gofmt": "a"},
	calls: []mock.Call{{
		Args: []string{"gofmt", "-s"},
		Env:  map[string]string{"GORC": "1"},
		Got:  []byte("a"),
	}},
	want: map[string]string{"/work/a.go": "a"},
}, {
	name: "walks directories for go files",
	rc:   "gofmt -s\n",
	args: []string{"gofmt", "-l", "/work"},
	files: map[string]string{
		"/work/a.go":       "a",
		"/work/sub/b.go":   "a",
		"/work/.hidden.go": "a",
		"/work/c.txt":      "a",
	},
	returns: map[string]string{"gofmt": "b"},
	calls: []mock.Call{{
		Args: []string{"gofmt", "-s"},
		Env:  map[string]string{"GORC": "1"},
		Got:  []byte("a"),
	}, {
		Args: []string{"gofmt", "-s"},
		Env:  map[string]string{"GORC": "1"},
		Got:  []byte("a"),
	}},
	out: "/work/a.go\n/work/sub/b.go\n",
}, {
	name: "rejects -w with standard input",
	rc:   "gofmt -s\n",
	args: []string{"gofmt", "-w"},
	err:  "cannot use -w with standard input",
}, {
	name: "passes through without a go.fmt",
	args: []string{"gofmt", "-l", "."},
	exec: true,
}, {
	name: "passes through when GORC is set",
	rc:   "gofmt -s\n",
	gorc: "1",
	args: []string{"gofmt"},
	exec: true,
}, {
	name: "passes through with an empty go.fmt",
	rc:   "// comments only\n",
	args: []string{"gofmt"},
	exec: true,
}}

func TestRun(t *testing.T) {
	for _, tt := range runTests {
		t.Run(tt.name, func(t *testing.T) {
			mm := swapMachine(t)
			for arg, out := range tt.returns {
				mm.Return(strings.NewReader(out), arg)
			}
			swap(t, &os.Args, tt.args)
			swap(t, &stdin, io.Reader(strings.NewReader(tt.stdin)))
			var out strings.Builder
			swap(t, &stdout, io.Writer(&out))
			var execCalled bool
			swap(t, &testHookExec, func() error {
				execCalled = true
				return nil
			})
			t.Setenv("GORC", tt.gorc)
			ctx := fs.WithWorkDir(t.Context(), "/work")
			if err := fs.MkdirAll(ctx, fsys, "/work"); err != nil {
				t.Fatal(err)
			}
			if tt.rc != "" {
				err := fs.WriteFile(ctx, fsys, "/work/go.fmt", []byte(tt.rc))
				if err != nil {
					t.Fatal(err)
				}
			}
			for name, content := range tt.files {
				err := fs.MkdirAll(ctx, fsys, path.Dir(name))
				if err != nil {
					t.Fatal(err)
				}
				err = fs.WriteFile(ctx, fsys, name, []byte(content))
				if err != nil {
					t.Fatal(err)
				}
			}
			err := run(ctx)
			if tt.err == "" && err != nil {
				t.Fatalf("run() = %v; want nil", err)
			}
			if tt.err != "" && (err == nil ||
				!strings.Contains(err.Error(), tt.err)) {
				t.Fatalf("run() = %v; want %q", err, tt.err)
			}
			// Pipeline commands start concurrently, so calls are
			// recorded in no particular order.
			opts := []cmp.Option{
				cmpopts.EquateEmpty(),
				cmpopts.SortSlices(func(a, b mock.Call) bool {
					return strings.Join(a.Args, " ") <
						strings.Join(b.Args, " ")
				}),
			}
			if !cmp.Equal(tt.calls, mm.Calls, opts...) {
				t.Errorf("calls: -want +got\n%s",
					cmp.Diff(tt.calls, mm.Calls, opts...),
				)
			}
			if got := out.String(); got != tt.out {
				t.Errorf("stdout = %q; want %q", got, tt.out)
			}
			if execCalled != tt.exec {
				t.Errorf("passthrough = %v; want %v", execCalled, tt.exec)
			}
			for name, content := range tt.want {
				got, err := fs.ReadFile(ctx, fsys, name)
				if err != nil {
					t.Fatal(err)
				}
				if string(got) != content {
					t.Errorf("%s = %q; want %q", name, got, content)
				}
			}
		})
	}
}

func TestDiffMode(t *testing.T) {
	mm := swapMachine(t)
	mm.Return(strings.NewReader("b\n"), "gofmt")
	swap(t, &os.Args, []string{"gofmt", "-d", "/work/a.go"})
	swap(t, &stdin, io.Reader(strings.NewReader("")))
	var out strings.Builder
	swap(t, &stdout, io.Writer(&out))
	t.Setenv("GORC", "")
	ctx := fs.WithWorkDir(t.Context(), "/work")
	if err := fs.MkdirAll(ctx, fsys, "/work"); err != nil {
		t.Fatal(err)
	}
	err := fs.WriteFile(ctx, fsys, "/work/go.fmt", []byte("gofmt -s\n"))
	if err != nil {
		t.Fatal(err)
	}
	err = fs.WriteFile(ctx, fsys, "/work/a.go", []byte("a\n"))
	if err != nil {
		t.Fatal(err)
	}
	err = run(ctx)
	var cmdErr *command.Error
	if !errors.As(err, &cmdErr) || cmdErr.Code != 1 {
		t.Fatalf("run() = %v; want exit code 1", err)
	}
	want := string(diff.Diff(
		"/work/a.go.orig", []byte("a\n"), "/work/a.go", []byte("b\n"),
	))
	if got := out.String(); got != want {
		t.Errorf("stdout = %q; want %q", got, want)
	}
}

func TestParseErrorWarnsAndPassesThrough(t *testing.T) {
	swapMachine(t)
	swap(t, &os.Args, []string{"gofmt"})
	var execCalled bool
	swap(t, &testHookExec, func() error { execCalled = true; return nil })
	t.Setenv("GORC", "")
	ctx := fs.WithWorkDir(t.Context(), "/work")
	if err := fs.MkdirAll(ctx, fsys, "/work"); err != nil {
		t.Fatal(err)
	}
	err := fs.WriteFile(ctx, fsys, "/work/go.fmt", []byte("A=1\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := run(ctx); err != nil {
		t.Fatalf("run() = %v; want nil", err)
	}
	if !execCalled {
		t.Error("run() did not pass through")
	}
}
