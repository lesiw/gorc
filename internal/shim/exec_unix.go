//go:build unix || plan9

package shim

import (
	"context"
	"os"
	"syscall"

	"lesiw.io/command"
)

func Exec(_ context.Context, _ command.Machine, path string, argv []string) error {
	return syscall.Exec(path, argv, os.Environ())
}
