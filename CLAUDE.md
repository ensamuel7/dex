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
- Lint a file: `go run . lint <file.dx>` (or `--lint` / `--lint=strict` on build)
- Generate docs: `go run . docs`
- Start LSP: `go run . lsp`

## Project Structure

- `ast/` - AST node definitions and type system
- `lexer/` - Tokenizer
- `parser/` - Parser (tokens -> AST)
- `checker/` - Type checker and semantic analysis
- `resolve/` - User module resolution, method flattening, the shared AST walker
- `lint/` - Lint rules run after checking (`dex lint`, `--lint`)
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

## What an unwind collects, and what it still leaves

A `throw` is `longjmp`, so every frame between the throw and the catch is
skipped: nothing it would have released on its way out runs. That used to mean
two heap locals leaked per unwound frame per throw — measured at 600 objects for
300 throws through one intermediate frame. (Deferred calls were skipped the same
way; they are a separate stack now, below.)

The runtime now keeps a **cleanup stack** (`_dex_cleanup_push` /
`_dex_cleanup_unwind_to` in `cruntime/exceptions.c`). Every heap value a frame
would release on its way out also registers there, and each try records the
stack's height before its `setjmp`; the caught branch releases everything above
that height, because those entries belong to frames that no longer exist. The
same measurement is now zero. `examples/unwind_test.dx` is the regression case.

Three properties hold it together, and breaking any one turns a leak into a
double free:

- **Entries hold the pointer, not the variable's address.** The unwind runs in
  the catching frame, whose own calls overwrite the stack memory the dead frames
  occupied, so reading a dead frame's slot would be a use-after-free.
- **A value that can be replaced keeps a slot.** Holding the pointer means the
  write that replaces a value — and releases it — has to point the entry at the
  new one, or the entry names freed memory. `_dex_cleanup_push` returns the slot
  it took and `_dex_cleanup_set` writes through it: one slot per reassignable
  local, one per heap field of a struct local whose fields are written or whose
  whole value is assigned. A value written once takes no slot.
- **Restoring the height only ever lowers it.** `_dex_cleanup_drop_to` is
  monotone, so two restores in either order — a statement's and the enclosing
  scope's — cannot bring a stale entry back into range.

A struct is registered a field at a time rather than as itself: releasing a
struct means releasing its fields through its address, which the unwind cannot
safely read. `registerStructFieldsForUnwind` does that, and skips a field that
is itself a struct or an optional rather than descending into it — those are
what still leak on an unwind, and the only thing that does.

Registration is guarded on `_dex_exc_top >= 0`: with no exception frame live a
throw exits the process and nothing unwinds, so the push is a load and a branch.
Nothing at all is emitted for a program that never throws (`usesExceptions`).

A new leak here is now a regression worth a test rather than a known cost —
`examples/unwind_test.dx` is the case.

## Leaving a try by jumping out of it

A try pushes its exception frame before the `setjmp` and pops it after the
handlers, so a `return`, `break` or `continue` in the **try body** skips that
pop. The frame stack is a fixed 64-entry thread-local array, so the skipped
pops accumulated until it was written past the end — silently, into whatever
thread-local followed it, which now includes the cleanup stack.

`emitExcFramePops` emits the missing pops at each jump site, from `tryDepth`
(incremented only while generating a try *body* — a catch or finally runs after
its own frame is popped). `_dex_exc_push_frame` bounds-checks entry and exits
naming the limit rather than overrunning.

The pops go **after** the return value is evaluated, which `emitReturn` is there
to enforce. A return expression can itself throw, and until it has produced a
value the enclosing try is still its handler: popping first sent
`return load(path)` to the caller's catch instead of the one wrapped around it.

## Deferred calls are a runtime stack, not emitted code

`defer` used to collect its expression at compile time and emit it at the
function's exit. That made it lexical: a defer in a branch nothing took still
ran, a defer in a loop ran once however many iterations there were, and an
exception unwinding past the frame skipped it — the case that leaves a lock
held, which is exactly what the docs recommended `defer` for.

It now compiles to a closure pushed onto a defer stack (`_dex_defer_push` in
`cruntime/closure.c`) where the statement runs, and `_dex_defer_run_to` runs
them most-recent-first at every exit: each `return`, the fall-through at the end
of a function, and the catch of a try the throw unwound past. `containsDefer`
decides whether a function records a base height at entry.

A closure is what makes the unwind case work: the call's operands are captured
and owned where the defer is written, rather than read out of a frame that may
be gone. The consequence is that arguments are evaluated at the `defer` rather
than at the exit, which is also what Go does. `examples/defer_test.dx` pins all
of it.

## A module's types belong to it

