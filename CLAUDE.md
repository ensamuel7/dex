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
- Format files: `go run . format [path...]` (`--check` to report without rewriting)
- Lint a file: `go run . lint <file.dx>`. `build`, `run` and `dev` report lint
  findings by default (`--no-lint` to skip, `--lint=strict` to fail on them);
  `test` only with `--lint`
- Generate docs: `go run . docs`
- Start LSP: `go run . lsp`
- Start a dex.mod: `go run . mod init <module path>`; add a library:
  `go run . get <module>[@version]`; fetch everything: `go run . mod download`

## Project Structure

- `ast/` - AST node definitions and type system
- `lexer/` - Tokenizer
- `parser/` - Parser (tokens -> AST)
- `checker/` - Type checker and semantic analysis
- `resolve/` - User module resolution, method flattening, the shared AST walker
- `lint/` - Lint rules run after checking (`dex lint`, and every build unless `--no-lint`)
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

## Calling C

`external module` blocks declare C functions (`parser/parse_external.go`,
`checker/check_external.go`). A block becomes a `stdlib.Module` via
`stdlib.FromExternal` and is merged into the checker's import map and the
generator's `importedModules`, so every existing qualified-call path handles it
unchanged — the checker's signature match at `check_expr.go` and codegen's
`CName` emission at `gen_call.go` needed no changes at all.

It is deliberately **not** passed to `stdlib.Register`. That registry is a
package global populated at `init` and never reset, and the LSP is one process
compiling many files, so a module registered there would outlive the file that
declared it and could shadow a real one. Codegen therefore resolves module
functions through `g.lookupFunc`, not the global `stdlib.LookupFunc`.

Scalars and `string` cross (`externalTypeSupported`). A returned string has two
conventions and the declaration picks one: `string` copies and leaves the
pointer alone (`dex_string_copy_cstr`), `owned string` copies and then frees it
(`dex_string_from_cstr`), and `owned string freed by "f"` copies and then calls
the library's own deallocator — emitted inline as a statement expression, not
through a function pointer, because `OPENSSL_free` is a macro. The default matters — `dex_string_from_cstr` frees,
which is right for the stdlib, where every CName function returning a string
hands over malloc'd memory, and heap corruption for a user binding `getenv`.

`pkg "name"` runs `pkg-config --cflags --libs` through `stdlib.PkgConfig`,
cached per process because the LSP checks on every edit. `filterPkgFlags`
passes on only `-I`, `-L`, `-l`, `-D`, `-pthread` and `-framework`: the name is
the block's, the output is the machine's, and both reach the compiler. The
checker reports a failure at the block; `FromExternal` silently contributes no
flags for one, and copies `Links` before appending so the declaration's own
slice is never written.

Every C name a block supplies — `= "symbol"`, `closed by`, `freed by` — is
pasted into the generated C, so `validCSymbol` holds each to a C identifier,
for the same reason `validLinkFlag` restricts flags.

`owned` is a soft keyword in the return position, not an annotation: `#[owned]`
already means no-aliasing on a variable, and a marker deciding whether `free`
runs is semantics rather than advice. There is no `#[raw]` for parameters —
bytes cross as a pointer and a length, because a linked library cannot see the
`DexString` layout that the stdlib's `RawParams` relies on.

An opaque handle (`type X closed by "f"`) is a refcounted box: `DexHandle` in
`cruntime/handle.c` holds the C pointer, and the closing function is the box's
destroy hook, so one struct serves every handle type. `IsHeapType` is true for
a handle, which is what gives it retain/release, scope cleanup and the unwind
stack with no site knowing about handles — `_dex_cleanup_push` on a handle is
emitted by the existing path. `dex_handle_new` yields NULL for a NULL pointer,
so `X?` falls out of the existing heap-pointer optional representation with no
new codegen. The destroy shim must not free the box: destroy releases contents,
the runtime frees the object, the same contract `dex_string_destroy` follows.
`ast.ResetHandleTypes` is wired into both reset sites for the LSP.

