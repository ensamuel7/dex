# DexLang

A statically-typed language that compiles to C. Sits somewhere between TypeScript and Go in terms of how it feels to write, with the performance of C under the hood.

## Background

This started before and around the beginning of my undergrad. I was getting into C++ and later deep diving into Java during my master's, and I kept wanting something that had the developer experience of TypeScript and Go but compiled down to native code through C. So I just started building it.

The real motivation was understanding how everything fits together — lexers, parsers, type checkers, code generation — the full pipeline from source to binary. If it turns into something I can use for personal projects or internal tooling, even better.

Memory is managed by automatic reference counting, not a garbage collector. Strings, arrays, maps, channels and boxed values are refcounted; the compiler decides at every expression whether a value is owned or borrowed, and emits the retain and release. Values a frame held are released even when an exception unwinds past it. There is no runtime pause and nothing to tune, `DEX_MEM_CHECK=1` builds with a checker that attributes a leak or a double free back to the `.dx` statement that allocated the object, and `dex profile` shows which lines use the memory: live bytes over time, and what each line holds at the peak.

This is currently an independent project developed and maintained primarily by me. The long-term goal is to release it publicly and encourage community contributions, but in the near term development will remain focused on a small team, with a few colleagues expected to contribute as the project matures.

The boilerplate and most of the verbose code was written with the help of Claude. The language design, architecture decisions, fine tuning, and implementation verification are all mine.

## Install

### One-line install (macOS / Linux)

```bash
curl -fsSL https://raw.githubusercontent.com/ensamuel7/dex/main/install.sh | sh
```

### Prerequisites

You need a C compiler since `dex` compiles to C:

- **macOS:** `xcode-select --install`
- **Ubuntu/Debian:** `sudo apt install gcc`
- **Fedora:** `sudo dnf install gcc`

### From source

```bash
go install github.com/ensamuel7/dex@latest
```

Or build manually:

```bash
go build -o dex && sudo mv dex /usr/local/bin/
```

## Usage

```bash
# run a program
dex run examples/hello.dx

# compile to a binary
dex build examples/hello.dx
./build/hello

# run tests
dex test

# lay every .dx file out the one canonical way
dex format

# report what compiles but is probably not meant
dex lint app.dx
```

## Docker

The image `ensamuel7/dexlang` has `dex` and everything it compiles with: gcc,
git, and the headers for SQLite, PostgreSQL, OpenSSL, libcurl and hiredis. You
need nothing else installed, which makes it a good fit for CI, or for trying the
language without installing it.

```bash
docker pull ensamuel7/dexlang:0.9.23     # or :latest
```

The image's entrypoint is `dex`, and its working directory is `/workspace`, so
mount your project there and pass a command:

```bash
docker run --rm -v "$(pwd):/workspace" ensamuel7/dexlang run hello.dx
docker run --rm -v "$(pwd):/workspace" ensamuel7/dexlang test
docker run --rm -v "$(pwd):/workspace" ensamuel7/dexlang build hello.dx
```

`build` writes a **Linux** binary to `build/`, so it runs in a container or on
a Linux server, not on a Mac or Windows host. On a Linux host, add
`--user "$(id -u):$(id -g)"` so `build/` belongs to you rather than to root.

A server needs its port published:

```bash
docker run --rm -p 8080:8080 -v "$(pwd):/workspace" ensamuel7/dexlang run server.dx
```

### Shipping a program in its own image

Build in the dex image and copy only the binary into a small one:

```dockerfile
FROM ensamuel7/dexlang:0.9.23 AS build
WORKDIR /src
COPY . .
RUN dex build main.dx

FROM debian:trixie-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
        libsqlite3-0 libpq5 libssl3t64 libcurl4t64 \
    && rm -rf /var/lib/apt/lists/*
COPY --from=build /src/build/main /usr/local/bin/app
EXPOSE 8080
CMD ["app"]
```

A project with a `dex.mod` needs its libraries fetched first: add
`RUN dex mod download` before the build. Without a `dex.mod` that command
fails, so leave it out.

The runtime image must be the same Debian release the dex image is built on
(13, "trixie"), because the binary links against that release's libraries.

The binary links only the libraries for the modules it imports. A program
using nothing beyond `fmt` needs libc alone, so the `apt-get` line can go.
To see exactly what one needs:

```bash
docker run --rm -v "$(pwd):/workspace" --entrypoint ldd ensamuel7/dexlang build/main
```

Pin a version tag in a Dockerfile rather than `latest`, so a new release
cannot change a build that used to work.

