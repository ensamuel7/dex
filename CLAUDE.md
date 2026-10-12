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
- A function value is a **closure**: `DexClosure*`, a code pointer plus the
  environment it carries, with the environment passed as a hidden first argument
  so every kind is invoked identically. A plain function gets a thunk that
  ignores it; a lambda gets a generated environment struct; a method value gets a
  copy of its receiver. See `codegen/gen_closure.go` and `cruntime/closure.c`.
- Concurrency (`spawn`, `send`, `receive`, `channel`, `close`) compiles to pthreads + lock-free channels in C.

## Naming Conventions

- DexLang standard library functions use **camelCase** (e.g. `json.encode`, `time.nowNs`), not snake_case.
- After any compiler or stdlib change, always regenerate docs with `go run . docs` and update `LANGUAGE.md` if the change affects the language API.

## Verifying a change

The editor extension shells out to whatever `dex` is on PATH, so after any
parser, checker, or LSP change run `make install` — otherwise the IDE reports
errors the current compiler does not have. The language server is spawned once
per session, so it also needs restarting to pick the new binary up.

A compiler change is not verified until all four pass:

1. `go test ./...` — compiler unit tests
2. `go run . test` — the `.dx` example suite in `examples/`
3. `make memcheck` — the same suite under the debug memory checker
4. A leak sweep — every example must report zero leaks:

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

## Finding a memory error

`DEX_MEM_CHECK=1 go run . build <file>.dx` builds with the debug memory
checker (`codegen/cruntime/memcheck.c`). It widens every object header with a
magic word, a size, and the `.dx` statement that allocated it, keeps a registry
of live objects, and parks each freed object in a quarantine ring with its
payload poisoned instead of handing it straight back to `malloc`. Codegen emits
a `DEX_AT("file.dx:line")` marker ahead of every statement so an allocation is
attributed to the line that made it.

What it turns into a diagnosis rather than a mystery:

- a release or retain of an already-freed object — panics *at that release*,
  naming the statement that allocated it and the one that freed it;
- a release of something that is not a managed object;
- a reference count that would go negative;
- a read through a freed pointer, which sees `0xDD` rather than plausible data;
- everything still alive at exit, grouped by allocating statement.

Runtime switches: `DEX_MEM_LEAKS=error` makes objects-alive-at-exit a failing
exit status (70) — the default only reports them, because a handler registered
with `http.route` is owned by a table that outlives `main`. `DEX_MEM_VERBOSE=1`
confirms a clean run. `DEX_MEM_ABORT=1` aborts on an error instead of exiting,
for a core dump; the default is a fast exit because `abort()` on macOS hands the
process to the crash reporter and looks like a hang.

It complements rather than replaces the two existing tools. `leaks` is
reachability-based and conservatively scans the stack, so it reports zero for a
closure whose only remaining pointer is a dead stack slot — the checker catches
those. The quarantine is bounded, so a double release separated by thousands of
intervening frees can still reach real freed memory; `DEX_SANITIZE=1` (ASan)
catches that. The checker knows about `.dx` statements and reference counts,
ASan knows about the whole heap.

It cannot find everything at compile time, and will not: whether a release is
the last one is a runtime fact, and a reference cycle is not statically
decidable at all — which is why `#[debug(cycles)]` is a runtime tool too.

## Exceptions leak the frames they unwind

A `throw` is `longjmp`, so every frame between the throw and the catch is
skipped: its heap locals are never released and its `defer`s never run. Two
heap locals per unwound frame, per throw — measured at 600 objects for 300
throws through one intermediate frame. The thrown *message* is handled (it is
staged into a thread-local buffer, the string released, and the longjmp done
last — see `_dex_exc_stage`), but the unwound frames' own locals are not.

So a throw on a hot error path accumulates. Keep heap locals out of functions a
throw passes through, or use a return value rather than an exception there.
Fixing it properly needs a per-frame cleanup list the runtime can walk on
unwind, which costs a push per heap local everywhere — a deliberate trade, not a
patch.

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
  function with no locals of its own. **This recurses.** A nested struct field is
  a value, not a heap type, but the release pass descends into it — so the retain
  pass has to as well, or `Result { output: entry }` returns freed strings while
  `entry` looked perfectly alive at the call site.
- **A container owns its elements.** `dex_array_string_push` retains what it is
  given and the array's destructor releases every slot, so an array literal
  built from a borrowed expression must let push take that reference and an
  owned temporary must be released after the push. All three array-literal
  sites go through `genArrayLitElem` for this; writing `arr->data[i]` directly
  is what made `["find", dir]` free the caller's `dir`, and it also wrote past
  the eight-slot initial capacity. Assigning into a slot (`names[0] = other`)
  is the same rule: release the old, retain a borrowed new one.
- A **struct array** is the same rule with the retain one level down.
  `dex_array_struct_push` is a `memcpy` and cannot retain what it copies —
  only the element type's cleanup function knows the layout — so the reference
  has to be taken at the push site. All four push sites go through
  `emitRetainPushedStructFields`; the three literal ones did not, and `[unit]`
  freed strings that `unit` still held. The generated `dex_cleanup_<Struct>`
  that the array's destructor runs goes through `emitReleaseVar`, so an element
  is torn down exactly as a local of the same type is — releasing only the
  top-level heap fields left nested struct fields and heap-backed optionals
  alive.
- **An identifier is not always a borrow.** Naming a top-level function where a
  value is expected compiles to a `dex_closure_new`, so it mints a reference
  despite being spelled as an `Ident` — `isFuncValueIdent` is the carve-out, the
  mirror of `IsMethodValue` for a field access. Without it `let f = twice`
  retained a closure that already held the only reference there was, and
  `apply(twice, 4)` minted one that nothing released.
- **A row filler owns what it stores, the other way round.** `db.fill` assigns a
  freshly-read column into a string field, and `dex_db_col_dexstr` already
  minted that reference — so the field's old one is released and the new one
  moves in with *no* retain. Retaining there would leak one reference per row.
  That is the mirror of the setter rule below, where the value arrives borrowed.
- **A reflective write owns what it stores.** `reflect.setString` releases what
  the field held and retains what replaced it, retain *first* — the two can be
  the same object when a field is written from a borrowed read of itself. The
  value argument goes through `genBorrowed`, so a string built for the call is
  released after the statement rather than handed over; the setter takes its own
  reference, the same convention `dex_array_string_push` follows.
  `checkReflectSetterSubject` is the other half: a by-value struct parameter is
  a copy sharing the caller's heap pointers, so writing one would release a
  reference the caller still holds, and an array element would write into the
  shallow copy `genReflectSubject` makes for reads. Both are compile errors, and
  `genReflectMutableSubject` therefore has no element path at all.
- `json.Value` has its own ownership rules — indexing a document mints a reference
  where indexing an array only borrows one. See `jsonValueOwned` in `gen_jsonvalue.go`.
- A method value is a fresh closure even though it reads like a field access, so
  ownership questions go through `borrowsHeapValue`, never `isBorrowedExpr`
  directly.
- Traversals over the AST use the shared walker in `resolve/resolve.go`
  (`walkStmtsWith`, `walkExpr`). Hand-written walkers drift: both the module
  prefixer and the method-body field rewriter had silently stopped visiting
  `switch` bodies and object literals.

## Post-Push Checks

- After every push, run code coverage to verify that functions are actually used: `go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out`
- Review the output to ensure new or modified functions have test coverage and are not dead code.
