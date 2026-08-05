package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"lesiw.io/command"
)

const unformatted = "package main\n\nvar  x  =  1\n"

var parityTests = []struct {
	name  string
	stdin string
	args  []string
}{{
	name: "stdout",
	args: []string{"a.go"},
}, {
	name: "list",
	args: []string{"-l", "."},
}, {
	name:  "stdin",
	stdin: unformatted,
}, {
	name: "diff",
	args: []string{"-d", "a.go"},
}, {
	name: "diff clean",
	args: []string{"-d", "b.go"},
}, {
	name: "write",
	args: []string{"-w", "a.go"},
}}

// TestParity runs the shim with plain gofmt as its only backing
// formatter and compares its behavior byte for byte with gofmt itself.
func TestParity(t *testing.T) {
	gofmt, err := exec.LookPath("gofmt")
	if err != nil {
		t.Fatal(err)
	}
	setup := func(t *testing.T) string {
		t.Helper()
		dir := t.TempDir()
		files := map[string]string{
			"go.fmt": "gofmt\n",
			"a.go":   unformatted,
			"b.go":   "package main\n\nvar y = 2\n",
		}
		for name, content := range files {
			err := os.WriteFile(filepath.Join(dir, name),
				[]byte(content), 0o644)
			if err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}
	vanilla := func(
		t *testing.T, dir, stdin string, args ...string,
	) (string, int) {
		t.Helper()
		cmd := exec.Command(gofmt, args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GORC=1")
		cmd.Stdin = strings.NewReader(stdin)
		var out, errs bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errs
		err := cmd.Run()
		var exitErr *exec.ExitError
		ok := errors.As(err, &exitErr)
		if err != nil && !ok {
			t.Fatalf("gofmt %v: %v\n%s", args, err, errs.String())
		}
		var code int
		if ok {
			code = exitErr.ExitCode()
		}
		return out.String(), code
	}
	ours := func(t *testing.T, dir, in string, args ...string) (string, int) {
		t.Helper()
		t.Chdir(dir)
		t.Setenv("GORC", "")
		swap(t, &stderr, io.Writer(io.Discard))
		swap(t, &os.Args, append([]string{"gofmt"}, args...))
		swap(t, &stdin, io.Reader(strings.NewReader(in)))
		var out strings.Builder
		swap(t, &stdout, io.Writer(&out))
		err := run(t.Context())
		var cmdErr *command.Error
		ok := errors.As(err, &cmdErr)
		if err != nil && !ok {
			t.Fatalf("run() = %v; want nil", err)
		}
		var code int
		if ok {
			code = cmdErr.Code
		}
		return out.String(), code
	}
	for _, tt := range parityTests {
		t.Run(tt.name, func(t *testing.T) {
			theirs, dir := setup(t), setup(t)
			want, wantCode := vanilla(t, theirs, tt.stdin, tt.args...)
			got, gotCode := ours(t, dir, tt.stdin, tt.args...)
			if got != want {
				t.Errorf("output = %q; want %q", got, want)
			}
			if gotCode != wantCode {
				t.Errorf("exit code = %d; want %d", gotCode, wantCode)
			}
			for _, name := range []string{"a.go", "b.go"} {
				after, err := os.ReadFile(filepath.Join(dir, name))
				if err != nil {
					t.Fatal(err)
				}
				text, err := os.ReadFile(filepath.Join(theirs, name))
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(after, text) {
					t.Errorf("%s = %q; want %q", name, after, text)
				}
			}
		})
	}
}
