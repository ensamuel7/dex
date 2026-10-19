# Package management

**Status: proposed. None of this is implemented.** `LANGUAGE.md` describes what
the compiler does today; this describes what it should do next, and why.

## The problem

Colleagues want to write first-party libraries in Dex and use them from other
projects. Today an import is a path relative to the importing file, so the only
way to share a library is to copy its `.dx` files into each project.

## Libraries are shared as source, not binaries

A Dex library is distributed as `.dx` source (plus any `.c` and `.h` files it
ships) and compiled together with the program that uses it. Prebuilt Dex
binaries in a `lib/` directory cannot work, for reasons built into the compiler:

- **A program is one C translation unit.** Codegen writes one C file and `cc`
  builds it. There is no defined calling convention or object layout between
  two separately compiled Dex programs.
- **Generics are instantiated at the use site.** `Box<int>` is made by
  re-parsing the template inside the program that names it. A prebuilt library
  only has the instantiations its author happened to use.
- **Object layout depends on build flags.** `DEX_MEM_CHECK=1` widens every
  object header, and the allocation pool is on or off by build mode. A library
  built one way corrupts memory inside a program built the other way.
- **Ownership is decided per call.** Whether a value is retained, borrowed or
  released at a call is decided by codegen with both sides in view.

Go builds from source for the same reasons, and this design follows Go modules
closely.

## How it works

### A library

A library is a git repository with a `dex.mod` at its root. Its name is its
URL:

```
dexsqlite/
├── dex.mod
├── sqlite.dx
└── internal.dx
```

```
// dex.mod
module github.com/acme/dexsqlite
```

### A release

A release is a git tag. There is no upload and, to start with, no registry:

```sh
git tag v1.0.0
git push --tags
```

Versions are semantic. A breaking change is a new major version, and from v2 on
the major version is part of the name (`github.com/acme/dexsqlite/v2`), so v1
and v2 are different packages and can coexist.

### Using a library

```sh
dex get github.com/acme/dexsqlite@v1.0.0
```

This:

1. downloads the repository at that tag into the module cache,
   `~/.dex/pkg/github.com/acme/dexsqlite@v1.0.0/`, shared by every project and
   never edited in place;
2. adds `require github.com/acme/dexsqlite v1.0.0` to the project's `dex.mod`;
3. records a hash of the downloaded source in `dex.sum`. A build whose source
   does not match its hash fails.

```dex
import "github.com/acme/dexsqlite" as "sqlite"

fn main(): void {
    let db = sqlite.connect("app.db")
}
```

`dex build` reads the version from `dex.mod`, takes the source from the cache,
and compiles it with the program into one binary.

### Dependencies of dependencies

A library's own requirements are in its own `dex.mod`; `dex get` follows them.
When two libraries need different versions of the same dependency, the build
uses the **lowest version that satisfies all of them** (Go's minimal version
selection). That is deterministic, needs no solver, and means `dex.mod` plus
`dex.sum` is the whole lock: there is no separate lockfile.

There is only ever one version of a package per major version in a build. Two
would mean two copies of the same generated C names.

### Developing a library and an app together

```
// the app's dex.mod
replace github.com/acme/dexsqlite => ../dexsqlite
```

The import then reads the local checkout instead of the cache. This also covers
a monorepo where several packages are built together and never published.

### The files

| File | Where | What it holds | Written by |
|---|---|---|---|
| `dex.mod` | every project | its name and what it requires | `dex get`, or by hand |
| `dex.sum` | every project | hashes of downloaded source; commit it | `dex get` only |
| `~/.dex/pkg/` | each machine | downloaded source | Dex only |
| tags (`v1.0.0`) | the git host | releases | the library author |

## Libraries that wrap C

A library can declare `external module` blocks. Its `include`, `link` and
`pkg` lines travel with its source and reach the C build of every program that uses it, so
the program's author never writes `-lsqlite3`.

What is not downloaded is the C library itself. Whoever builds needs it
installed, headers included, as with cgo; `pkg "name"` finds it wherever the
machine put it. A library can instead ship the C source (sqlite is a single
`sqlite3.c`) beside its `.dx` files so it builds with nothing installed.

`examples/externalSqlite_test.dx` binds sqlite end to end — `pkg` to find
it, out-parameters to open and prepare, handles closed by their own functions
— which is the shape a library wrapping C takes.

`link` accepts only `-l`, `-L` and bare library names, `pkg` passes on only
the flag kinds a library needs, and every C name in a block must be a C
identifier. A dependency must not be able to run code at
build time, and these are what stop it. There are deliberately no install
scripts, which is npm's largest hole.

## What the compiler must change first

These block a package manager regardless of how fetching is done.

### 1. A package's names belong to the package — done

Each file now has its own identity (`util`, then `util_2` for a different
`util.dx`), what a file calls an import is local to it, and an external module is
visible only in the file that declares it. `examples/moduleIdentity_test.dx`
uses two libraries that each contain a `util.dx` with the same names. What
follows is the problem as it stood.

Today a module's name is its file's base name, shared across the whole
program, and that name becomes the C prefix (`alpha_make`, `alpha_Response`).
Two libraries that each have a `util.dx` merge into one module `util`, and a
function both declare is an error the app author cannot fix: the clash is in
code they do not own, and an import alias cannot reach it.

External modules have the same problem. They are global, and two libraries
that each declare `external module cstr` collide. Today that is silently
mishandled rather than reported (identical blocks merge; differing ones lose
the second, whose calls then fail as undefined), contrary to `LANGUAGE.md`,
which says it is an error. A library's raw C bindings are also callable by every
program that imports it, so they become public API by accident.

The fix: a module's identity comes from its package path, so generated C names
are unique by construction; and an `external module` is visible only in the
file that declares it. A library calls its own bindings as it does now, an app
never sees them, and two libraries can use the same name. Sharing a set of
bindings is done by making them a library and importing it like any other.

### 2. Imports resolve through `dex.mod` — done

`dex mod init`, `module`, `require` and `replace` (to a directory) work, and an
import whose first segment is a host name resolves through them. Two choices the
sketch above left open: importing a module path whole reads
`<root>/<last segment>.dx`, and a `replace` must be backed by a `require`. See
LANGUAGE.md, "Modules and dex.mod".

A non-relative import path is looked up in `dex.mod` and read from the cache,
or from a `replace` directory.

### 3. User modules can declare generics

`resolveModuleFile` rejects a generic in a user module, because a template
lives in the parser that read it. Libraries need generics almost at once, so
this moves from deliberately unsupported to required.

`private` already exists, so a library can hide its internals.

## Order of work

1. ~~Package-scoped names and file-scoped external modules.~~ Done.
2. ~~`dex.mod` and import resolution, with `replace` only.~~ Done: sharing
   from local directories and monorepos works.
3. `dex get`: fetching tags into the cache and writing `dex.sum`.
4. Generics in user modules.
5. A registry or proxy, only once git hosting is not enough.

## What this takes from elsewhere

- **From Go modules:** URLs as names, tags as releases, `go.sum`, minimal
  version selection, the major version in the path, a read-only cache,
  `replace`, building from source.
- **From npm:** workspaces, covered here by `replace`.
- **Avoided from npm:** install scripts, and several versions of one package in
  one build.
- **Avoided from Deno:** versions written into import paths in source. Keeping
  them in `dex.mod` means an upgrade touches one file.
