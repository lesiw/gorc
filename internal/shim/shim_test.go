package shim

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestFind builds a PATH whose first directory holds this test binary
// itself, searched under its own name, and whose second holds another
// executable of the same name. Find must skip itself and return the other.
func TestFind(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	name := strings.TrimSuffix(filepath.Base(exe), ".exe")
	realDir := t.TempDir()
	real := filepath.Join(realDir, exeName(name))
	if err := os.WriteFile(real, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", strings.Join(
		[]string{filepath.Dir(exe), realDir}, string(os.PathListSeparator),
	))
	got, err := Target(name)
	if err != nil {
		t.Fatal(err)
	}
	if got != real {
		t.Errorf("Find(%q) = %q; want %q", name, got, real)
	}
}

func TestFindOnlySelf(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	name := strings.TrimSuffix(filepath.Base(exe), ".exe")
	t.Setenv("PATH", filepath.Dir(exe))
	if got, err := Target(name); err == nil {
		t.Errorf("Find(%q) = %q; want error", name, got)
	}
}

var displayTests = []struct {
	in   []string
	want string
}{
	{[]string{"go", "test", "-count=5"}, "go test -count=5"},
	{[]string{"go", "run", ".", "hello world"},
		`go run . "hello world"`},
	{[]string{"A=x y", "go"}, `"A=x y" go`},
	{[]string{""}, `""`},
}

func TestDisplay(t *testing.T) {
	for _, tt := range displayTests {
		if got := Display(tt.in); got != tt.want {
			t.Errorf("Display(%q) = %q; want %q", tt.in, got, tt.want)
		}
	}
}

func exeName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}