An out-parameter is a `&T` external parameter, parsed in `parseExternalFunc`
because `parseType` allows `&` only on structs and scalars. A scalar crosses as
`&var`, uncast so a header still checks it. A handle needs `genHandleOutCall`:
C writes into a `void*` local, which is read back with `memcpy` — clang gives
distinct pointer types distinct aliasing classes by default (`-pointer-tbaa`),
so a store through `sqlite3**` inlined from a header could otherwise be assumed
not to change a `void*` — then boxed into the variable as an assignment would
store it. `isReassigned` counts an out-argument as an assignment, or a variable
written only by C would get no unwind slot and the cleanup stack would name
what it held before. `checkOutArg` requires a variable of exactly the referenced
type, refusing one narrowed from `H?`, since C may write NULL.

`IsTypeParamType` is bounded above by `TypeHandleBase`. It was open-ended, so
every handle counted as a type parameter and a function taking one failed with
an internal instantiation error.

Refcounting does not model dependency: a statement handle outliving its
connection handle is a use-after-free the language cannot catch. Documented
rather than implied away.

An external module naming no `include` gets C prototypes emitted from its Dex
signatures (`emitExternalPrototypes`), so a header-less library binds without a
hand-written one. Naming a header is the default and keeps the compiler's
cross-check; omitting one is the opt-out and an ABI mismatch nothing catches.
`-Wno-pointer-sign` is added only for a program that declares an external,
because a string crosses as `char*` and byte APIs are declared `unsigned char*`;
`-Werror=incompatible-pointer-types` stays fatal.

A local `include` in a user module is emitted as an absolute path to the
header beside that module (`ExternalModule.Dir`, set in resolve, read by
`FromExternal`), because only the main file's directory is on `-I`. A library's
C helpers must not be named `<module>_<fn>`: that is the C name of the module's
own Dex function `fn`.

Callbacks remain unsolved: `throw` is `longjmp`, so a C frame between a throw
and its catch has its cleanup skipped.

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

Fault injection rides on the checker. `DEX_FAIL_ALLOC=N` makes the Nth managed
allocation throw `injected allocation failure #N at file.dx:line` through the
same staged throw `throw` uses, so every unwind path can be driven
deliberately; `DEX_FAIL_ALLOC_SCOPE=try` counts only allocations made while a
try is live, `DEX_FAIL_ALLOC_REPORT=1` prints the total, and
`DEX_FAIL_ALLOC_TRACE=1` a C backtrace at the failing one.
`faultinject/faultinject_test.go` sweeps N over the fixtures in
`faultinject/testdata/` on every `go test`, and over the whole example suite
with `make faultinject`. It fails only on a memory error, a crash or a hang; a
leak is reported, not failed, because of the two bounded classes in the unwind
section below.

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

## Sanitizers and the other safety nets

`DEX_SANITIZE` takes a comma list: `address` (or `1`), `undefined`, `thread`,
`memory`, `leak`. `thread` and `memory` each exclude `address`. Every build
also takes `DEX_CFLAGS`, appended last, and `dex test` takes any number of
files or directories.

- `make sanitize` (ASan+UBSan) and `make tsan` run the suite on macOS, whose
  clang has no MSan, LSan or libFuzzer.
- `make sanitize-linux` runs ASan+LSan+UBSan, MSan, TSan and Valgrind in
  `sanitize/Dockerfile` against a read-only copy of the tree, so its `build/`
  never touches the host's. `MODES="asan valgrind"` picks a subset. Linux
  clang's UBSan checks calls through a mismatched function type, which Apple
  clang 17 does not; that is what found the interface vtable casts.
- MSan sees memory written by an uninstrumented library as uninitialised, so
  `run.sh` leaves out programs that link OpenSSL, sqlite, libpq and the like.
- `make strict-c` builds the suite with `-Wall -Wextra -Wconversion -Werror`,
  minus the categories the generated C produces on purpose (unused statics,
  missing initialisers in a struct literal, pointer sign at an external, an
  unwind slot for a field that is never reassigned) and `-Wignored-qualifiers`,
  which librdkafka's own header trips.
  `-Wshadow` is off: nested JSON literals reuse `_jo` in inner statement
  expressions.
- `matrix/matrix_test.go` crosses 13 value types with 12 ways of producing one
  and 46 ways of consuming it, builds each combination under the memory checker
  and runs it 50 times with `DEX_MEM_LEAKS=error`. Plain `go test` runs a fixed
  sample; `DEX_MATRIX=1` runs all 6624 (about 75 minutes, most of it macOS
  scanning each new binary), `DEX_MATRIX_FILTER=<substr>` a subset,
  `DEX_MATRIX_ASAN=1` under ASan, and `DEX_MATRIX_OUT=<dir>` keeps every
  failing program with its log. A combination the checker rejects counts as
  invalid, not failed. `knownGaps` lists a combination that fails for a
  reason not yet fixed, so it is reported rather than failing the run; it is
  empty. Pass `-count=1`: the test shells out to a freshly built compiler,
  which Go's test cache cannot see.
