# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

`autodoc` is a Go project at `github.com/vukamecos/autodoc`. The repository is currently in early initialization — no source code exists yet.

## Getting Started

Once a `go.mod` is present, standard Go commands apply:

```bash
go build ./...       # build all packages
go test ./...        # run all tests
go test ./pkg/...    # run tests in a specific package
go vet ./...         # run static analysis
```

If a `Makefile` is added, prefer its targets over raw `go` commands.

## Conventions

- Follow standard Go project layout (`cmd/`, `internal/`, `pkg/` as appropriate).
- Use `go test -run TestFunctionName ./path/to/pkg` to run a single test.
