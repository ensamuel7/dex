# csplit: compiling a large program on every core

`dex build` generates one C file for the whole program: the runtime, the
stdlib modules it imports and every function the program defines. The
compiler optimises one file on one core, so a large program (the ChargeBase
backend is 230,000 lines of C) took close to two minutes no matter how many
cores the machine had.

## What was slow

Measured on the ChargeBase backend (`-O3 -flto`, 11-core M-series Mac, and
`debian:bookworm-slim` with gcc 12 under OrbStack on the same machine):

| step | clang (Mac) | gcc (Linux) |
| --- | --- | --- |
| dex itself (parse, check, generate) | 2 s | 2 s |
| compile to LTO bitcode (`-c -flto`) | 41 s | 10 s |
| link-time optimisation | 65 s | 90 s |
| total | 112 s | 100 s |

No single function dominates: there are 7,000 of them, the largest 1,500
lines. Lowering the optimisation level does not help enough to be worth
changing the program's code: plain `-O2` without LTO still took 81 s (clang)
and 71 s (gcc).

gcc already splits the link-time work into 128 partitions, but it runs them
in parallel only by handing a makefile to `make`, and a slim build image has
no `make`, so it ran all 128 one after another ("using serial compilation of
128 LTRANS jobs").

## What dex does now

1. **Split.** `Split` cuts the generated C into one unit per core. Every unit
   is the whole file in its original order with the bodies of the functions
   it does not own replaced by prototypes, so macros, `#if` blocks and
   declaration order mean exactly what they meant in one file. Function
   bodies are dealt out in contiguous runs of roughly equal size.
2. **Linkage.** The runtime was written for one translation unit, with every
   symbol `static`. In a split build:
   - a function that moves loses `static` (and `inline`) in its definition
     and in every prototype, and is compiled with `-fvisibility=hidden`, so
     it is still invisible outside the executable;
   - a file-scope variable is defined once, in the first unit, and declared
     `extern` in the others, so runtime state (thread pools, the arena,
     globals) still exists exactly once;
   - a `static inline` helper with no static local is copied into every unit
     as before, so `dex_retain` and friends still inline without link-time
     help;
   - a read-only array of unspecified size (reflection tables) stays a
     private copy in each unit, because `sizeof` cannot see through an
     `extern` declaration of one;
   - a top-level macro instantiation (`DEX_MAP_DEFINE(...)`,
     `DEX_LIT_DEF(...)`) is copied as written; these expand to stateless
     static functions and immortal string literals.
3. **Link-time optimisation joins the units again.** clang links them with
   ThinLTO, whose per-unit backends run in parallel (full LTO would merge
   them back into one serial job). gcc links with `-flto=auto`; when the
   system has no `make`, dex puts itself on `PATH` under that name for the
   link and runs gcc's partition makefile itself (`makeshim.go`). The
   optimisation level is unchanged (`-O3`), and calls across units are
   inlined at link time as calls within the one file were.
4. **Cache.** Each unit's object is kept in `build/<name>.units/`, keyed by
   a hash of its text and the compiler flags, and reused when unchanged. On
   the Mac the ThinLTO backends are cached there too. An edit inside one
   function body recompiles one unit; a new string literal or a changed
   signature changes the declarations every unit carries, so it recompiles
   them all.

Each unit gets at least 512 KB of function bodies (`minUnitBytes` in
`cbuild.go`), so a program with less than twice that is not split: below it
the declarations every unit re-parses cost more than the parallelism saves.
Small programs, including every example and test, build exactly as before.

A program whose external module includes a header of its own
(`include "shim.h"`) is not split either. The preprocessor pastes that header
into every unit, out of the scanner's sight, and a `static` counter in it would
silently become one counter per unit (`examples/external_test.dx` caught
exactly that).

`Split` refuses any construct it does not recognise; and if a split build
fails for any reason, dex says so and builds the program again as one unit,
so a scanner mistake costs time, not a build.

## Results

Wall clock, before and after:

| ChargeBase backend | clang (Mac, 11 cores) | gcc (Linux container, 11 cores) |
| --- | --- | --- |
| before | 112 s | 100 s |
| one unit, gcc partitions in parallel | 112 s | 24 s |
| split (default) | 22 s | 12 s |
| rebuild, nothing changed | 1.2 s | 11 s |

DexRail (`src/main.dx`, smaller) on the Mac: 36 s before, 9 s split, 0.7 s
for a rebuild with nothing changed.

A microbenchmark (map, array, string and struct work across units) ran in
the same time split and whole.

## Controls

- `DEX_JOBS=N` caps the compiler processes; `DEX_JOBS=1` compiles one unit,
  as before.
- `DEX_SPLIT_MIN=bytes` lowers the size floor, which is how the tests split
  small programs.
- `DEX_KEEP_C=1` keeps the unit sources in `build/<name>.units/` beside the
  whole file.
