# DexLang

A statically-typed language that compiles to C. Sits somewhere between TypeScript and Go in terms of how it feels to write, with the performance of C under the hood.

## Background

This started before and around the beginning of my undergrad. I was getting into C++ and later deep diving into Java during my master's, and I kept wanting something that had the developer experience of TypeScript and Go but compiled down to native code through C. So I just started building it.

The real motivation was understanding how everything fits together — lexers, parsers, type checkers, code generation — the full pipeline from source to binary. If it turns into something I can use for personal projects or internal tooling, even better. A garbage collector is planned but not yet implemented.

This is currently an independent project developed and maintained primarily by me. The long-term goal is to release it publicly and encourage community contributions, but in the near term development will remain focused on a small team, with a few colleagues expected to contribute as the project matures.

The boilerplate and most of the verbose code was written with the help of Claude. The language design, architecture decisions, fine tuning, and implementation verification are all mine.

## Quick Start

You need Go 1.24+ and a C compiler (`gcc` or `cc`).

```bash
# build the compiler
go build -o dex

# run a program
./dex run examples/hello.dx

# compile to a binary
./dex build examples/hello.dx
./build/hello

# run tests
./dex test
```

To install `dex` globally so you can use it from anywhere:

```bash
go build -o dex && sudo mv dex /usr/local/bin/
```

Then you can run `dex` directly from any directory:

```bash
dex run examples/hello.dx
dex build examples/hello.dx
```

## What It Looks Like

```dex
import "fmt"

struct Point {
    x: int
    y: int
}

fn distance(a: Point, b: Point): double {
    let dx: double = (b.x - a.x) * (b.x - a.x)
    let dy: double = (b.y - a.y) * (b.y - a.y)
    return math.sqrt(dx + dy)
}

fn main(): void {
    let origin = Point { x: 0, y: 0 }
    let target = Point { x: 3, y: 4 }
    fmt.print(distance(origin, target))
}
```

Types are explicit where it matters, inferred where it's obvious:

```dex
let name = "Dex"       // string
let count = 42          // int
let temps = [1.0, 2.5]  // double[]
const pi = 3.14159
```

## Concurrency

Spawn tasks, send and receive values through channels. Compiles down to pthreads and lock-free channels in C.

```dex
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

| Module | What it does |
|--------|-------------|
| `fmt`  | Print to stdout |
| `http` | HTTP server with route handlers |
| `json` | Build and stringify JSON objects |
| `db`   | Database access (SQLite, Postgres, MySQL, MongoDB) |
| `math` | Trig, rounding, sqrt, pow, etc. |
| `time` | Timestamps and sleep |
| `file` | Read, write, append, delete, upload |

A basic web server:

```dex
import "http"
import "json"

fn handleHealth(): string {
    let resp = json.new()
    resp = json.set(resp, "status", "ok")
    return resp
}

fn main(): void {
    http.route("GET", "/health", "handleHealth")
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

```bash
dex test                          # run all tests
dex test examples/loops_test.dx   # run a specific test
```

## Editor Support

There's a VSCode extension in `editors/vscode/dex-lang/` with syntax highlighting and LSP integration (completions, diagnostics).

```bash
dex lsp
```

## How It Compiles

```
.dx source -> lexer -> parser -> type checker -> C codegen -> gcc -> binary
```

The compiler is written in Go. The generated C links against a small runtime for the HTTP server, JSON handling, and database drivers.

## Project Layout

```
ast/        AST node definitions and type system
lexer/      Tokenizer
parser/     Parser
checker/    Type checker and semantic analysis
codegen/    C code generation
stdlib/     Standard library (Go definitions + C runtime)
lsp/        Language server
editors/    Editor extensions
examples/   Example programs
```

---

Made with love and curiosity.
