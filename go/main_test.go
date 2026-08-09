package main

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"lesiw.io/command"
	"lesiw.io/command/mock"
	"lesiw.io/fs"

	"lesiw.io/gorc/internal/arity"
)

func swap[T any](t *testing.T, orig *T, with T) {
	t.Helper()
	o := *orig
	t.Cleanup(func() { *orig = o })
	*orig = with
}

func swapMachine(t *testing.T) *mock.Machine {
	t.Helper()
	mm := new(mock.Machine)
	swap(t, &m, command.Machine(mm))
	swap(t, &fsys, command.FS(mm))
	swap(t, &stderr, io.Writer(io.Discard))
	return mm
}

var runTests = []struct {
	name  string
	rc    string      // go.rc content; empty for no go.rc
	local string      // go.local.rc content; empty for none
	gorc  string      // GORC value; empty for unset
	args  []string    // os.Args
	fail  []string    // command the machine fails with code 3
	calls []mock.Call // commands run on the machine
	exec  []string    // arguments passed through to the real go
	err   string      // error returned from run
}{{
	name: "intercepts a defined verb",
	rc:   "test MARK=hello go test -count=5\n",
	args: []string{"go", "test", "./..."},
	calls: []mock.Call{{
		Args: []string{"go", "test", "-count=5"},
		Env:  map[string]string{"GORC": "1", "MARK": "hello"},
	}},
}, {
	name: "expands variables",
	rc:   "test go test $GOARGS -mid $GOFLAGS\n",
	args: []string{"go", "test", "./p", "-v"},
	calls: []mock.Call{{
		Args: []string{"go", "test", "./p", "-mid", "-v"},
		Env:  map[string]string{"GORC": "1"},
	}},
}, {
	name: "runs commands in order",
	rc:   "test (\n\tgo one\n\tgo two\n)\n",
	args: []string{"go", "test"},
	calls: []mock.Call{{
		Args: []string{"go", "one"},
		Env:  map[string]string{"GORC": "1"},
	}, {
		Args: []string{"go", "two"},
		Env:  map[string]string{"GORC": "1"},
	}},
}, {
	name: "stops at the first failure",
	rc:   "test (\n\tgo one\n\tgo two\n\tgo three\n)\n",
	args: []string{"go", "test"},
	fail: []string{"go", "two"},
	calls: []mock.Call{{
		Args: []string{"go", "one"},
		Env:  map[string]string{"GORC": "1"},
	}, {
		Args: []string{"go", "two"},
		Env:  map[string]string{"GORC": "1"},
	}},
	err: "exit status 3",
}, {
	name: "passes through when a command expands to nothing",
	rc:   "test $GOARGS\n",
	args: []string{"go", "test"},
	exec: []string{"test"},
}, {
	name: "passes through an undefined verb",
	rc:   "test go test\n",
	args: []string{"go", "build", "./..."},
	exec: []string{"build", "./..."},
}, {
	name: "passes through without a go.rc",
	args: []string{"go", "version"},
	exec: []string{"version"},
}, {
	name: "passes through when GORC is set",
	rc:   "version go test\n",
	gorc: "0",
	args: []string{"go", "version"},
	exec: []string{"version"},
}, {
	name: "passes through without arguments",
	args: []string{"go"},
	exec: []string{},
}, {
	name:  "go.local.rc overrides a verb",
	rc:    "test go test -count=5\n",
	local: "test go test -timeout=10m\n",
	args:  []string{"go", "test"},
	calls: []mock.Call{{
		Args: []string{"go", "test", "-timeout=10m"},
		Env:  map[string]string{"GORC": "1"},
	}},
}, {
	name:  "go.local.rc leaves other verbs alone",
	rc:    "test go test -count=5\nbench go test -bench=.\n",
	local: "test go test -short\n",
	args:  []string{"go", "bench"},
	calls: []mock.Call{{
		Args: []string{"go", "test", "-bench=."},
		Env:  map[string]string{"GORC": "1"},
	}},
}, {
	name:  "go.local.rc adds a verb",
	rc:    "test go test\n",
	local: "bench go test -bench=.\n",
	args:  []string{"go", "bench"},
	calls: []mock.Call{{
		Args: []string{"go", "test", "-bench=."},
		Env:  map[string]string{"GORC": "1"},
	}},
}, {
	name:  "go.local.rc works alone",
	local: "test go test -short\n",
	args:  []string{"go", "test"},
	calls: []mock.Call{{
		Args: []string{"go", "test", "-short"},
		Env:  map[string]string{"GORC": "1"},
	}},
}}

