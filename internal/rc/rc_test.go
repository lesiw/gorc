package rc

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"lesiw.io/fs"
	"lesiw.io/fs/memfs"
)

var parseVerbsTests = []struct {
	name    string
	in      string
	want    map[string][]Command
	wantErr string
}{{
	name: "single line",
	in:   "test go test -count=5\n",
	want: map[string][]Command{
		"test": {{Arg: []string{"go", "test", "-count=5"}}},
	},
}, {
	name: "block",
	in: `test (
	CGO_ENABLED=0 go test -shuffle=on
	CGO_ENABLED=1 go test -race
)
`,
	want: map[string][]Command{
		"test": {{
			Env: []string{"CGO_ENABLED=0"},
			Arg: []string{"go", "test", "-shuffle=on"},
		}, {
			Env: []string{"CGO_ENABLED=1"},
			Arg: []string{"go", "test", "-race"},
		}},
	},
}, {
	name: "repeated verbs accumulate",
	in: `test go test -count=1
test go test -race
`,
	want: map[string][]Command{
		"test": {
			{Arg: []string{"go", "test", "-count=1"}},
			{Arg: []string{"go", "test", "-race"}},
		},
	},
}, {
	name: "line and block accumulate",
	in: `test go test -count=1
test (
	go test -race
)
`,
	want: map[string][]Command{
		"test": {
			{Arg: []string{"go", "test", "-count=1"}},
			{Arg: []string{"go", "test", "-race"}},
		},
	},
}, {
	name: "comments and blank lines",
	in: `// leading comment
test go test // trailing comment

vet go tool labs.lesiw.io/vet
`,
	want: map[string][]Command{
		"test": {{Arg: []string{"go", "test"}}},
		"vet":  {{Arg: []string{"go", "tool", "labs.lesiw.io/vet"}}},
	},
}, {
	name: "quoted argument",
	in:   "run go run . \"hello world\"\n",
	want: map[string][]Command{
		"run": {{Arg: []string{"go", "run", ".", "hello world"}}},
	},
}, {
	name: "env quoting forms",
	in: `test A=1 B='two' C="three\t3" go test
`,
	want: map[string][]Command{
		"test": {{
			Env: []string{"A=1", "B=two", "C=three\t3"},
			Arg: []string{"go", "test"},
		}},
	},
}, {
	name: "quoted whole-word env value",
	in:   "test \"CGO_FLAGS=a b\" go test\n",
	want: map[string][]Command{
		"test": {{
			Env: []string{"CGO_FLAGS=a b"},
			Arg: []string{"go", "test"},
		}},
	},
}, {
	name:    "missing command",
	in:      "test\n",
	wantErr: "missing command",
}, {
	name:    "env only",
	in:      "test CGO_ENABLED=0\n",
	wantErr: "missing command",
}, {
	name:    "unclosed block",
	in:      "test (\n\tgo test\n",
	wantErr: "unclosed block",
}, {
	name:    "unclosed quote",
	in:      "test go run . \"hello\n",
	wantErr: "mismatched quoted string",
}, {
	name:    "text after quote",
	in:      "test go run . \"hello\"x\n",
	wantErr: "expect space after quoted argument",
}}

func TestParseVerbs(t *testing.T) {
	for _, tt := range parseVerbsTests {
		t.Run(tt.name, func(t *testing.T) {
			rc, err := parseVerbs(
				strings.NewReader(tt.in), func(s string) string { return s },
			)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("parseVerbs(%q): want error %q, got nil",
						tt.in, tt.wantErr,
					)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("parseVerbs(%q): want error %q, got %q",
						tt.in, tt.wantErr, err,
					)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseVerbs(%q): %v", tt.in, err)
			}
			if !cmp.Equal(tt.want, rc) {
				t.Errorf("parseVerbs(%q): -want +got\n%s",
					tt.in, cmp.Diff(tt.want, rc),
				)
			}
		})
	}
}

var parseListTests = []struct {
	name    string
	in      string
	want    []Command
	wantErr string
}{{
	name: "commands in order",
	in: `gofmt -s
go tool goimports -local=lesiw.io
`,
	want: []Command{
		{Arg: []string{"gofmt", "-s"}},
		{Arg: []string{"go", "tool", "goimports", "-local=lesiw.io"}},
	},
}, {
	name: "comments and blank lines",
	in: `// leading comment
gofmt -s // trailing comment

gofumpt
`,
	want: []Command{
		{Arg: []string{"gofmt", "-s"}},
		{Arg: []string{"gofumpt"}},
	},
}, {
	name: "env assignment",
	in:   "GOWORK=off go tool goimports\n",
	want: []Command{{
		Env: []string{"GOWORK=off"},
		Arg: []string{"go", "tool", "goimports"},
	}},
}, {
	name:    "env only",
	in:      "CGO_ENABLED=0\n",
	wantErr: "missing command",
}, {
	name:    "unclosed quote",
	in:      "gofmt \"-r\n",
	wantErr: "mismatched quoted string",
}}