## What It Looks Like

```dex
import "fmt"
import "math"

struct Point {
    x: double
    y: double
}

fn distance(a: Point, b: Point): double {
    let dx: double = (b.x - a.x) * (b.x - a.x)
    let dy: double = (b.y - a.y) * (b.y - a.y)
    return math.sqrt(dx + dy)
}

fn main(): void {
    let origin = Point { x: 0.0, y: 0.0 }
    let target = Point { x: 3.0, y: 4.0 }
    fmt.println(distance(origin, target))
}
```

Types are explicit where it matters, inferred where it's obvious:

```dex
let name = "Dex"       // string
let count = 42          // int
let temps = [1.0, 2.5]  // double[]
const pi = 3.14159
```

A number never widens on its own. A literal adapts to the type it is assigned
to, but a variable or an expression does not — `int` into `double` is a type
error, and `num.toDouble` is how you ask for it:

```dex
let a: long = 5                    // the literal adapts
let d: double = 2.5
let ok: double = d * 2             // so does this one

let n: int = 3
let bad: double = n * d            // error: cannot apply '*' to int and double
let good: double = num.toDouble(n) * d
```

Arithmetic is held to the same rule — both operands must already agree, and
`total += n` is refused exactly as `total = total + n` is.

This is Rust's rule, not Java's. Implicit widening reads as harmless and isn't:
`long` into `double` silently loses precision past 2^53, so the conversion that
looks safest is the one that quietly corrupts data. Asking for it costs one call
and means the narrowing is always visible at the point it happens.

Methods read fields directly and write them through `self`. Returning `self`
builds a fluent chain:

```dex
struct Query {
    from: string
    max: int

    fn table(name: string): &Query {
        self.from = name
        return self
    }

    fn limit(n: int): &Query {
        self.max = n
        return self
    }
}

let q: Query = Query { from: "", max: 0 }
q.table("users").limit(25)
```

A call is not the end of an expression, so results keep chaining:

```dex
let n: int = sb.toString().len()
let third: string = csv.split(",")[2]
let size: int = csv.split(",")[2].len()
```

Fields can carry tags — metadata the compiler stores and never interprets, so a
library decides what its own keys mean:

```dex
struct Book {
    id: long
    authorName: string `db:"COALESCE(a.name, '')"`
    priceMinor: long `db:"b.price_minor" json:"price"`
}
```

Structs can also be read by field position at runtime, through compiler-emitted
metadata that costs nothing when unused:

```dex
import "reflect"

let i: int = 0
while (i < reflect.fieldCount(q)) {
    fmt.println(reflect.fieldName(q, i) + " = " + reflect.toString(q, i))
    i = i + 1
}
```

## Interfaces

Structural typing — a struct satisfies an interface by having its methods. There
is no `implements`. Dispatch is one indirect call through a vtable the compiler
fills in at the coercion site, with no lookup and no type metadata:

```dex
interface Shape {
    fn area(): double
}

struct Circle {
    r: double

    fn area(): double {
        return 3.14159 * r * r
    }
}

struct Square {
    side: double

    fn area(): double {
        return side * side
    }
}

let shapes: Shape[] = [Circle { r: 1.0 }, Square { side: 2.0 }]
```

An interface value owns a copy of the struct in a refcounted box, so it can be
returned, stored in a field, and held in an array. Mutation through an interface
reaches the copy, not the original — the same trade Go makes.

## Generics

Type parameters on functions and structs, monomorphised at compile time. Each
instantiation is an ordinary function or struct by the time codegen sees it, so
there is no boxing and no runtime dispatch:

```dex
struct Box<T> {
    value: T
}

fn max<T>(a: T, b: T): T {
    if (a > b) { return a }
    return b
}

let b: Box<int> = Box<int> { value: 42 }
let m: int = max(3, 7)
```

A function's type arguments are inferred from its arguments; a struct's are
written. Two mentions of `Box<int>` are the same type, not two.

## Enums and Tagged Unions

A plain enum is an integer constant. A variant that carries fields makes the
enum a tagged union — a closed set of shapes, each with its own data, which a
variant may name recursively:

```dex
enum Expr {
    Lit(long)
    Add(Expr, Expr)
    Mul(Expr, Expr)
}

fn eval(e: Expr): long {
    switch (e) {
        case Expr.Lit(v):    { return v }
        case Expr.Add(a, b): { return eval(a) + eval(b) }
        case Expr.Mul(a, b): { return eval(a) * eval(b) }
    }
}
```

