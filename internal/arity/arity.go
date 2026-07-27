// Package arity records which go command flags consume the following
// word as their value when not written as -flag=value.
package arity

//go:generate go run gen.go

import (
	"context"
	"regexp"
	"strings"

	"lesiw.io/command"
)

var entry = regexp.MustCompile(`^\t-([^\s=]+)( \S.*)?$`)

// Parse extracts a flag table from the output of a go help topic.
func Parse(help string) map[string]bool {
	flags := make(map[string]bool)
	for line := range strings.Lines(help) {
		match := entry.FindStringSubmatch(strings.TrimRight(line, "\n"))
		if match == nil {
			continue
		}
		flags[match[1]] = flags[match[1]] || match[2] != ""
	}
	return flags
}

// Load parses the flag table from the go command itself.
func Load(ctx context.Context, m command.Machine) (map[string]bool, error) {
	flags := make(map[string]bool)
	for _, topic := range []string{"build", "testflag"} {
		help, err := command.Read(ctx, m, "go", "help", topic)
		if err != nil {
			return nil, err
		}
		for name, takesValue := range Parse(help) {
			flags[name] = flags[name] || takesValue
		}
	}
	return flags, nil
}