func TestParseList(t *testing.T) {
	for _, tt := range parseListTests {
		t.Run(tt.name, func(t *testing.T) {
			cmds, err := parseList(
				strings.NewReader(tt.in), func(s string) string { return s },
			)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("parseList(%q): want error %q, got nil",
						tt.in, tt.wantErr,
					)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("parseList(%q): want error %q, got %q",
						tt.in, tt.wantErr, err,
					)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseList(%q): %v", tt.in, err)
			}
			if !cmp.Equal(tt.want, cmds) {
				t.Errorf("parseList(%q): -want +got\n%s",
					tt.in, cmp.Diff(tt.want, cmds),
				)
			}
		})
	}
}

var assignmentTests = []struct {
	in   string
	want string
	ok   bool
}{
	{"KEY=VALUE", "KEY=VALUE", true},
	{"KEY='VALUE'", "KEY=VALUE", true},
	{`KEY="VALUE"`, "KEY=VALUE", true},
	{`KEY="tab\there"`, "KEY=tab\there", true},
	{"KEY=", "KEY=", true},
	{"_K1=x", "_K1=x", true},
	{"KEY=a b", "KEY=a b", true},
	{"go", "", false},
	{"=VALUE", "", false},
	{"1KEY=VALUE", "", false},
	{"K-EY=VALUE", "", false},
	{"./cmd=x", "", false},
	{`KEY="bad`, `KEY="bad`, true},
}

func TestAssignment(t *testing.T) {
	for _, tt := range assignmentTests {
		got, ok := assignment(tt.in)
		if got != tt.want || ok != tt.ok {
			t.Errorf("assignment(%q) = %q, %v; want %q, %v",
				tt.in, got, ok, tt.want, tt.ok,
			)
		}
	}
}

func TestFindDir(t *testing.T) {
	fsys := memfs.New()
	ctx := fs.WithWorkDir(t.Context(), "/a/b/c")
	if err := fs.MkdirAll(ctx, fsys, "/a/b/c"); err != nil {
		t.Fatal(err)
	}
	err := fs.WriteFile(ctx, fsys, "/a/go.rc", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := FindDir(ctx, fsys, "go.rc", "go.local.rc"); got != "/a" {
		t.Errorf("FindDir() = %q; want %q", got, "/a")
	}
}

func TestFindDirSecondName(t *testing.T) {
	fsys := memfs.New()
	ctx := fs.WithWorkDir(t.Context(), "/a/b")
	if err := fs.MkdirAll(ctx, fsys, "/a/b"); err != nil {
		t.Fatal(err)
	}
	err := fs.WriteFile(ctx, fsys, "/a/go.local.rc", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := FindDir(ctx, fsys, "go.rc", "go.local.rc"); got != "/a" {
		t.Errorf("FindDir() = %q; want %q", got, "/a")
	}
}

func TestFindDirNone(t *testing.T) {
	fsys := memfs.New()
	ctx := fs.WithWorkDir(t.Context(), "/a/b/c")
	if got := FindDir(ctx, fsys, "go.rc"); got != "" {
		t.Errorf("FindDir() = %q; want %q", got, "")
	}
}

func TestParseFileStampsSource(t *testing.T) {
	fsys := memfs.New()
	ctx := fs.WithWorkDir(t.Context(), "/work")
	if err := fs.MkdirAll(ctx, fsys, "/work"); err != nil {
		t.Fatal(err)
	}
	err := fs.WriteFile(ctx, fsys, "/work/go.local.rc",
		[]byte("test go test\n"),
	)
	if err != nil {
		t.Fatal(err)
	}
	cmds, err := ParseFile(ctx, fsys, "/work/go.local.rc",
		func(s string) string { return s },
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := cmds["test"][0].Src; got != "go.local.rc" {
		t.Errorf("Src = %q; want %q", got, "go.local.rc")
	}
}

func TestParseListFileStampsSource(t *testing.T) {
	fsys := memfs.New()
	ctx := fs.WithWorkDir(t.Context(), "/work")
	if err := fs.MkdirAll(ctx, fsys, "/work"); err != nil {
		t.Fatal(err)
	}
	err := fs.WriteFile(ctx, fsys, "/work/go.fmt", []byte("gofmt -s\n"))
	if err != nil {
		t.Fatal(err)
	}
	cmds, err := ParseListFile(ctx, fsys, "/work/go.fmt",
		func(s string) string { return s },
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := cmds[0].Src; got != "go.fmt" {
		t.Errorf("Src = %q; want %q", got, "go.fmt")
	}
}

func TestParseFileMissing(t *testing.T) {
	fsys := memfs.New()
	ctx := fs.WithWorkDir(t.Context(), "/work")
	cmds, err := ParseFile(ctx, fsys, "/work/go.rc",
		func(s string) string { return s },
	)
	if err != nil {
		t.Fatalf("ParseFile() = %v; want nil", err)
	}
	if cmds != nil {
		t.Errorf("ParseFile() = %v; want nil", cmds)
	}
}
