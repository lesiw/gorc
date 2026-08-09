// Package rc parses rc files: line-oriented command lists that follow the
// word-splitting rules of go:generate directives.
package rc

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"

	"lesiw.io/fs"
	"lesiw.io/fs/path"
)

// A Command is one command line from an rc file.
type Command struct {
	Src string // base name of the file that defined the command
	Env []string
	Arg []string
}

// FindDir returns the nearest directory at or above the working directory
// that contains any of the named files.
func FindDir(ctx context.Context, fsys fs.FS, names ...string) string {
	dir, err := fs.Abs(ctx, fsys, ".")
	if err != nil {
		return ""
	}
	for {
		for _, base := range names {
			info, err := fs.Stat(ctx, fsys, path.Join(dir, base))
			if err == nil && !info.IsDir() {
				return dir
			}
		}
		parent := path.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// ParseFile parses one rc file of verb-keyed commands, expanding each line
// with expand. It returns nil and no error if name does not exist.
func ParseFile(ctx context.Context, fsys fs.FS, name string, expand func(string) string) (map[string][]Command, error) {
	f, err := open(ctx, fsys, name)
	if f == nil {
		return nil, err
	}
	defer f.Close()
	rc, err := parseVerbs(f, expand)
	for _, cmds := range rc {
		stamp(cmds, path.Base(name))
	}
	return rc, err
}

// ParseListFile parses one rc file of commands without verbs, one command
// per line, expanding each line with expand. It returns nil and no error if
// name does not exist.
func ParseListFile(ctx context.Context, fsys fs.FS, name string, expand func(string) string) ([]Command, error) {
	f, err := open(ctx, fsys, name)
	if f == nil {
		return nil, err
	}
	defer f.Close()
	cmds, err := parseList(f, expand)
	stamp(cmds, path.Base(name))
	return cmds, err
}

// open opens an rc file for parsing, returning nil without error if name
// does not exist or is a directory.
func open(ctx context.Context, fsys fs.FS, name string) (fs.ReadPathCloser, error) {
	info, err := fs.Stat(ctx, fsys, name)
	if err != nil || info.IsDir() {
		return nil, nil
	}
	return fs.Open(ctx, fsys, name)
}

func stamp(cmds []Command, src string) {
	for i := range cmds {
		cmds[i].Src = src
	}
}

func parseVerbs(r io.Reader, expand func(string) string) (map[string][]Command, error) {
	var (
		rc    = make(map[string][]Command)
		scn   = bufio.NewScanner(r)
		verb  string
		block bool
	)
	for n := 1; scn.Scan(); n++ {
		words, err := split(expand(scn.Text()))
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", n, err)
		}
		switch {
		case len(words) == 0:
		case block && len(words) == 1 && words[0] == ")":
			block = false
		case block:
			cmd, err := parseCommand(words)
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", n, err)
			}
			rc[verb] = append(rc[verb], cmd)
		case len(words) == 2 && words[1] == "(":
			verb, block = words[0], true
		default:
			cmd, err := parseCommand(words[1:])
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", n, err)
			}
			rc[words[0]] = append(rc[words[0]], cmd)
		}
	}
	if err := scn.Err(); err != nil {
		return nil, err
	}
	if block {
		return nil, fmt.Errorf("unclosed block")
	}
	return rc, nil
}

func parseList(r io.Reader, expand func(string) string) (cmds []Command, err error) {
	scn := bufio.NewScanner(r)
	for n := 1; scn.Scan(); n++ {
		words, err := split(expand(scn.Text()))
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", n, err)
		}
		if len(words) == 0 {
			continue
		}
		cmd, err := parseCommand(words)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", n, err)
		}
		cmds = append(cmds, cmd)
	}
	if err := scn.Err(); err != nil {
		return nil, err
	}
	return cmds, nil
}

// parseCommand separates leading environment variable assignments from the
// program and its arguments.
func parseCommand(words []string) (c Command, err error) {
	for len(words) > 0 {
		env, ok := assignment(words[0])
		if !ok {
			break
		}
		c.Env = append(c.Env, env)
		words = words[1:]
	}
	if len(words) == 0 {
		return Command{}, fmt.Errorf("missing command")
	}
	c.Arg = words
	return c, nil
}

// assignment reports whether word is an environment variable assignment of the
// form KEY=VALUE, KEY='VALUE', or KEY="VALUE", and returns it with the quotes
// around the value removed. Values that are not fully quoted are used as
// written.
func assignment(word string) (string, bool) {
	k, v, ok := strings.Cut(word, "=")
	if !ok || k == "" {
		return "", false
	}
	for i, r := range k {
		if r == '_' || unicode.IsLetter(r) {
			continue
		}
		if i == 0 || !unicode.IsDigit(r) {
			return "", false
		}
	}
	if len(v) >= 2 && v[0] == '\'' && v[len(v)-1] == '\'' {
		v = v[1 : len(v)-1]
	} else if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		if u, err := strconv.Unquote(v); err == nil {
			v = u
		}
	}
	return k + "=" + v, true
}

// split breaks a line into words following the rules of go:generate
// directives: words are separated by spaces and tabs, and a word beginning
// with a double quote is a Go string literal. A word beginning with //
// introduces a comment that runs to the end of the line.
func split(line string) (words []string, err error) {
loop:
	for {
		line = strings.TrimLeft(line, " \t")
		if line == "" || strings.HasPrefix(line, "//") {
			return words, nil
		}
		if line[0] == '"' {
			for i := 1; i < len(line); i++ {
				switch line[i] {
				case '\\':
					if i+1 == len(line) {
						return nil, fmt.Errorf("bad backslash")
					}
					i++
				case '"':
					word, err := strconv.Unquote(line[:i+1])
					if err != nil {
						return nil, fmt.Errorf("bad quoted string")
					}
					words = append(words, word)
					line = line[i+1:]
					if line != "" && line[0] != ' ' && line[0] != '\t' {
						return nil, fmt.Errorf(
							"expect space after quoted argument",
						)
					}
					continue loop
				}
			}
			return nil, fmt.Errorf("mismatched quoted string")
		}
		i := strings.IndexAny(line, " \t")
		if i < 0 {
			i = len(line)
		}
		words = append(words, line[:i])
		line = line[i:]
	}
}
