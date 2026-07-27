package arity

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"lesiw.io/command"
	"lesiw.io/command/mock"
)

const buildHelp = `usage: go build [-o output] [build flags] [packages]

Build compiles the packages named by the import paths.

	-o output
		write the resulting executable to output.
	-race
		enable data race detection.
	-p n
		the number of programs that can be run in parallel.
`

const testHelp = `The following flags are recognized by the go test command.

	-bench regexp
		Run only those benchmarks matching a regular expression.
	-v
		Verbose output.
	-race
		This flag also appears in build flags.
`

func TestLoad(t *testing.T) {
	mm := new(mock.Machine)
	mm.Return(strings.NewReader(buildHelp), "go", "help", "build")
	mm.Return(strings.NewReader(testHelp), "go", "help", "testflag")
	got, err := Load(t.Context(), command.Machine(mm))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"o": true, "race": false, "p": true, "bench": true, "v": false,
	}
	if !cmp.Equal(want, got) {
		t.Errorf("Load: -want +got\n%s", cmp.Diff(want, got))
	}
}
