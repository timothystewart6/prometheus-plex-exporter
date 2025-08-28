
# Contributing

Thanks for wanting to contribute! This file documents the development workflow, local checks, and how to run the project's quality gates locally before opening a PR.

## Pre-commit checks

This repository includes a pre-commit hook in `.githooks/pre-commit` that will:

- run `gofmt -s -w` on staged `.go` files and re-add them to the index
- run the configured linters (prefer `golangci-lint` if installed, otherwise falls back to `go vet`)
- run the full test suite (`go test ./...`)

To enable the hook locally (one-time):

```bash
git config core.hooksPath .githooks
chmod +x .githooks/pre-commit
```

You can skip the hook for a single commit with `git commit --no-verify`, but avoid doing that for PRs unless there is a valid reason.

## Installing the recommended linter (golangci-lint)

We recommend installing `golangci-lint` for fast, consistent linting. It runs many linters in parallel and respects the project's `.golangci.yml`.

macOS (Homebrew):

```bash
brew install golangci-lint
```

Linux (bash):

```bash
curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin v1.59.0
# ensure $(go env GOPATH)/bin is in your PATH
```

Or see the official instructions: <https://golangci-lint.run/usage/install/>

## Running checks locally

Format code:

```bash
gofmt -s -w $(git ls-files '*.go')
```

Lint (preferred):

```bash
golangci-lint run ./...
```

Fallback lint (if you don't have golangci-lint):

```bash
go vet ./...
```

Run tests:

```bash
go test ./...
```

## Branching and PRs

1. Fork the repo and create a feature branch from `main`.
2. Make changes and ensure pre-commit checks pass locally.
3. Open a PR against `main` with a clear description of the change.

CI will run the linter and tests on your PR. Please address any lint/test failures before merging.

Thanks — contributions are welcome!
# Contributing

Thanks for wanting to contribute! This file documents the development workflow, local checks, and how to run the project's quality gates locally before opening a PR.

## Pre-commit checks

This repository includes a pre-commit hook in `.githooks/pre-commit` that will:

- run `gofmt -s -w` on staged `.go` files and re-add them to the index
- run the configured linters (prefer `golangci-lint` if installed, otherwise falls back to `go vet`)
- run the full test suite (`go test ./...`)

To enable the hook locally (one-time):

```bash
git config core.hooksPath .githooks
chmod +x .githooks/pre-commit
```

You can skip the hook for a single commit with `git commit --no-verify`, but avoid doing that for PRs unless there is a valid reason.

## Installing the recommended linter (golangci-lint)

We recommend installing `golangci-lint` for fast, consistent linting. It runs many linters in parallel and respects the project's `.golangci.yml`.

macOS (Homebrew):

```bash
brew install golangci-lint
```

Linux (bash):

```bash
curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin v1.59.0
# ensure $(go env GOPATH)/bin is in your PATH
```

Or see the official instructions: <https://golangci-lint.run/usage/install/>

## Running checks locally

Format code:

```bash
gofmt -s -w $(git ls-files '*.go')
```

Lint (preferred):

```bash
golangci-lint run ./...
```

Fallback lint (if you don't have golangci-lint):

```bash
go vet ./...
```

Run tests:

```bash
go test ./...
```

## Branching and PRs

1. Fork the repo and create a feature branch from `main`.
2. Make changes and ensure pre-commit checks pass locally.
3. Open a PR against `main` with a clear description of the change.

CI will run the linter and tests on your PR. Please address any lint/test failures before merging.

Thanks — contributions are welcome!
