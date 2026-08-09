// go wraps the go command, applying per-project command definitions from a
// go.rc file.
//
// When go is run with a verb defined in the nearest go.rc file, found by
// searching from the current directory upward, the commands defined for that
// verb run in its place, exactly as written in go.rc. Arguments after the verb
// do not carry over unless a command asks for them: $GOARGS expands to the
// invocation's arguments, such as package patterns, and $GOFLAGS expands to
// its flags. Flags the go command documents as taking a value carry the
// following word with them; any other word beginning with - is a flag on
// its own, so spell unrecognized flags' values as -flag=value. References
// expand before each line is split into words, and any
// other name expands from the environment. Every other invocation passes
// through unchanged to the next go command in PATH.
//
// A go.local.rc file in the same directory, typically kept out of version
// control, overrides go.rc verb by verb, and may also be used on its own.
//
// Setting GORC in the environment to any non-empty value bypasses
// interception. Intercepted commands run with GORC=1 set, so go commands they
// invoke are not intercepted again.
//
// A line in a go.rc has the form:
//
//	verb command
//
// and multiple commands may be grouped go keyword style:
//
//	verb (
//		command
//		command
//	)
//
// Repeated verbs accumulate. Blank lines are ignored. // indicates a comment.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/signal"
	"slices"
	"strings"

	"lesiw.io/command"
	"lesiw.io/command/sys"
	"lesiw.io/fs/path"

	"lesiw.io/gorc/internal/arity"
	"lesiw.io/gorc/internal/rc"
	"lesiw.io/gorc/internal/shim"
)

var (
	m       = sys.Machine()
	fsys    = command.FS(m)
	rcNames = []string{"go.rc", "go.local.rc"}

	stderr io.Writer = os.Stderr

	testHookExec func() error
)

func main() {
	if err := run(context.Background()); err != nil {
		var cmdErr *command.Error
		if errors.As(err, &cmdErr) && cmdErr.Code != 0 {
			os.Exit(cmdErr.Code)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	// Allow os.Interrupt to propagate to children but ignore it ourselves.
	signal.Notify(make(chan os.Signal, 1), os.Interrupt)

	if len(os.Args) < 2 || os.Getenv("GORC") != "" {
		return passthrough(ctx)
	}
	dir := rc.FindDir(ctx, fsys, rcNames...)
	if dir == "" {
		return passthrough(ctx)
	}
	goargs, goflags := splitArgs(arity.Table, os.Args[2:])
	verbs := make(map[string][]rc.Command)
	for _, base := range rcNames {
		name := path.Join(dir, base)
		cmds, err := rc.ParseFile(ctx, fsys, name, func(line string) string {
			return expand(line, goargs, goflags)
		})
		if err != nil {
			_, err := fmt.Fprintf(stderr, "%s: %v\n", name, err)
			if err != nil {
				return err
			}
			return passthrough(ctx)
		}
		maps.Copy(verbs, cmds)
	}
	cmds := verbs[os.Args[1]]
	if len(cmds) == 0 {
		return passthrough(ctx)
	}
	for _, c := range cmds {
		_, err := shim.Fprintf(stderr,
			"%s: %s\n", c.Src, shim.Display(slices.Concat(c.Env, c.Arg)),
		)
		if err != nil {
			return err
		}
		env := map[string]string{"GORC": "1"}
		for _, kv := range c.Env {
			k, v, _ := strings.Cut(kv, "=")
			env[k] = v
		}
		ctx := command.WithEnv(ctx, env)
		if err := command.Exec(ctx, m, c.Arg...); err != nil {
			return err
		}
	}
	return nil
}

// splitArgs separates an intercepted invocation's arguments from its
// flags. A word beginning with - is a flag on its own; flags the table
// records as taking a value also carry the following word. Test flags
// are recognized with an optional test. prefix, per go help testflag.
func splitArgs(table map[string]bool, words []string) (goargs, goflags []string) {
	for i := 0; i < len(words); i++ {
		w := words[i]
		if !strings.HasPrefix(w, "-") {
			goargs = append(goargs, w)
			continue
		}
		name, _, assigned := strings.Cut(strings.TrimLeft(w, "-"), "=")
		name = strings.TrimPrefix(name, "test.")
		goflags = append(goflags, w)
		if name == "args" {
			// go test -args passes the rest to the test binary.
			goflags = append(goflags, words[i+1:]...)
			break
		}
		if table[name] && !assigned && i+1 < len(words) {
			i++
			goflags = append(goflags, words[i])
		}
	}
	return goargs, goflags
}

// expand expands variable references in one go.rc line before it is split into
// words. GOARGS and GOFLAGS take the intercepted invocation's arguments and
// every other name takes its environment value.
func expand(line string, goargs, goflags []string) string {
	return os.Expand(line, func(name string) string {
		switch name {
		case "GOARGS":
			return strings.Join(goargs, " ")
		case "GOFLAGS":
			return strings.Join(goflags, " ")
		}
		return os.Getenv(name)
	})
}

func passthrough(ctx context.Context) error {
	if h := testHookExec; h != nil {
		return h()
	}
	path, err := shim.Target("go")
	if err != nil {
		return err
	}
	return shim.Exec(ctx, m, path, os.Args)
}
