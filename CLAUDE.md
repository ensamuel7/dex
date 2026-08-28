# DexLang - Claude Code Guidelines

## Documentation

- **Never edit `docs/index.html` directly.** It is a generated file.
- Edit `docgen/template.html` for HTML/layout/content changes, or `docgen/generate.go` for data-generation logic.
- After editing, regenerate with: `go run . docs`
- The template uses Go `html/template` syntax. Dynamic data (modules, functions, keywords, examples) comes from `generate.go`.
- `LANGUAGE.md` is the language specification and reference. Consult it for syntax, semantics, and feature details when making changes to the compiler or docs.

## Build & Run

- Build a `.dx` file: `go run . build <file.dx>`
- Run a `.dx` file: `go run . run <file.dx>`
- Run tests: `go run . test`
- Generate docs: `go run . docs`
- Start LSP: `go run . lsp`

## Project Structure

- `ast/` - AST node definitions and type system
- `lexer/` - Tokenizer
- `parser/` - Parser (tokens -> AST)
- `checker/` - Type checker and semantic analysis
- `codegen/` - C code generation
- `stdlib/` - Standard library module definitions (Go) and C runtime (`stdlib/cruntime/`)
- `docgen/` - Documentation generator (`template.html` + `generate.go`)
- `docs/` - Generated output (do not edit)
- `examples/` - Example `.dx` programs
- `lsp/` - Language server protocol implementation

## Compilation Pipeline

`.dx` source -> lexer (tokens) -> parser (AST) -> checker (type checking) -> codegen (C code) -> cc/gcc -> native binary

## Key Patterns

- Polymorphic stdlib functions (e.g. `json.encode`, `db.col`) use `Params: nil, CName: ""` in their `FuncDef` and are special-cased in `checker/checker.go` and `codegen/codegen.go`.
- The `json` module is `json.encode` / `json.decode` plus the `json.Value` type. The
  fifteen older functions (`set`, `get*`, `array*`, ...) are deprecated and warn; do
  not use them in new examples or docs.
- `CallExpr.ResolvedType` is used for return-type polymorphism (set by checker, read by codegen).
- Concurrency (`spawn`, `send`, `receive`, `channel`, `close`) compiles to pthreads + lock-free channels in C.

## Naming Conventions

- DexLang standard library functions use **camelCase** (e.g. `json.encode`, `time.nowNs`), not snake_case.
- After any compiler or stdlib change, always regenerate docs with `go run . docs` and update `LANGUAGE.md` if the change affects the language API.

## Verifying a change

A compiler change is not verified until all three pass:

1. `go test ./...` — compiler unit tests
2. `go run . test` — the `.dx` example suite in `examples/`
3. A leak sweep — every example must report zero leaks:

```sh
for f in examples/*_test.dx; do
  b=$(basename $f .dx)
  go run . build $f >/dev/null 2>&1
  leaks --atExit -- ./build/$b 2>&1 | grep -oE "[0-9]+ leaks? for"
done
```

Memory correctness is a recurring problem area: a change that passes tests but leaks
is not finished. `DEX_KEEP_C=1 go run . build <file>.dx` keeps the generated C in
`build/` so ownership decisions can be inspected directly.

## Memory ownership rules

These are the invariants the codegen maintains. Breaking one produces either a leak
or a use-after-free, so change them deliberately:

- A heap value (`DexString`, arrays, maps, channels, `DexJsonValue`) is refcounted.
  An expression either **mints** a reference (+1, the consumer owns it) or **borrows**
  one (the consumer must not release it). `isNewAlloc` in `gen_stmt_helpers.go` is the
  single source of truth for which.
- A value passed somewhere that only *reads* it — a print, a borrowed function
  argument, a map key, a `StringBuilder.append`, a string comparison — must go through
  `genBorrowed`, which hoists an owned temporary into a statement-scoped variable and
  releases it after the statement.
- A struct **owns** its heap fields: they are released when the struct goes out of
  scope. A struct literal that stores a *borrowed* value must therefore retain it
  (`emitRetainStructLitFields`), including when the literal is returned from a
  function with no locals of its own.
- `json.Value` has its own ownership rules — indexing a document mints a reference
  where indexing an array only borrows one. See `jsonValueOwned` in `gen_jsonvalue.go`.

## Post-Push Checks

- After every push, run code coverage to verify that functions are actually used: `go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out`
- Review the output to ensure new or modified functions have test coverage and are not dead code.