- `fuzz/` holds libFuzzer harnesses for the runtime's input parsers;
  `fuzz/run.sh` runs them under a local coverage-guided driver, `--docker` under
  real libFuzzer. `fuzz/runtime.h` includes the runtime in `gen_program.go`'s
  order and has to follow it.
- The front end has Go fuzzers: `FuzzTokenize`, `FuzzParse`, `FuzzCheck` and
  `FuzzFormat`, each in its package's `fuzz_test.go`, seeded from `examples/`.
  Their crashers in `testdata/fuzz/` run on every `go test`. Run one with
  `go test ./parser -run='^$' -fuzz=^FuzzParse$ -fuzztime=8m
  -fuzzminimizetime=3s`; the default minimise time stalls on inputs grown from
  the examples. The parser stops at 1000 levels of nesting and the lexer at
  100 levels of interpolation, each with a diagnostic, because both recursed
  without bound.

Hardening flags are probed per compiler (`hardeningFlags` in `main.go`, once
per process): `_FORTIFY_SOURCE=3`, stack clash protection, branch protection,
and zeroed automatic variables. A sanitizer build drops fortify, which ASan
defines to 0, and an MSan build drops zero-init, which would hide exactly what
MSan looks for.

### Undefined behaviour the generated C used to have

- Integer `/` and `%`, compound forms included, go through `dex_div_int` and
  friends in `safety.c`: zero panics, and `MIN / -1` is `MIN` with remainder
  `0`, Go's answer. `-fwrapv` covers `+ - *` but not division, which faults on
  x86 and was folded to garbage at `-O3`.
- `num.toInt`/`toLong` of a double saturates and maps NaN to 0
  (`dex_double_to_int`). Narrowing a long wraps, as Go does.
- An index is evaluated once and checked at the access:
  `a->data[dex_checked_index(i, a->len)]`. The check took the index as a
  separate expression, so `a[next()]` called `next` twice and could check one
  slot and touch another. An index assignment evaluates the index, then the
  value, then checks, so a value that shrinks the array cannot make the check
  stale.
- An interface vtable slot is a typed thunk, `dex_vt_<Iface>_<Struct>_<m>`,
  not the method cast to a `void*`-receiver pointer type.
- `str.toInt` and every runtime parse of outside data go through
  `dex_parse_int`/`dex_parse_long`/`dex_parse_double` in `core.c`, which
  saturate; `atoi` on an out-of-range string is undefined.

`core.c` is emitted into every program, ahead of every other runtime file. It
holds the stack-overflow handler, the parse helpers, and `dex_xmalloc` and its
siblings, which every stdlib allocation goes through: out of memory is fatal
with the same message `dex_obj_alloc` gives. The stack guard records each
thread's stack top and size — `main` from `RLIMIT_STACK`, a pool worker from
the default attribute — and its alternate stack is static or in the worker's
own frame, because a malloc'd one reachable only from the kernel reads as a
leak to `leaks`. It is off under ASan, TSan and MSan, which install their own
handlers.

An external function whose handle return is declared without `?` goes through
`dex_handle_require`, so a NULL stops the program at the call that produced it
rather than at the next C function to receive it.

## Struct layout in the editor

`ast/layout.go` computes a struct's C layout from its Dex field types —
`TypeLayout` mirrors `cType`, so a new C representation for a type has to be
reflected there too. The LSP hover shows the size, alignment and padding, and
the `struct-padding` lint rule (a hint in the editor) names a smaller order
when sorting by alignment gives one. The compiler keeps declared order: the
hover and the lint describe what the C really is. `StructDef.Pos` and
`StructDef.Annotations` exist for this rule, and a generic struct's
instantiations share their template's position, so the rule reports a
position once.

## Profiling memory

`dex profile <file>.dx` (or `DEX_PROFILE=1` on any build) compiles in
`codegen/cruntime/profile.c` in place of the checker. It shares the checker's
`DEX_AT` statement markers and nothing else: no quarantine and no poisoning,
and the object pool stays on, so the numbers are the program's own. The two
are refused together in `configureGen`.