The match must be exhaustive. A `switch` that omits a variant is a compile error
naming what it missed, so adding a variant later breaks every incomplete match
instead of silently falling through one.

## Maps and Optionals

```dex
let scores: map[string, int] = {}
scores.set("alice", 100)

if (scores.has("alice")) {
    fmt.println(scores.get("alice"))
}
```

Appending `?` to a type makes it optional. Inside an `if` guarded by a null
check, the variable narrows to the inner type:

```dex
let name: string? = lookup(id)
if (name != null) {
    fmt.println("found " + name)   // narrowed to string here
}
```

## Closures and defer

A function value is a closure — a code pointer plus the environment it captured:

```dex
fn makeAdder(n: int): fn(int): int {
    return fn(x: int): int { return x + n }
}

let add5: fn(int): int = makeAdder(5)
fmt.println(add5(10))   // 15
```

`defer` registers a call to run when the function is left — on a `return`, on
falling off the end, and when an exception unwinds past the frame:

```dex
fn increment(c: &Counter): void throws {
    c.mu.lock()
    defer c.mu.unlock()   // runs however increment is left
    risky()               // including when this throws
    c.value = c.value + 1
}
```

The call is registered when the `defer` statement runs, not where it is written
in the file — one in a branch nothing took registers nothing, and one in a loop
registers per iteration. Its arguments are evaluated at the `defer`.

## Sharing Code Between Projects

A project that uses libraries from other projects has a `dex.mod` at its root,
naming the project by its URL and listing what it uses:

```bash
dex mod init github.com/acme/web
dex get github.com/acme/greet          # the latest release, or @v0.1.0
```

```
module github.com/acme/web

require github.com/acme/greet v0.1.0
```

```dex
import "github.com/acme/greet"              // greet.dx at the library's root
import "github.com/acme/web/service/users"  // this project's own file, from anywhere in it
import "service/users"                      // a plain path is still relative, as always
```

`require` names a library and the version used, and a version is a git tag.
`dex get` downloads it into a shared cache (`~/.dex/pkg/mod`) and records its
checksum in `dex.sum`; commit both files. `dex mod download` fetches everything
`dex.mod` requires, which is what a Dockerfile or CI runs before building.

