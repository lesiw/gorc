// Package shim locates and runs the real command a PATH shim stands in
// front of, and renders interception notices.
package shim

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Target returns the first executable named name in PATH that is not this
// executable.
func Target(name string) (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	selfInfo, err := os.Stat(self)
	if err != nil {
		return "", err
	}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			continue
		}
		found, err := exec.LookPath(filepath.Join(dir, name))
		if errors.Is(err, exec.ErrDot) {
			found, err = filepath.Abs(found)
		}
		if err != nil {
			continue
		}
		info, err := os.Stat(found)
		if err != nil || os.SameFile(info, selfInfo) {
			continue
		}
		return found, nil
	}
	return "", fmt.Errorf("%s not found", name)
}

// Fprintf formats like fmt.Fprintf, rendered dim and italic when w is a
// terminal.
func Fprintf(w io.Writer, format string, a ...any) (int, error) {
	if styled(w) {
		format = "\x1b[2;3m" + format + "\x1b[0m"
	}
	return fmt.Fprintf(w, format, a...)
}

// styled reports whether w renders ANSI styles.
func styled(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	term := os.Getenv("TERM")
	return term != "" && term != "dumb"
}

// Display renders words as a copy-pastable command line, quoting words that
// contain spaces or quotes.
func Display(words []string) string {
	quoted := make([]string, len(words))
	for i, w := range words {
		if w == "" || strings.ContainsAny(w, " \t'\"") {
			quoted[i] = strconv.Quote(w)
		} else {
			quoted[i] = w
		}
	}
	return strings.Join(quoted, " ")
}
