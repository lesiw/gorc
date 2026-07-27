# lesiw.io/gorc

[![Go Reference](https://pkg.go.dev/badge/lesiw.io/gorc/go.svg)](https://pkg.go.dev/lesiw.io/gorc/go)
[![CI](https://github.com/lesiw/gorc/actions/workflows/main.yml/badge.svg?branch=main)](https://github.com/lesiw/gorc/actions/workflows/main.yml)
[![Release](https://img.shields.io/github/v/tag/lesiw/gorc?sort=semver&label=release)](https://github.com/lesiw/gorc/tags)
[![Go Version](https://img.shields.io/github/go-mod/go-version/lesiw/gorc)](go.mod)
[![Discord](https://img.shields.io/discord/1145827224516300971?logo=discord&logoColor=white&color=5865F2&label=discord)](https://lesiw.dev/discord)
[![License](https://img.shields.io/github/license/lesiw/gorc)](LICENSE)

gorc runs the go command with per-project command definitions from
a go.rc file. A companion gofmt shim does the same for formatting,
applying format commands from a go.fmt file.

```
test (
    CGO_ENABLED=0 go test $GOARGS -shuffle=on -timeout=5s $GOFLAGS
    CGO_ENABLED=1 go test $GOARGS -shuffle=on -timeout=30s -race $GOFLAGS
)
vet go tool golangci-lint run $GOARGS
```

With gorc installed, running a defined verb runs the configured
commands in its place. Every other invocation passes through to
the real go command unchanged:

```
$ go test ./...
go.rc: CGO_ENABLED=0 go test ./... -shuffle=on -timeout=5s
go.rc: CGO_ENABLED=1 go test ./... -shuffle=on -timeout=30s -race
```

## Quick Start

```sh
go install lesiw.io/gorc/go@latest
go install lesiw.io/gorc/gofmt@latest
```

The Go bin directory must come before the Go distribution in PATH:

```sh
export PATH="$(go env GOPATH)/bin:$PATH"
```

## go.rc

The nearest go.rc, found by searching upward from the current
directory, defines the project's verbs. Groups run in order and
stop at the first failure:

```
test (
    CGO_ENABLED=0 go test -shuffle=on
    CGO_ENABLED=1 go test -shuffle=on -race
)
```

New verbs define new subcommands:

```
xcompile GOOS=linux GOARCH=arm64 go build $GOARGS
```

Commands run exactly as written. Arguments after the verb carry
over only where a command asks for them: `$GOARGS` expands to the
invocation's arguments, such as package patterns, and `$GOFLAGS`
expands to its flags:

```
test go test $GOARGS -shuffle=on $GOFLAGS
```

Here `go test ./... -run TestFoo` runs
`go test ./... -shuffle=on -run TestFoo`. Editor and IDE test
runners keep working: their `-run`, `-timeout`, and `-json` flags
flow into the configured commands. Flags written after `$GOFLAGS`
cannot be overridden by the caller. Spell values of flags the go
command does not document as `-flag=value`. Every other `$NAME`
expands from the environment.

Commands are not interpreted by a shell — no pipes, redirections,
or globs. Lines split into words by go:generate's rules: spaces
and tabs separate words, a word beginning with a double quote is a
Go string literal, `//` introduces a comment, and a command may
begin with `KEY=VALUE` environment assignments. Full syntax:
[pkg.go.dev/lesiw.io/gorc/go](https://pkg.go.dev/lesiw.io/gorc/go).

A go.local.rc, typically kept out of version control, overrides
the go.rc in its directory verb by verb — a longer test timeout on
a slow machine, a personal verb in a project that has no go.rc.

Setting GORC to any non-empty value disables interception.

```
GORC=0 go test -count=1 ./... // Runs the real go test.
```

## go.fmt

The gofmt shim reads a go.fmt, found by the same upward search: a
list of format commands, each reading Go source on standard input
and writing the result to standard output, piped in order:

```
gofmt -s
go tool goimports -local=example.com
```

The result presents through the standard gofmt interface: `-l`
lists, `-w` rewrites, `-d` prints diffs, and files, directories,
and standard input work as usual. Other gofmt flags are accepted
and ignored: go.fmt defines the formatting. Anything that shells
out to gofmt — editors, scripts, agents — gets the project's
formatter pipeline with no configuration of its own. The same
GORC switch applies.

```
GORC=0 gofmt -l . // Run the real gofmt.
```

## Motivation

Most Go developers use the go command as their make equivalent. We
collectively know what go fmt, go test, go build, go generate, and
now go fix mean. This is already an elegant and well-understood
system. go.rc adds per-project defaults and preferred tooling.

go.rc deliberately avoids introducing a new build language. The
format is spartan, borrowing the factored block syntax of go.mod.
Anyone needing more complex
behavior already has the perfect tool to write it in: go tool, and
more Go. And a note for anyone writing complex build automation:
try [command buffers](https://cmdbuf.io) next time you reach for
os/exec.