- **Sites are keyed on the literal's address**, in an open-addressed table, so
  an allocation costs a hash rather than the checker's `strcmp` scan. The
  header gains the site slot, the object's size, and its buffer size.
- **Container buffers count.** Arrays, maps, string builders, channels and
  `json.Value` call `DEX_PF_BUF_SET(obj, bytes)` after every (re)allocation of
  their backing store. The delta is held by the site that allocated the
  container and charged as allocation to the statement that grew it. A new
  container type must do the same or its memory is invisible.
- **`DEX_PF_FRAME()` at every function entry** restores the caller's marker on
  return (`__attribute__((cleanup))`), so the rest of `let s = f() + "x"` is
  charged to the caller rather than to `f`'s last line. An unwind skips it;
  the next statement's marker corrects the site.
- **At peak** is copied whenever the peak grows by 1%, not on every new high,
  so it is approximate by that much.
- The writer runs from `atexit` and from SIGUSR1/SIGINT/SIGTERM (only where the
  program left the default disposition), so it uses `write(2)` from a static
  buffer and takes the lock only with trylock in a handler.
- `profview/` renders the JSON plus the source files it names into one HTML
  page (`template.html`, embedded); `profilecli.go` is the command.

Allocations are attributed to the line, not the call stack: everything inside a
shared helper is charged to the helper's line.

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
safely read. `registerStructFieldsForUnwind` does that, descending into a
nested struct and registering an optional, a `T?` box or an interface's `_obj`
under a dotted slot key; `emitUnwindPathUpdate` keeps each slot current after a
field write. A call that may mutate a struct local's fields — a mutating
method, a `&T` argument, `reflect.setString`, `db.fill` — goes through
`gen_unwind_refresh.go`, which clears those slots before the call and refreshes
them after. If that callee itself throws, the field it was writing leaks, one
object per throw: the caller cannot know the new value and the dead frame
cannot be read. That and allocations failing inside runtime constructors are
what the fault-injection sweep still finds leaking.

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

## Names the C library already uses

