# DexLang

A statically-typed language that compiles to C. Sits somewhere between TypeScript and Go in terms of how it feels to write, with the performance of C under the hood.

## Background

This started before and around the beginning of my undergrad. I was getting into C++ and later deep diving into Java during my master's, and I kept wanting something that had the developer experience of TypeScript and Go but compiled down to native code through C. So I just started building it.

The real motivation was understanding how everything fits together — lexers, parsers, type checkers, code generation — the full pipeline from source to binary. If it turns into something I can use for personal projects or internal tooling, even better. A garbage collector is planned but not yet implemented.

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
```

## Docker

Docker images are available for CI/CD or environments where you don't want to install `dex` directly:

```bash
docker pull dexlang/dexlang:latest
docker run --rm -v "$(pwd):/workspace" dexlang/dexlang run app.dx
```

## What It Looks Like

```dex
import "fmt"
import "math"

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
| `json` | Build, stringify, and parse JSON (including struct serialization) |
| `db`   | Database access (SQLite, Postgres, MySQL, MongoDB) |
| `math` | Trig, rounding, sqrt, pow, etc. |
| `time` | Timestamps and sleep |
| `file` | Read, write, append, delete, upload |
| `os`   | Environment variables, command execution |
| `ws`   | WebSocket server and client connections |

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

See the [Language Reference](LANGUAGE.md#testing) for the full testing specification.

## Editor Support

There's a VSCode extension in `editors/vscode/dex-lang/` with syntax highlighting and LSP integration (completions, diagnostics).

```bash
dex lsp
```

## How It Compiles

DexLang compiles `.dx` source to native binaries through C. See the [Language Reference](LANGUAGE.md#compilation) for the full compilation pipeline.

## Project Layout

See [CONTRIBUTING.md](CONTRIBUTING.md#project-structure) for the full directory structure.

## Releasing

Releases are automated via GitHub Actions. When you push a version tag, the workflow cross-compiles binaries for all platforms, creates a GitHub Release, and deploys docs to GitHub Pages.

**First-time setup:**

1. Push the workflow and install script to `main` (just a normal `git push`)
2. In your GitHub repo, go to **Settings > Pages** and set the source to **GitHub Actions**

**To cut a release:**

```bash
make release VERSION=0.1.4
```

This runs `git tag v0.1.4` and `git push origin v0.1.4`. The tag push triggers the workflow which:
- Builds `dex` for linux/amd64, linux/arm64, darwin/amd64, darwin/arm64
- Creates a GitHub Release with all 4 tarballs attached
- Deploys docs to GitHub Pages

**To also publish Docker images** (separate from the GitHub release):

```bash
make docker-publish DEX_VERSION=0.1.4
```

`make release` and `make docker-publish` are independent — run either or both.

---

Made with love and curiosity.