func TestRun(t *testing.T) {
	for _, tt := range runTests {
		t.Run(tt.name, func(t *testing.T) {
			mm := swapMachine(t)
			if tt.fail != nil {
				mm.Return(command.Fail(&command.Error{Code: 3}), tt.fail...)
			}
			swap(t, &os.Args, tt.args)
			var execArgs []string
			var execCalled bool
			swap(t, &testHookExec, func() error {
				execCalled, execArgs = true, os.Args[1:]
				return nil
			})
			t.Setenv("GORC", tt.gorc)
			ctx := fs.WithWorkDir(t.Context(), "/work")
			if err := fs.MkdirAll(ctx, fsys, "/work"); err != nil {
				t.Fatal(err)
			}
			if tt.rc != "" {
				err := fs.WriteFile(ctx, fsys, "/work/go.rc", []byte(tt.rc))
				if err != nil {
					t.Fatal(err)
				}
			}
			if tt.local != "" {
				err := fs.WriteFile(ctx, fsys,
					"/work/go.local.rc", []byte(tt.local),
				)
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
			opt := cmpopts.EquateEmpty()
			if !cmp.Equal(tt.calls, mm.Calls, opt) {
				t.Errorf("calls: -want +got\n%s",
					cmp.Diff(tt.calls, mm.Calls, opt),
				)
			}
			if execCalled != (tt.exec != nil) {
				t.Errorf("passthrough = %v; want %v",
					execCalled, tt.exec != nil)
			}
			if !cmp.Equal(tt.exec, execArgs, opt) {
				t.Errorf("exec args: -want +got\n%s",
					cmp.Diff(tt.exec, execArgs, opt),
				)
			}
		})
	}
}

func TestParseErrorWarnsAndPassesThrough(t *testing.T) {
	mm := swapMachine(t)
	swap(t, &os.Args, []string{"go", "version"})
	var execCalled bool
	swap(t, &testHookExec, func() error { execCalled = true; return nil })
	t.Setenv("GORC", "")
	ctx := fs.WithWorkDir(t.Context(), "/work")
	if err := fs.MkdirAll(ctx, fsys, "/work"); err != nil {
		t.Fatal(err)
	}
	err := fs.WriteFile(ctx, fsys, "/work/go.rc", []byte("test\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := run(ctx); err != nil {
		t.Fatalf("run() = %v; want nil", err)
	}
	if !execCalled {
		t.Error("run() did not pass through")
	}
	if len(mm.Calls) != 0 {
		t.Errorf("calls = %v; want none", mm.Calls)
	}
}

var splitArgsTests = []struct {
	in      []string
	goargs  []string
	goflags []string
}{{
	in: nil,
}, {
	in:     []string{"./..."},
	goargs: []string{"./..."},
}, {
	in:     []string{"./a", "./b"},
	goargs: []string{"./a", "./b"},
}, {
	in:      []string{"-run", "TestFoo", "./go"},
	goargs:  []string{"./go"},
	goflags: []string{"-run", "TestFoo"},
}, {
	in:      []string{"-count=1", "./..."},
	goargs:  []string{"./..."},
	goflags: []string{"-count=1"},
}, {
	in:      []string{"./go", "-v"},
	goargs:  []string{"./go"},
	goflags: []string{"-v"},
}, {
	in:      []string{"-json", "-run", "^TestX$", "./pkg"},
	goargs:  []string{"./pkg"},
	goflags: []string{"-json", "-run", "^TestX$"},
}, {
	in:      []string{"./p", "-args", "x", "y"},
	goargs:  []string{"./p"},
	goflags: []string{"-args", "x", "y"},
}, {
	in:      []string{"-test.fullpath=true", "-test.run", "TestFoo", "./p"},
	goargs:  []string{"./p"},
	goflags: []string{"-test.fullpath=true", "-test.run", "TestFoo"},
}, {
	in:      []string{"-frob", "x", "./p"},
	goargs:  []string{"x", "./p"},
	goflags: []string{"-frob"},
}, {
	in:      []string{"./p", "-frob=x", "-v"},
	goargs:  []string{"./p"},
	goflags: []string{"-frob=x", "-v"},
}}

func TestSplitArgs(t *testing.T) {
	for _, tt := range splitArgsTests {
		goargs, goflags := splitArgs(arity.Table, tt.in)
		opt := cmpopts.EquateEmpty()
		if !cmp.Equal(tt.goargs, goargs, opt) {
			t.Errorf("splitArgs(%q) goargs: -want +got\n%s",
				tt.in, cmp.Diff(tt.goargs, goargs, opt),
			)
		}
		if !cmp.Equal(tt.goflags, goflags, opt) {
			t.Errorf("splitArgs(%q) goflags: -want +got\n%s",
				tt.in, cmp.Diff(tt.goflags, goflags, opt),
			)
		}
	}
}

var expandTests = []struct {
	line    string
	goargs  []string
	goflags []string
	want    string
}{{
	line: "go test $GOARGS -race $GOFLAGS",
	want: "go test  -race ",
}, {
	line:    "go test $GOARGS -race $GOFLAGS",
	goargs:  []string{"./..."},
	goflags: []string{"-run", "TestFoo"},
	want:    "go test ./... -race -run TestFoo",
}, {
	line:    "go test -count=1",
	goargs:  []string{"./..."},
	goflags: []string{"-v"},
	want:    "go test -count=1",
}, {
	line:   "go vet $GOARGS $GOARGS",
	goargs: []string{"./a"},
	want:   "go vet ./a ./a",
}, {
	line:    "go test ${GOARGS} ${GOFLAGS}",
	goargs:  []string{"./a", "./b"},
	goflags: []string{"-v"},
	want:    "go test ./a ./b -v",
}}

func TestExpand(t *testing.T) {
	for _, tt := range expandTests {
		got := expand(tt.line, tt.goargs, tt.goflags)
		if got != tt.want {
			t.Errorf("expand(%q, %q, %q) = %q; want %q", tt.line,
				tt.goargs, tt.goflags, got, tt.want,
			)
		}
	}
}

func TestExpandEnvironment(t *testing.T) {
	t.Setenv("GORC_TEST_VALUE", "x")
	line := "echo $GORC_TEST_VALUE v=${GORC_TEST_VALUE}"
	got := expand(line, nil, nil)
	if want := "echo x v=x"; got != want {
		t.Errorf("expand(%q) = %q; want %q", line, got, want)
	}
}