`replace github.com/acme/greet => ../greet` reads a library from a folder on
your machine instead of downloading it, which is how a monorepo, or a library
you are changing alongside the app that uses it, works. See the
[Language Reference](LANGUAGE.md#modules-and-dexmod) and
[design/package-management.md](design/package-management.md).

First-party libraries — SQLite and a Redis client over C libraries — live in
[dex-modules](https://github.com/ensamuel7/dex-modules):

```bash
dex get github.com/ensamuel7/dex-modules/redis
```

## Calling C

An `external module` block declares C functions, the headers they need, and the
libraries to link against. The block is the declaration, so there is no import,
and a call compiles to a direct call to the C symbol with no wrapper:

```dex
import "fmt"

external module zlib {
    include "<zlib.h>"
    link "-lz"

    fn compressBound(sourceLen: long): long
    fn compileFlags(): long = "zlibCompileFlags"
}

fn main(): void {
    fmt.println(zlib.compressBound(1024))   // 1037
}
```

`= "symbol"` names a C function whose own name reads badly in Dex — `fn absInt(n: int): int = "abs"`.

`int`, `long`, `double`, `bool`, `char` and `string` cross the boundary, plus
`void` as a return type. Anything else is refused where it is declared, naming
the type, rather than left to produce a C error about the generated code.

A returned string is the one place a declaration has to say something, because
C has two conventions and guessing wrong corrupts the heap rather than leaking.
By default the bytes are copied and the pointer left alone, which is what
`getenv` and `strerror` need. `owned` on the return type says the callee
`malloc`'d it, so Dex frees it after copying — what `strdup` needs:

```dex
fn errorText(n: int): string = "strerror"        // copied
fn dup(s: string): owned string = "strdup"       // taken over and freed
```

A library that allocates with its own allocator names the function that frees
it, because plain `free` on its memory is undefined behaviour. It may be a
macro:

```dex
fn hex(buf: string, len: long): owned string freed by "OPENSSL_free" = "OPENSSL_buf2hexstr"
```

Bytes that may contain a NUL cross as a pointer and a length, which is how the
C functions taking them are already written — `.len()` is the byte length, so
nothing truncates.

Handle-based libraries declare the function that closes the handle:

```dex
external module cio {
    include "<stdio.h>"

    type File closed by "fclose"

    fn open(path: string, mode: string): File? = "fopen"
    fn readByte(f: File): int = "fgetc"
}
```

A handle is a pointer the program passes around and never dereferences. It is
refcounted rather than move-only, so the close runs when the last reference
goes — at scope exit, on overwrite, and when an exception unwinds past the
frame holding it. `fopen` returns NULL on failure, so the return is optional
and a failed open reads as `null`.

A C function that returns a result through a pointer argument takes it as an
`&` parameter, written by C into the variable you pass; `null` skips it:

```dex
fn open(path: string, db: &Db?): int = "sqlite3_open"

let db: Db? = null
let rc: int = sq.open("app.db", db)
```

What refcounting does not give you is dependency: a statement handle that
outlives its connection handle is still a use-after-free, and nothing here
catches that.

`link` accepts `-l<name>`, `-L<path>` and a bare library name, and refuses the
rest: a block that could hand arbitrary flags to the C compiler would be code
execution at build time. A library outside the default search paths — all of
Homebrew on Apple Silicon — is found with `pkg "openssl"`, which asks
`pkg-config` for its header and library paths and passes on only `-I`, `-L`,
`-l`, `-D`, `-pthread` and `-framework`. For the same reason every C name a block supplies —
`= "symbol"`, `closed by`, `freed by` — must be a plain C identifier.

**It is the C ABI, not the C language.** Any library exposing C-ABI symbols can
be bound — C++ behind `extern "C"`, Rust built as a `staticlib` with
`#[no_mangle] pub extern "C"`, Zig's `export fn`, Go with `-buildmode=c-archive`.
A C++ object held as a handle gets `delete`d through its closing function when
the last reference goes, so Dex's reference counting drives the C++ destructor.
What cannot cross is anything the C ABI has no word for: classes, methods,
templates, overloads, exceptions, traits, generics. Those need a flat
`extern "C"` shim, which is also the only shape this syntax can describe.

A library with no header to point at needs none: name no `include` and the C
functions are declared from the signatures written in Dex. Naming a header
stays the better path, because the C compiler then cross-checks your
declaration against the real one.

## Error Handling

Exceptions carry a type. Any struct with a `message: string` can be thrown,
`throws` says what escapes a signature, and a `try` takes a clause per type:

```dex
import "fmt"

struct ParseError(message: string, line: int) {}
struct IOError(message: string) {}

fn load(path: string): int throws ParseError, IOError {
    if (path == "") {
        throw IOError("no path")
    }
    throw ParseError("bad digit", 12)
}

fn main(): void {
    try {
        let n = load("config.txt")
        fmt.println("n=${n}")
    } catch (e: ParseError) {
        fmt.println("line ${e.line}: ${e.message}")
    } catch (e: IOError) {
        fmt.println("io: ${e.message}")
    }
}
```

`catch (e: Exception)` is the catch-all. Anything no clause matches carries on
outward after `finally` runs, and the heap the skipped frames held is released
on the way — so an error path does not accumulate memory.

The compiler does not enforce a `throws` declaration; `dex lint` checks it
against what the function actually does, and tells a caller that has no handler:

```
app.dx:13:12: 'load' may throw IOError (raised at app.dx:7:9), and 'readConfig'
does not declare throws — wrap it in try/catch, or declare the signature throws
IOError [uncaught-throw]
```

## Linting

Lint reports what compiles but is probably not meant: a binding nothing
reads, a statement nothing reaches, an exception nothing handles. Building
reports it by default and never changes what is emitted; findings only fail the
build with `--lint=strict`.

```bash
dex build app.dx                # build and report findings
dex build app.dx --no-lint      # build without them
dex build app.dx --lint=strict  # ... fail the build if anything is reported
dex lint app.dx                 # front end only: no C generated, no cc run
dex lint --rules                # list the rules
```

Eleven rules, each suppressible on the declaration it fires on with
`#[allow(<rule>)]`. The language server reports the same findings as you type.
See the [Language Reference](LANGUAGE.md#linting) for the full list.

## Concurrency

Spawn tasks, send and receive values through channels. Compiles down to pthreads and lock-free channels in C.

```dex
import "fmt"

fn compute(n: int): int {
    return n * 2
}

fn main(): void {
    let task = spawn compute(21)
    let result: int = receive(task)
    fmt.print(result) // 42

    // fan-in with channels
    let ch = channel(int)
    for (let i = 0; i < 5; i++) {
        spawn { send(ch, i * 10) }
    }
    let total = 0
    for (let i = 0; i < 5; i++) {
        total += receive(ch)
    }
    fmt.print(total) // 100
}
```

## Standard Library

| Module    | What it does |
|-----------|-------------|
| `fmt`     | Print to stdout |
| `http`    | HTTP server with route handlers, and a client |
| `json`    | `encode` / `decode` between structs and JSON, plus the `json.Value` type |
| `db`      | Database access (SQLite, Postgres, MySQL, MongoDB) |
| `redis`   | Redis client — strings, hashes, lists, sets, expiry |
| `kafka`   | Kafka producer and consumer |
| `crypto`  | Hashing, HMAC, base64, AES, RSA signing |
| `math`    | Trig, rounding, sqrt, pow, etc. |
| `num`     | Explicit numeric conversion — `toInt`, `toLong`, `toDouble`, `toChar` |
| `str`     | String helpers beyond the built-in methods |
| `time`    | Timestamps, formatting, and sleep |
| `file`    | Read, write, append, list, rename, move, remove, and byte-exact variants |
| `io`      | Standard input and line reading |
| `os`      | Environment variables, command execution |
| `ws`      | WebSocket server and client connections |
| `smtp`    | Sending mail |
| `reflect` | Read and write struct fields by position at runtime |

Every module is documented with its full function list in the
[Language Reference](LANGUAGE.md).

A basic web server:

```dex
import "http"
import "json"

struct Health {
    status: string
    uptime: long
}

fn handleHealth(): string {
    return json.encode(Health { status: "ok", uptime: 1200 })
}

fn main(): void {
    http.route("GET", "/health", handleHealth)
    http.listen(8080)
}
```

## Testing

Any file ending in `_test.dx` is a test file. Use `assert()` to check conditions:

```dex
fn main(): void {
    assert(2 + 2 == 4)
    assert("hello" != "world")
}
```

See the [Language Reference](LANGUAGE.md#testing) for the full testing specification.

## Editor Support

There's a VSCode extension in `editors/vscode/dex-lang/` with syntax highlighting and LSP integration (completions, diagnostics, hover, go-to-definition).

`self` highlights as a language variable, and inside a struct body completion offers `self` plus the struct's own fields and methods behind `self.`.

Lint findings appear as you type alongside type errors. Only what is actually
wrong is a warning — a `throws` the body contradicts, a `catch` that can never
run, code nothing reaches. The rest are hints, which editors render as a quiet
grey mark rather than a squiggle, because an editor asks for every diagnostic
on every keystroke.

Unresolved names carry an import quick fix. A call into a module the file never imported (`fmt.println(...)`, `userService.init(...)`) offers to add the import line, and an unqualified call offers to add the import and qualify the call. A type declared in another file (`fn f(c: Columns)` with `Columns` in `query.dx`) offers to import the file that declares it. Typing a bare name also completes to `module.function` from any stdlib module or sibling `.dx` file that exports it, writing the import alongside.

```bash
dex lsp
```

## How It Compiles

DexLang compiles `.dx` source to native binaries through C. See the [Language Reference](LANGUAGE.md#compilation) for the full compilation pipeline.

## Project Layout

See [CONTRIBUTING.md](CONTRIBUTING.md#project-structure) for the full directory structure.

## Planned

Proposals for work that is designed but not built live in [`design/`](design/):

- [Package management](design/package-management.md) — what remains:
  generics in user modules, and a registry once git hosting is not enough.

## Releasing

Releases are automated via GitHub Actions. When you push a version tag, the workflow cross-compiles binaries for all platforms, creates a GitHub Release, and deploys docs to GitHub Pages.

**First-time setup:**

1. Push the workflow and install script to `main` (just a normal `git push`)
2. In your GitHub repo, go to **Settings > Pages** and set the source to **GitHub Actions**

**To cut a release:**

```bash
make release VERSION=0.9.23
```

This creates an annotated tag `v0.9.23` and pushes it. The tag push triggers the workflow which:
- Builds `dex` for linux/amd64, linux/arm64, darwin/amd64, darwin/arm64
- Creates a GitHub Release with all 4 tarballs attached
- Deploys docs to GitHub Pages

**To also publish Docker images** (separate from the GitHub release):

```bash
make docker-publish DEX_VERSION=0.9.23
```

This builds the image for linux/amd64 and linux/arm64 with `docker buildx`
(making a builder named `dexlang-multi` the first time), pushes both under
the version and `latest`, and runs each to check its version. Set
`DOCKER_PLATFORMS` to build others. `make docker-build` makes a local image
for your own machine only, to try before publishing.

`make release` and `make docker-publish` are independent — run either or both.

---

Made with love and curiosity.