A top-level function or global named like a C library symbol (`read`, `time`)
would collide in the generated C. `resolve.RenameCCollisions` runs after
checking, so diagnostics keep the written name, and renames just those to
`dx_<name>` through the shadow-aware renamer the module prefixer uses.
`resolve/cnames.go` is the set: every function, variable and typedef the system
headers the runtime includes declare (from clang's AST dump), their lowercase
macros, and a few Linux-only names.

## dex.mod

`resolve/mod.go` reads a `dex.mod` (`module`, `require`, `replace`, with go.mod's
block form) and `importFile` turns an import into a file: a path whose first
segment holds a dot is a module path (Go's rule), anything else is relative to
the importing file as it always was. Every site that used to join a path with
`.dx` goes through it — pre-registration, `moduleIdentities`, `fileAliases`,
resolution, and the three LSP lookups via `resolve.ImportFile`.

Only the main program's `dex.mod` is read, found by walking up from the main
file; a library's imports, its own module path included, resolve through the
program's `require` and `replace`. A replace names a directory only, and must be
required, so deleting it leaves a complete file. A module path imported whole
reads `<root>/<last segment>.dx`.

A required module that is not replaced is read from the module cache
(`resolve/get.go`): `ModuleDir` is `<cache>/<path>@<version>`, the cache is
`~/.dex/pkg/mod` or `DEX_MODCACHE`. A build never fetches; `CheckModuleImports`
runs before pre-registration so a missing one is reported at its import rather
than as an unknown type. `dex get` finds the repository (three segments on a
known host, else the shortest prefix `git ls-remote` accepts), lists tags, and
walks the subdirectory upward until a folder-prefixed tag matches, so a file
path inside a module still finds it. `fetch` shallow-clones the tag, copies the
folder into the cache read-only, and refuses a `dex.mod` whose module line
disagrees with where it was found. `dex.sum` is an `h1:` hash over every file's
sha256 and path, checked on every `get` and `mod download`. A library's own
requirements are added to the program's `dex.mod` as `// indirect`, skipped
when the program already requires an equal or newer version. `repoURL` is a
package variable so the tests fetch from local `file://` repositories.

## A module is a file, and its names are its own

A module's **identity** — the prefix on its functions, globals and types in the
generated C — is assigned per file by `resolve.moduleIdentities`: the name the
file is first imported by, or `util_2`, `util_3` when a different file already
holds it. It was the base name, shared by every file with that base name, so two
libraries each with a `util.dx` merged and a function both declared was an error
nobody could fix. A program with no such clash is named exactly as before.

The walk is depth first in import order and runs separately in every caller —
pre-registration, resolution, the LSP — so each derives the same names.

What a file calls an import is local to it. `fileAliases` maps a name written in
a file to the identity where they differ, and two imports under one name in one
file are an error. The rename happens in two places, because references are
resolved in two places:

- **Calls and field accesses** are renamed after parsing by `renameModuleRefs`,
  shadow-aware through `withoutShadowed`, for the main program and each module.
- **Types, struct literals and enum variants** are resolved by the parser, so it
  is told the aliases first (`Parser.SetModuleAliases`, read by
  `moduleIdentity` at the three qualified-name sites).

A generic is re-parsed by the checker after the main program's calls were
renamed, so `resolve.Instantiator` wraps the parser and renames each
instantiation the same way. `program.ModuleAliases` carries the main program's
map to it.

An **external module** is visible only in the file that declares it. In a user
module the parser registers it as `<module>__<name>` (`ExternalModule.Local`
keeps the written name for diagnostics) and its handle types under that key,
and resolve renames the module's calls to match. The main program's keep their
names, so `refuseForeignExternals` stops a module reaching one.

Globals are defined in `program.GlobalLets` order, so `ResolveUserModules` puts
the main program's after every module's: they were first, and `let TOP: int =
counter.LIMIT + 1` read a constant not yet defined. Modules merge dependencies
first, so that order puts every global after what it can name.

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

An **interface** a module declares belongs to it too, and that took more than
the missing merge in `resolve.go`. `parseInterfaceDef` registers the qualified
name when `Parser.module` is set and `InterfaceDef.Module` carries it for
diagnostics; the pre-registration scan picks up `interface` beside `struct` and
`enum`; and `ModuleType.IsEnum` became `Kind`, because a bool cannot say which
of three kinds a name is. Both type-position paths resolve one — the bare name
through the candidate list a struct uses, and `alpha.Handler` through the
qualified branch — and missing the second is why the bare form worked while the
qualified form still reported an unknown type. `DisplayStructName` renders an
interface as well, so a diagnostic reads `alpha.Handler`.

Without this a library could not state a contract: the declaration parsed and
was dropped, and the importing file reported an unknown type for a name
visible in the source.

## Where a heap value comes from, and whether it dies

Two things sit underneath the ownership rules, and both are about allocation
rather than ownership. Neither changes when an object dies — only where its
memory came from — which is why neither needs escape analysis to be safe.

**A string literal is immortal.** It is the same bytes at every evaluation, so
codegen emits it once as a `static const` object (`DEX_LIT_DEF` in `strings.c`)
and references it with `DEX_LIT`, instead of calling `dex_string_from_lit` and
paying a `malloc`, a `strlen` and a `memcpy` per evaluation. Its count is a
sentinel, `DEX_RC_IMMORTAL`, and `dex_retain`, `dex_release` and
`dex_owned_free` all return early on it. Four things hold it together:

- **Ownership is deliberately untouched.** `isNewAlloc` still answers true for a
  `StringLit`, so every retain and release that was emitted before is still
  emitted — they are simply no-ops. That is what made this safe to add without
  revisiting one ownership decision: a literal that is over-released frees
  nothing and one that is under-released leaks nothing.
- **The count must not be decremented, not merely spared the free.** Release
  outnumbers retain on a literal by one per evaluation, so a counter that
  drifted would reach zero after ~2^31 of them and free a static.
- **The immortal test precedes `dex_mc_check`.** A static is absent from the
  memory checker's registry by construction, and the checker reports a release
  of something it does not know as an error.
- **`const` is load-bearing.** It puts the literal in read-only memory, so the
  guard failing open would fault rather than quietly corrupt a shared string —
  and it tells the optimiser the bytes never change, which is what lets a
  concat of two literals still fold to its result the way it did when both
  sides were allocating calls on known contents. Dropping it costs about 25% on
  a loop that concatenates literals.

A literal holding a NUL byte is not pooled: `strlen` truncates it on the
allocating path, `sizeof` would not, and that is a different string.
`poolLiteral` keeps that case allocating. `\0` is not one of the four escapes
the lexer accepts, but a raw NUL in a source file reaches it.

**A small object is recycled, not returned to malloc.** `dex_obj_alloc` takes
freed blocks from a bounded free list (`DexPoolLane` in `refcount.c`) before
asking `malloc`. On an allocation-heavy loop `malloc` and `free` dominated by
an order of magnitude over the reference counting around them. An object is
still individually owned, released, and destroyed exactly when its count
reaches zero — only the source of its memory differs, which is the whole
difference between this and a region. Three constraints shape it:

- **Only a `dex_obj_alloc` block may be freed into it.** `_dex_obj_free` reads
  the size class back out of the header, so anything else is filed under a size
  it does not have and later handed out for an object that does not fit. The
  only two callers are `dex_release` and `dex_owned_free`; a managed object that
  was never malloc'd is immortal and returns before reaching either.
- **The lanes are plain globals, not thread-locals.** A pooled block is freed to
  the program but still malloc'd to the system, so it has to stay *reachable* or
  a leak checker reports it. `leaks` roots at thread stacks, registers and the
  global data segment — not at thread-local storage — so a thread-local pool
  made every block sitting in it at exit a reported leak, which is how
  `concurrency_test` caught this. One lane per thread in a global array keeps
  them reachable and needs no drain at thread exit at all.
- **It is off under both memory debuggers.** Recycling hands a freed block
  straight back out, which is exactly the state the checker's quarantine and
  ASan's redzones exist to detect; leaving it on would make a use-after-free
  read as a successful allocation. `DEX_USE_POOL` is 0 under `DEX_MEM_CHECK` and
  under ASan, so the two tools still see every free.

The size class rides in the four bytes of padding `DexObjHeader` already had
between `rc` and `destroy`, so the header is still 16 bytes on LP64.

`#[region]` is the third thing of this kind and the one that is *not* safe. It
does change when memory dies, so it needs to know that no value outlives the
region — and its checks cover only three escape routes (`return` of a bare
identifier, `spawn` capture, closure capture), while storing a region value
into a returned struct, a caller's array, or a global is unguarded. `arena.c`
is also stale against `DexString` and fails C compilation, and a computed RHS
allocates while the release is suppressed, which leaks. It is unusable today;
completing it is an escape analysis, not a repair.

## Generics are monomorphised by re-parsing, not by substitution

A generic declaration is captured as the **tokens it was written as** and
skipped rather than parsed (`captureGenericDecl`). Instantiating it re-parses
those tokens with its type parameters bound to concrete types and its own name
mangled — `Box_int`, `max_int` — and what comes out is an ordinary function or
struct. Codegen therefore knows nothing about generics: `Dex_Box_int` falls out
of the existing struct path with no codegen change at all.

Re-parsing is what makes this tractable here. The parser is where a type name
becomes an `ast.Type`, so binding `T` to `int` is one lookup in
`Parser.subst.types`. Substituting over a parsed body instead would mean
deep-copying and type-remapping all forty-nine expression and statement kinds,
and every kind missed is a node that silently keeps the type parameter.

The two halves instantiate in different places, for one reason:

- **A struct instantiates in the parser**, because its type arguments are
  written — `Box<int>` names them. `instantiateStruct` re-parses the template
  and appends the definition to `Parser.instantiatedStructs`, which `Parse`
  folds into `program.Structs`. The language's existing declare-before-use rule
  is what makes this legal: `Box<int>` can only be written below `struct
  Box<T>`, so the template is always captured by the time a use site needs it.
- **A function instantiates in the checker**, because its type arguments are
  inferred and only the checker knows that `1` is an int. They meet at
  `checker.Instantiator`, which the parser implements and `main.go` wires up
  via `SetGenerics`. The checker infers, asks for the instantiation, registers
  the signature, appends to `program.Functions`, and rewrites the call's `Name`
  to the mangled one.

Four things hold it together:

- **`resolveTypeName` is the single rename hook.** Inside a template being
  re-parsed, the template's own name means *this* instantiation, so `Box {
  value: v }` written in `struct Box<T>` resolves to `Box_int`. Putting the
  rename there rather than at each name-reading site is what makes struct
  literals in both spellings, constructor calls, and annotations all follow.
- **The body-checking pass is indexed, not ranged.** Checking a body can
  instantiate a generic, which appends to `program.Functions`; a `range` would
  have captured the slice first and never reached it. The indexed loop reaches
  an appended function and checks it like any other — including one instantiated
  by an instantiation.
- **An instantiation is marked before its body is checked.** A recursive generic
  calls itself with the same type arguments, and marking afterwards would
  instantiate forever. `TestGenericRecursion` pins it.
- **A type parameter must never reach codegen.** It has no C representation, so
  one that survived is a hole in instantiation rather than a program error.
  `Checker.rejectTypeParams` says so plainly instead of letting `cType` emit
  `void`. `ArrayTypeOf` does register an entry for `T[]`, but only so inference
  can read the element back out; the instantiation re-parses with `T` bound and
  produces a real array type.

Deliberately not supported, each with its own reason rather than a shared
"unimplemented": a user module cannot declare a generic (the template lives in
the parser that read it, and a module is read after the main program was
parsed — `resolveModuleFile` rejects it by name); there are no type parameter
bounds; and a generic struct cannot appear in a generic function's signature,
which would need structural unification through an instantiation.

## An interface value owns a copy, and that is why it can be stored

An interface value is `{ void* _data; void* _obj; struct { ... } _vtable; }`:
a refcounted box holding a **copy** of the struct, a pointer into that box, and
the concrete type's method table filled at the coercion site. Dispatch is one
indirect call with no lookup and no type metadata, exactly as when `_data`
borrowed the caller's struct.

Owning rather than borrowing is the whole feature. A borrowed interface could
not be returned, stored in a field, or held in a container — those are the
positions that outlive the frame it pointed into, and all four were compile
errors. A box survives all of them, so the restrictions are gone and
`checkInterfacePositions`, `checkInterfaceBinding` and the parser's array
refusal went with them.

Four things hold it together:

- **The constructor retains, the caller borrows.** `dex_iface_<Iface>_from_<Struct>`
  memcpys the struct in and then retains each heap field of the *copy*, because
  a shallow copy shares the original's pointers and the box's destructor will
  release them. Callers pass through `genBorrowed`, the same convention
  `dex_array_string_push` follows: a struct built in the argument position is
  hoisted to a statement-scoped temporary and released at the end of the
  statement, and the retain here is what keeps the box's copy alive past it.
- **An argument is the one position that does not own.** A parameter is
  borrowed, so the box minted for a coercion at a call site has no owner in the
  callee. `genArgMaybeInterface` hoists it into a statement temp; every other
  position — return, let, field, array element — takes the bare mint and owns
  it. Forwarding a value that is *already* an interface mints nothing and
  retains instead.
- **The release goes through `_obj`, never through the value.** The value is
  three words passed by value, so `ast.IsHeapType` is deliberately false for an
  interface and `ast.NeedsRelease` deliberately true. Every site that emits a
  release or a retain special-cases it: `emitReleaseVar`,
  `emitRetainBorrowedStructFields`, `emitRetainStructLitFields`,
  `emitUnwindSlotUpdate`, and the cleanup-stack push in `registerScopeVar`,
  which registers `name._obj` because the cleanup stack holds pointers and
  cannot read a dead frame's multi-word local.
- **Typedef order is load-bearing.** Every struct is forward-declared
  (`typedef struct Dex_X Dex_X;`) first, then the interface structs, then the
  struct definitions. A vtable's function pointers name the structs its methods
  take and return, which a forward declaration is enough for; a struct holding
  an interface by value needs the interface complete, which is why interfaces
  still precede the definitions. The box constructors reference the flattened
  method functions by name, so they are emitted *after* those forward
  declarations, not beside the typedefs.

The cost is one pooled allocation per coercion and one real semantic change:
mutation through an interface reaches the box, not the original. `Tally` in
`examples/interface_test.dx` pins that, and Go makes the same trade for the
same reason.

A by-value container reuses the struct-array machinery — `ArrayTypeOf` routes
an interface element to `StructArrayTypeOf` — with `dex_cleanup_iface_<Iface>`
as the element destructor, because the array owns the element memory and the
element owns its box. Two containers still refuse one and say why: a map holds
only the six scalar value types (a struct value is refused identically), and an
optional has no spelling for absent because an interface value is not a pointer.

## An optional struct is a refcounted box

`T?` for a struct is a `Dex_T*` into a box laid out `{ DexObjHeader; Dex_T }`,
allocated by the per-struct `dex_optbox_<T>` and released through
`dex_optbox_release`, which steps back over the header. The pointer addresses
the value rather than the header so field access and method calls through it are
unchanged; the cost is that `dex_retain`/`dex_release` must never be called on it
directly, which is why `IsStructOptionalType` sites call the `dex_optbox_*`
helpers instead of joining the `IsHeapOptionalType` ones.

It was a plain `malloc` with one owner, and every copy copied the pointer:
`let r: T? = h.field`, a struct holding one copied by value, `return self.c`
each made a second owner, and the second release was a use-after-free. A `T?`
of a struct with no heap fields was never freed at all, because `NeedsRelease`
asked only about the fields. `genOptionalStructOwned` is now the one place a
`T?` is produced owned: it boxes a `T` (retaining what a borrowed source holds)
and retains a borrowed `T?`. `examples/optionalStruct_test.dx` covers each path.

The box goes on the cleanup stack as its header pointer, or NULL when empty,
with a slot when the variable is reassigned (`registerScopeVar`/`unwindHeld`).

## Maps reject what they cannot represent

The map runtime is generated per key/value suffix pair, and `mapKeySuffix` /
`mapValSuffix` fall through to `"int"` for anything without one. That silently
truncated a `long` key and turned a struct value into a C error about
`Dex_Person` and `int` — a diagnostic about the generated code rather than
about the program. `mapKeyTypeSupported` and `mapValueTypeSupported` reject the
type where it is built instead, naming it through `describeType`. The supported
sets are exactly what `LANGUAGE.md` documents: string or int keys, and int,
bool, string, long, double or char values.

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
  `genShortCircuit` therefore generates each operand of either operator as a
  condition of its own, through `genCondition`: whatever an operand hoists is
  declared and released inside it, so it exists only if the operand runs.
  Simply switching hoisting off is not enough — a value minted for a borrowing
  consumer, `f(name.toLower())`, then has no temporary and leaks per
  evaluation. Both `genExpr` and `genExprNoParen`, the path every `if` and
  `while` condition takes, route through it; the second once did not, and
  `if (false && s().len() == 1)` ran `s()`.
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
- **An optional heap field is a heap field.** `string?`, `string[]?` and a
  handle's `Db?` are the same pointer with NULL as absent, so a struct literal
  storing a borrowed one retains it and a `let` reading one out of a field
  retains it — `ast.IsHeapOptionalType` is what both ask, alongside
  `IsHeapType`. Neither did: `Box { name: n }` shared `n`'s reference, and
  `let n: string? = b.name` released the struct's string at scope exit. Both
  were silent until the memory checker ran, and both blocked a library holding
  a C connection in a struct. `examples/optionalFields_test.dx` covers them.
- **A discarded result is still owned.** A call used as a statement whose
  result needs releasing — a struct with heap fields, an interface, an optional
  — is bound to `_dex_discard` and released, not only a bare heap type.
- **A `foreach` over a minted array evaluates it once.** A literal or a call is
  bound to `_foreach_arr_N` in a scope of its own and released after the loop;
  inlined, it was rebuilt on every iteration and leaked each time.
- **An optional argument is borrowed like any other.** `genOptionalArg` goes
  through `genBorrowed` for a heap inner type, and `genBorrowed` hoists a minted
  `T?` as well as a `T`: `lenOf(maybe(s))` and a C handle passed straight into
  a Dex function leaked one reference per call. A compound assignment gets a
  statement prelude too, or `total += f(g())` had nowhere to hoist into.
- **An optional return retains what it borrows.** `return u.name` as `string?`,
  and `return U { name: name }` as `U?` (boxed), take the same references the
  non-optional paths do.
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
- **A map read of a string mints.** `dex_map_*_get` returns its value
  retained, so `m["k"]` is owned (`isMapHeapRead`), and `if (m["k"] == "v")`
  releases it like any other minted operand.
- **A local written in a try body escapes before the `setjmp`.** After a
  `longjmp` a local the body changed may be read from a register holding its
  old value, which was a double free when the old value had been released.
  `emitTryWrittenEscapes` takes each such local's address ahead of the
  `setjmp`, which keeps it in memory.
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
