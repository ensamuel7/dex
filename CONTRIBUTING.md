# Contributing to DexLang

Thanks for your interest in contributing to DexLang! This document covers the basics of getting set up and submitting changes.

## Prerequisites

- **Go 1.24+**
- **A C compiler** (`gcc` or `cc`)
- Optional: **libcurl** (for HTTP client features), **SQLite/PostgreSQL/MySQL/MongoDB** client libraries (for database features)

## Building

```bash
# Build the compiler
go build -o dex

# Or run directly with Go
go run . run examples/hello.dx
```

## Running Tests

DexLang has two types of tests:

### Go unit tests
Tests for individual compiler stages (lexer, parser, checker, codegen):
```bash
go test ./...
```

### DexLang integration tests
End-to-end tests that compile and run `.dx` programs:
```bash
go run . test
```

See the [Language Reference](LANGUAGE.md#testing) for test file conventions and the `assert()` built-in.

## Project Structure

```
ast/        AST node definitions and type system
lexer/      Tokenizer
parser/     Parser (tokens -> AST)
checker/    Type checker and semantic analysis
codegen/    C code generation + C runtime (codegen/cruntime/)
stdlib/     Standard library module definitions (Go) + C runtime (stdlib/cruntime/)
lsp/        Language server protocol implementation
docgen/     Documentation generator (template.html + generate.go)
docs/       Generated documentation output (do not edit directly)
examples/   Example .dx programs
editors/    Editor extensions (VSCode)
```

## Compilation Pipeline

See the [Language Reference](LANGUAGE.md#compilation) for the full compilation pipeline.

## Making Changes

1. Fork the repo and create a branch from `main`
2. Make your changes
3. Add or update tests as needed
4. Run `go test ./...` and `go run . test` to verify
5. If you changed stdlib or language features, update `LANGUAGE.md`
6. If you changed anything that affects the docs, regenerate with `go run . docs`
7. Submit a pull request

## Coding Conventions

- Standard library functions use **camelCase** (e.g., `json.setArray`, `time.nowNs`)
- Go code follows standard `gofmt` formatting
- The C runtime uses `dex_` prefix for all public symbols
- Generated C code targets C99 with GCC extensions

## Areas Where Help Is Wanted

- Additional test coverage (especially for HTTP, JSON, and database features)
- Error message improvements
- New standard library modules
- Documentation improvements
- Bug reports and fixes
- Editor integrations beyond VSCode

## Reporting Bugs

Open an issue with:
- The `.dx` source code that triggers the bug
- Expected vs actual behavior
- Your OS and Go version

## Documentation

- **Never edit `docs/index.html` directly** — it is generated
- Edit `docgen/template.html` for HTML changes or `docgen/generate.go` for data logic
- Regenerate with `go run . docs`
- `LANGUAGE.md` is the language specification — update it when changing language features
