//go:build !unix && !plan9

package shim

import (
	"context"

	"lesiw.io/command"
)

func Exec(ctx context.Context, m command.Machine, path string, argv []string) error {
	return command.Exec(ctx, m, append([]string{path}, argv[1:]...)...)
}
