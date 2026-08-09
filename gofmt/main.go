// Gofmt formats Go source through per-project format commands defined in a
// go.fmt file.
//
// When a go.fmt file is found in the nearest directory at or above the
// current directory, gofmt pipes source through each command listed in it,
// in order, and presents the result through the standard gofmt interface:
// with no arguments, standard input is formatted to standard output; file
// arguments are formatted individually; directory arguments are searched
// recursively for .go files, ignoring files that begin with a dot. The -l
// flag lists files whose formatting differs, -w rewrites them in place, and
// -d prints diffs.
//
// Other gofmt flags are accepted and ignored: the commands in go.fmt
// define the formatting. References like $NAME expand from the
// environment before each line is split into words.
//
// Every other invocation passes through unchanged to the next gofmt in
// PATH. Setting GORC in the environment to any non-empty value bypasses
// interception. Intercepted commands run with GORC=1 set, so gofmt
// commands they invoke are not intercepted again.
//
// A line in a go.fmt is one command, run exactly as written. Commands
// must read Go source on standard input and write the formatted result to
// standard output. Blank lines are ignored. // indicates a comment.
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rogpeppe/go-internal/diff"

	"lesiw.io/command"
	"lesiw.io/command/sys"
	"lesiw.io/fs"
	"lesiw.io/fs/path"

	"lesiw.io/gorc/internal/rc"
	"lesiw.io/gorc/internal/shim"
)

var (
	m    = sys.Machine()
	fsys = command.FS(m)

	stdin  io.Reader = os.Stdin
	stdout io.Writer = os.Stdout
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
		os.Exit(2)
	}
}

func run(ctx context.Context) error {
	if os.Getenv("GORC") != "" {
		return passthrough(ctx)
	}
	dir := rc.FindDir(ctx, fsys, "go.fmt")
	if dir == "" {
		return passthrough(ctx)
	}
	return intercept(ctx, path.Join(dir, "go.fmt"))
}

// A formatter runs source through the go.fmt commands and presents
// results per the gofmt interface.
type formatter struct {
	cmds  []rc.Command
	list  bool
	write bool
	diff  bool

	differed bool // some formatting differed
}

func intercept(ctx context.Context, name string) error {
	var (
		flags = flag.NewFlagSet("gofmt", flag.ContinueOnError)
		f     formatter
	)
	flags.BoolVar(&f.list, "l", false, "list files whose formatting differs")
	flags.BoolVar(&f.write, "w", false, "write result to (source) file")
	flags.BoolVar(&f.diff, "d", false, "display diffs instead of rewriting")
	flags.Bool("s", false, "no effect: go.fmt defines the formatting")
	flags.Bool("e", false, "no effect: go.fmt defines the formatting")
	flags.String("r", "", "no effect: go.fmt defines the formatting")
	flags.String("cpuprofile", "", "no effect")
	if err := flags.Parse(os.Args[1:]); err != nil {
		return &command.Error{Code: 2}
	}
	cmds, err := rc.ParseListFile(ctx, fsys, name, os.ExpandEnv)
	if err != nil {
		if _, err := fmt.Fprintf(stderr, "%s: %v\n", name, err); err != nil {
			return err
		}
		return passthrough(ctx)
	}
	if len(cmds) == 0 {
		return passthrough(ctx)
	}
	f.cmds = cmds
	displays := make([]string, len(cmds))
	for i, c := range cmds {
		displays[i] = shim.Display(slices.Concat(c.Env, c.Arg))
	}
	_, err = shim.Fprintf(stderr,
		"%s: %s\n", cmds[0].Src, strings.Join(displays, " | "),
	)
	if err != nil {
		return err
	}
	ctx = command.WithEnv(ctx, map[string]string{"GORC": "1"})
	if flags.NArg() == 0 {
		if f.write {
			return fmt.Errorf("cannot use -w with standard input")
		}
		src, err := io.ReadAll(stdin)
		if err != nil {
			return err
		}
		if err := f.format(ctx, "<standard input>", src); err != nil {
			return err
		}
		return f.exitErr()
	}
	files, err := gather(ctx, flags.Args())
	if err != nil {
		return err
	}
	var failed bool
	for _, file := range files {
		src, err := fs.ReadFile(ctx, fsys, file)
		if err == nil {
			err = f.format(ctx, file, src)
		}
		if err != nil {
			if _, err := fmt.Fprintln(stderr, err); err != nil {
				return err
			}
			failed = true
		}
	}
	if failed {
		return &command.Error{Code: 2}
	}
	return f.exitErr()
}

// exitErr returns the error that reports formatting differences under -d,
// which gofmt reflects as exit status 1.
func (f *formatter) exitErr() error {
	if f.diff && f.differed {
		return &command.Error{Code: 1}
	}
	return nil
}

// gather resolves arguments to the files to format: named files as given,
// and .go files found under named directories, excluding files that begin
// with a dot.
func gather(ctx context.Context, args []string) (files []string, err error) {
	for _, arg := range args {
		info, err := fs.Stat(ctx, fsys, arg)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			files = append(files, arg)
			continue
		}
		var found []string
		for entry, err := range fs.Walk(ctx, fsys, arg, 0) {
			if err != nil {
				return nil, err
			}
			name := entry.Name()
			if entry.IsDir() || strings.HasPrefix(name, ".") {
				continue
			}
			if strings.HasSuffix(name, ".go") {
				found = append(found, strings.TrimPrefix(entry.Path(), "./"))
			}
		}
		slices.Sort(found)
		files = append(files, found...)
	}
	return files, nil
}

// format runs src through the commands and presents the result for one
// file per the gofmt interface.
func (f *formatter) format(ctx context.Context, name string, src []byte) error {
	res, err := f.pipe(ctx, src)
	if err != nil {
		return err
	}
	if !f.list && !f.write && !f.diff {
		_, err := stdout.Write(res)
		return err
	}
	if bytes.Equal(src, res) {
		return nil
	}
	f.differed = true
	if f.list {
		if _, err := fmt.Fprintln(stdout, name); err != nil {
			return err
		}
	}
	if f.write {
		if err := fs.WriteFile(ctx, fsys, name, res); err != nil {
			return err
		}
	}
	if f.diff {
		name := filepath.ToSlash(name)
		_, err := stdout.Write(diff.Diff(name+".orig", src, name, res))
		return err
	}
	return nil
}

// pipe copies src through each command in sequence and returns the final
// output.
func (f *formatter) pipe(ctx context.Context, src []byte) ([]byte, error) {
	fils := make([]io.ReadWriter, len(f.cmds))
	for i, c := range f.cmds {
		ctx := ctx
		if len(c.Env) > 0 {
			env := make(map[string]string, len(c.Env))
			for _, kv := range c.Env {
				k, v, _ := strings.Cut(kv, "=")
				env[k] = v
			}
			ctx = command.WithEnv(ctx, env)
		}
		fils[i] = command.NewFilter(ctx, m, c.Arg...)
	}
	var buf bytes.Buffer
	_, err := command.Copy(&buf, bytes.NewReader(src), fils...)
	return buf.Bytes(), err
}

func passthrough(ctx context.Context) error {
	if h := testHookExec; h != nil {
		return h()
	}
	gofmtCmd, err := shim.Target("gofmt")
	if err != nil {
		return err
	}
	return shim.Exec(ctx, m, gofmtCmd, os.Args)
}