A type declared in a user module registers under that module — `alpha_Response`
— the way a function declared there becomes `alpha_make`. Two modules can each
declare a `Response`; before this a struct name was global and the second
declaration was rejected outright, while a qualified reference had its qualifier
discarded by the parser.

The identity is the flat name, so codegen's `Dex_alpha_Response` falls out with
no codegen change at all. What carries the work is resolution, in the parser,
which is the only place that knows which file it is reading:

- `Parser.module` is the module being parsed, and `SetModule` is called by
  `resolveModuleFile`. A type declared there registers qualified.
- `Parser.candidates` maps a bare name to the flat names in scope.
  `resolveTypeName` prefers the file's own module, then resolves a single
  candidate, then reports an ambiguity naming the alternatives, and finally
  falls through to the bare name — which is how the main program's own types
  and the struct types a stdlib module provides are still found.
- Every site that reads a type name goes through it: annotations, `&refs`,
  struct literals in both spellings, bare and qualified enum variants, and
  constructor calls, which the parser now resolves because only it knows the
  module.
- `StructDef.Module` and `EnumDef.Module` exist for diagnostics.
  `ast.DisplayStructName` turns the flat name back into `alpha.Response`, and
  both `typeName` renderers — the checker's and the LSP's — use it. Codegen must
  not: it needs the flat name.

A bare reference still reaches a module imported only indirectly, as it always
could. Narrowing that to direct imports would be defensible but would break
code that reads perfectly well.

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
  releases it after the statement. `genStringArg` is included: what reaches a
  stdlib callee is a raw `char*`, so the callee cannot own the `DexString`, and
  reading `->data` straight off a minted value leaked it once per call.
- **A condition is not a statement.** `if` and `while` generate their condition
  with no prelude for `genBorrowed` to hoist into, so a minted operand or
  argument there is leaked once per evaluation — in a loop, once per iteration.
  `genCondition` gives them one, emitted as a statement expression *inside* the
  condition rather than ahead of it: a `while` re-evaluates it along with the
  condition, and a release inside the condition cannot be skipped by a `return`
  or `break` in the body.
- **Hoisting out of a short-circuit changes behaviour, not just ownership.** The
  right-hand side of `&&` or `||` may never be evaluated, and a hoisted
  declaration evaluates it anyway: in
  `if (!s.isEmpty() && s.substring(0, 1) == "a")` that turns a leak into a crash.
  `genExpr` suspends hoisting for the operands of both operators, so anything
  inside one must be self-contained — which is why `genStringCompare` releases
  within a statement expression instead of asking for a hoist.
- **A field access borrows the field but not its subject.** `parse(x).ok`
  returns a struct nothing binds, and a struct owns its heap fields, so the
  subject goes through `genBorrowed` even though the read itself is a borrow.
- A struct **owns** its heap fields: they are released when the struct goes out of
  scope. A struct literal that stores a *borrowed* value must therefore retain it
  (`emitRetainStructLitFields`), including when the literal is returned from a
  function with no locals of its own. **This recurses.** A nested struct field is
  a value, not a heap type, but the release pass descends into it — so the retain
  pass has to as well, or `Result { output: entry }` returns freed strings while
  `entry` looked perfectly alive at the call site.
- **A struct has two spellings and one ownership rule.** `Point { x: a }` and
  `Point(a)` compile to the same C initialiser, so every retain site keys off
  `structAssignments`, which answers for both, rather than on `*StructLitExpr`.
  It did key on the literal, and the auto-constructor form took no reference at
  all: `return Result(status, message)` kept the caller's string, which was
  released at the call and released again at scope exit.
- **A predicate that gates a recursive pass has to recurse too.**
  `structLitBorrowsHeapField` decides whether a returned literal needs any
  retain, and it asked `IsHeapType` of each field. A nested struct is not a heap
  type, so `return Outer { inner: Inner { label: label } }` was declared to
  borrow nothing — and the retain pass that *does* recurse was never called.
  The returned struct carried a freed string, and comparing it failed against
  the value it had just been built from. `examples/structConstruct_test.dx`
  covers both spellings, nested and flat.
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
  `switch` bodies and object literals, and compound assignment, destructuring,
  lambda, match and interpolation bodies were missed for longer than that —
  which is why `x += mod.f()` was not module-prefixed.
- A pass that only *inspects* uses the read-only wrappers in `resolve/visit.go`
  (`VisitStmts`, `VisitExpr`) rather than the mutating walker, whose callback
  result is a replacement node: a visitor written against it truncates its own
  traversal the first time a callback returns something. `VisitExprInFrame`
  stops at a lambda or spawn body, for an analysis about what happens in *this*
  call — which exceptions escape it, say.

## Post-Push Checks

- After every push, run code coverage to verify that functions are actually used: `go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out`
- Review the output to ensure new or modified functions have test coverage and are not dead code.
