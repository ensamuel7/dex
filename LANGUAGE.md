# DexLang Language Reference

DexLang is a statically-typed language that compiles to C. It is designed for building web servers and applications. Source files use the `.dx` extension.

## Compilation

```
dex build <file.dx>       # compiles to native binary
dex run <file.dx>         # compiles and runs
dex dev <file.dx>         # rebuilds and reruns on every change
dex profile <file.dx>     # runs with the memory profiler and opens a report
dex test [file_test.dx]   # runs test files (*_test.dx)
dex format [path...]      # lays files out the one canonical way
dex lint <file.dx>        # reports lint findings without building
dex check <file.dx>       # runs cppcheck over the generated C
dex mod init <path>       # starts a dex.mod naming this project — see Modules and dex.mod
```

`build`, `run` and `dev` report lint findings alongside the build unless given
`--no-lint`; `test` reports them only when given `--lint`. `--lint=strict` fails
the build when there are any. See [Linting](#linting).

`dex profile` builds with the memory profiler, runs the program, and writes
`build/<name>.profile.html`: live memory over time, a sortable table of the
lines that allocate and hold memory (at the peak, at their own peak, at exit,
and in total), and the source with each line's share beside it. Growth of an
array, map or string builder is charged to the line that grew it, and held by
the line that created it. `--no-open` writes the report without opening it;
`dex profile <file.json>` renders a profile written earlier.

`DEX_PROFILE=1` on `build` or `run` compiles the profiler into the binary
directly; it writes JSON at exit to `DEX_PROFILE_OUT` (default
`dex-profile.json`). For a server, `kill -USR1 <pid>` writes a snapshot without
stopping it, and SIGINT or SIGTERM write one before it exits — in each case only
if the program has not installed a handler of its own. `DEX_PROFILE_QUIET=1`
drops the summary printed at exit. The profiler and `DEX_MEM_CHECK` cannot be
combined.

Pipeline: `.dx` source → tokens → AST (Abstract Syntax Tree) → type check → C code → native binary (via `cc`/`gcc`)

---

## Program Structure

Every program requires a `main` function. It returns `void` (the compiler emits an implicit exit code of 0):

```dex
fn main(): void {
  // your program here
}
```

### Statement Termination

Semicolons are **optional** in DexLang. Statements are terminated by newlines or by context (braces, keywords). You may add a trailing semicolon after any statement or import declaration if you prefer C/Java/Go-style syntax. Both styles can be mixed freely:

```dex
// Without semicolons
let x: int = 5
fmt.println("hello")

// With semicolons
let y: int = 10;
fmt.println("world");

// Mixed
let a: int = 1;
let b: int = 2
```

---

## Keywords

| Keyword    | Purpose                              |
|------------|--------------------------------------|
| `fn`       | Declare a function                   |
| `function` | Declare a function (alias for `fn`)  |
| `let`      | Declare a variable with a type       |
| `const`    | Declare an immutable variable        |
| `return`   | Return a value from a function       |
| `if`       | Conditional branch                   |
| `else`     | Alternative branch for `if`          |
| `while`    | Loop                                 |
| `for`      | C-style for loop                     |
| `foreach`  | Iterate over array elements          |
| `as`       | Separator in foreach syntax          |
| `break`    | Exit a loop early                    |
| `continue` | Skip to next loop iteration          |
| `true`     | Boolean literal                      |
| `false`    | Boolean literal                      |
| `import`   | Import a standard library module     |
| `public`   | Mark function/struct/field as public (default) |
| `private`  | Mark function/struct/field as private |
| `spawn`    | Launch a concurrent task             |
| `chan`      | Channel type annotation              |
| `try`      | Start a try-catch-finally block      |
| `catch`    | Handle an exception in a try block   |
| `finally`  | Cleanup block that always runs       |
| `throw`    | Throw an exception                   |
| `throws`   | Declare that a function may let an exception escape |
| `switch`   | Multi-way branch statement           |
| `case`     | Case branch in a switch              |
| `default`  | Default branch in a switch           |
| `int`      | Signed integer type                  |
| `long`     | Signed long integer type             |
| `double`   | Double-precision floating point type |
| `char`     | Single character type                |
| `bool`     | Boolean type                         |
| `string`   | String type                          |
| `void`     | No return value (for main/functions) |
| `struct`   | Define a composite type               |
| `enum`     | Define a named set of constants       |
| `interface`| Define a structural type contract     |
| `null`     | Absent value for optional types       |
| `match`    | Pattern matching expression           |
| `defer`    | Defer a call to function exit         |
| `external` | Declare a module of C functions       |

---

## Types

### Primitive types

| Type     | Description                        | Examples              |
|----------|------------------------------------|-----------------------|
| `int`    | Signed integer                     | `0`, `42`, `-7`       |
| `long`   | Signed long integer                | `0`, `1000000`        |
| `double` | Double-precision floating point    | `3.14`, `0.5`, `1.0`  |
| `char`   | Single character                   | `'A'`, `'z'`, `'\n'`  |
| `bool`   | Boolean                            | `true`, `false`       |
| `string` | String                             | `"hello"`, `"world"`  |
| `void`   | No return value (functions only)   | —                     |

Two further heap types come from the standard library and are written with their
module prefix: `StringBuilder` (see the StringBuilder section) and `json.Value`,
a dynamic JSON document (see the `json` module).

### Array types

Append `[]` to a primitive type to make an array type.

| Type       | Description              |
|------------|--------------------------|
| `int[]`    | Array of integers        |
| `long[]`   | Array of long integers   |
| `double[]` | Array of doubles         |
| `bool[]`   | Array of booleans        |
| `string[]` | Array of strings         |
| `char[]`   | Array of characters      |

Variables can have explicit type annotations or use type inference (see Variables section).

### Optional types

Append `?` to any type to make it optional. Optional types can hold either a value of the inner type or `null`.

```dex
let x: int? = 5       // has a value
let y: int? = null     // no value
let z: int?            // defaults to null (no initializer needed)
let s: string? = null  // optional string
```

| Type       | Description              |
|------------|--------------------------|
| `int?`     | Optional integer         |
| `long?`    | Optional long integer    |
| `double?`  | Optional double          |
| `bool?`    | Optional boolean         |
| `string?`  | Optional string          |
| `char?`    | Optional character       |
| `Struct?`  | Optional struct          |

**Null checks and type narrowing:**

Use `== null` or `!= null` to check if an optional has a value. Inside an `if` block guarded by a null check, the variable is automatically narrowed to the inner (non-optional) type.

```dex
let x: int? = 5
if (x != null) {
    // x is narrowed to int here
    let y: int = x + 1
}
```

**Functions with optional types:**

Functions can accept and return optional types.

```dex
fn findUser(id: int): string? {
    if (id == 1) {
        return "Alice"
    }
    return null
}

fn greet(name: string?): void {
    if (name != null) {
        fmt.println(name)
    }
}
```

**Rules:**
- `null` can only be assigned to optional types (`T?`), not plain types (`T`).
- A value of type `T` can be assigned to a variable of type `T?` (automatic wrapping).
- Wrapping a struct copies it, but copying a `Struct?` shares it: `let b: Point? = a`
  names the same struct as `a`, so a method that changes it through one is seen
  through the other. The struct is freed when the last `Struct?` holding it goes.
- Double-optional types (`T??`) are not allowed.
- There is no force-unwrap operator; use null checks for safe access.

---

## Variables

### Declaration (explicit type)

```dex
let name: type = value
```

Examples:

```dex
let x: int = 10
let big: long = 1000000
let pi: double = 3.14
let name: string = "Dex"
let flag: bool = true
let ch: char = 'A'
```

### Char Literals

Char literals use single quotes and support escape sequences:

```dex
let a: char = 'A'
let newline: char = '\n'
let tab: char = '\t'
let backslash: char = '\\'
let quote: char = '\''
```

Char is a numeric type (narrower than int). Arithmetic operations on chars widen to int:

```dex
let ch: char = 'A'
let code: int = ch + 1    // 66
let upper: char = 'a'
let diff: int = upper - 'A'  // 32
```

### Declaration (type inference)

When the type can be determined from the right-hand side, the `: type` annotation can be omitted:

```dex
let x = 42          // inferred as int
let pi = 3.14       // inferred as double
let name = "Dex"    // inferred as string
let flag = true     // inferred as bool
let nums = [1, 2, 3]  // inferred as int[]
```

Type inference does **not** work with empty array literals — these still require an explicit type:

```dex
let items: int[] = []   // explicit type required
```

### Multi-declaration

Multiple variables of the same type can be declared on one line:

```dex
let x, y, z: int = 0
let a, b: string?          // both default to null
const width, height: int = 100
```

Multi-declaration requires an explicit type annotation.

### Assignment

```dex
x = 20
name = "DexLang"
```

A variable must be declared with `let` before it can be assigned.

### Compound Assignment

```dex
x += 5    // equivalent to x = x + 5
x -= 2    // equivalent to x = x - 2
```

### Increment / Decrement

```dex
x++    // equivalent to x = x + 1
x--    // equivalent to x = x - 1
```

These operators work on numeric variables (`int`, `long`, `double`).

### Constants

Declare immutable variables with `const`. Constants cannot be reassigned, incremented, decremented, or modified with compound assignment after initialization.

```dex
const pi = 3.14
const max_size: int = 100
const name: string = "DexLang"
```

`const` supports the same syntax as `let` — both explicit type annotations and type inference work.

Attempting to modify a const variable is a compile error:

```dex
const x = 42
x = 10        // ERROR: cannot reassign const variable 'x'
x++           // ERROR: cannot modify const variable 'x'
x += 5        // ERROR: cannot modify const variable 'x'
```

---

## Access Modifiers

Functions, structs, and struct fields can be marked `public` or `private`. **By default, all functions and structs are public** — the `public` keyword is optional and exists only for explicitness. Use `private` to restrict access.

| Modifier  | Effect                                      |
|-----------|---------------------------------------------|
| (none)    | Public (default)                            |
| `public`  | Public (explicit, same as default)          |
| `private` | Restricted to the defining file             |

### Visibility semantics

Visibility in DexLang is **file-scoped**, not class-scoped (DexLang is not an object-oriented language). This means:

- **`public`** (default): The function is accessible from any file that imports the module. If no modifier is specified, the function is public.
- **`private`**: The function is only accessible within the same `.dx` file where it is defined. Another file that imports the module cannot call a private function.

### Functions

```dex
// These two are equivalent — functions are public by default
fn helper(): int {
  return 42
}

public fn also_public(): int {
  return 42
}

// Use private to restrict access
private fn internal_helper(): int {
  return 99
}
```

### Multi-file visibility example

In a project with multiple `.dx` files, `private` prevents access from other files:

**`math_utils.dx`:**

```dex
// Public — callable from any file that imports this module
fn square(n: int): int {
  return n * n
}

// Private — only callable within math_utils.dx
private fn clampInternal(n: int, lo: int, hi: int): int {
  if (n < lo) { return lo }
  if (n > hi) { return hi }
  return n
}

// Public wrapper that uses the private helper
fn clamp(n: int): int {
  return clampInternal(n, 0, 100)
}
```

**`main.dx`:**

```dex
fn main(): void {
  let x: int = square(5)            // OK — square is public
  let y: int = clamp(150)           // OK — clamp is public
  // let z: int = clampInternal(5, 0, 10)  // ERROR — clampInternal is private to math_utils.dx
}
```

### Structs and struct fields

Structs are public by default. Individual fields can be marked `private`:

```dex
struct User {
  name: string
  private password: string
}

// Structs can also have an explicit access modifier
private struct InternalConfig {
  debug: bool
}
```

**Field types** — a struct field may be any primitive, another struct, an enum, a
map, an optional, a reference, or an **array** (including an array of structs):

```dex
struct Tag {
  id: int
  kind: string
}

struct Post {
  tag: string
  views: int[]             // primitive array
  names: string[]
  tags: Tag[]              // struct array
}
```

Array fields are reference-counted like any other array, so they stay valid after
the value that supplied them goes out of scope. They also round-trip through
`json.encode` / `json.decode`.

### Zero values

Omitted fields in struct literals are initialized to their zero value, similar to Go:

| Type      | Zero value |
|-----------|------------|
| `int`     | `0`        |
| `long`    | `0`        |
| `double`  | `0.0`      |
| `bool`    | `false`    |
| `char`    | `'\0'`     |
| `string`  | `""`       |
| struct    | all fields zero-initialized recursively |
| array     | `null`     |
| optional  | `null`     |

```dex
struct Point {
  x: int
  y: int
}

let origin = Point{}           // Point{x: 0, y: 0}
let p = Point{x: 5}           // Point{x: 5, y: 0}
```

Nested structs are also zero-initialized recursively:

```dex
struct Line {
  start: Point
  end: Point
  label: string
}

let line = Line{label: "AB"}
// Line{start: Point{x: 0, y: 0}, end: Point{x: 0, y: 0}, label: AB}
```

### Value and reference semantics

DexLang uses **pass-by-value** as the default for primitives and structs. Arrays and strings are always passed as pointers. The `&Type` syntax opts into pass-by-reference for structs.

**Primitives** (`int`, `long`, `double`, `bool`, `char`) are always pass-by-value. Changes inside a function do not affect the caller:

```dex
fn increment(n: int): void {
  n = n + 1   // modifies the local copy only
}

let x: int = 5
increment(x)
fmt.println(x)   // still 5
```

**Structs** are pass-by-value by default. The function receives a copy:

```dex
fn tryUpdate(user: User): void {
  user.name = "changed"   // modifies the local copy only
}

let u = User{name: "john", age: 25}
tryUpdate(u)
fmt.println(u.name)   // still "john"
```

Use `&StructName` in the parameter type to pass by reference. Mutations through the reference affect the original:

```dex
fn update(user: &User): void {
  user.name = "changed"   // modifies the original
}

let u = User{name: "john", age: 25}
update(u)
fmt.println(u.name)   // "changed"
```

The caller does not need to write `&` at the call site — the compiler inserts it automatically. Field access, field assignment, and method calls all work transparently on references:

```dex
fn getName(user: &User): string {
  return user.name        // field access works with dot syntax
}

fn greetRef(user: &User): string {
  return user.greet()     // method calls work normally
}
```

**Arrays** (`int[]`, `string[]`, etc.) and **strings** are always pointers under the hood. Mutations to array contents are visible to the caller:

```dex
fn addItem(arr: int[]): void {
  arr.push(99)   // modifies the original array
}

let nums: int[] = [1, 2, 3]
addItem(nums)
fmt.println(nums)   // [1, 2, 3, 99]
```

**Reference fields in structs** — struct fields can use `&StructName` to hold a reference rather than a copy. This enables dependency injection patterns:

```dex
struct Database {
  host: string
}

struct Service {
  db: &Database
}

let db = Database{host: "localhost"}
let svc = Service{db: db}   // svc.db points to db
```

**Summary:**

| Type | Default passing | Mutable? | Caller sees changes? | Allocation | Opt-in reference |
|------|----------------|----------|---------------------|------------|-----------------|
| `int`, `long`, `double` | value | Yes | No | stack | `&int`, `&long`, `&double` |
| `bool`, `char` | value | Yes | No | stack | `&bool`, `&char` |
| `string` | pointer | Yes | Yes | heap (ref-counted) | — |
| `int[]`, `string[]`, etc. | pointer | Yes | Yes | heap (ref-counted) | — |
| `struct` | value (copy) | Yes | No | stack | `&Struct` |
| `&Struct` | pointer | Yes | Yes | stack (pointed-to) | — |
| `const` variable | — | No | — | same as type | — |
| `enum` | value | No (constants) | No | stack | — |
| `map[K, V]` | pointer | Yes | Yes | heap (ref-counted) | — |

**Rules:**
- Struct and primitive types (`int`, `long`, `double`, `bool`, `char`) can use the `&` prefix for pass-by-reference. `&string` is a compile error (strings are already heap-allocated pointers).
- Double references (`&&Struct`) are not allowed.

---

### Field tags

A field may carry a tag: backtick-delimited metadata written after its type.

```dex
struct Book {
    id: long
    pages: int
    authorName: string `db:"COALESCE(a.name, '')"`
    releasedOn: string `db:"b.released_on::text"`
    priceMinor: long `db:"b.price_minor" json:"price"`
    note: string
}
```

The format is space-separated `key:"value"` pairs, so one field can carry
several namespaces at once. A tag is **raw**: no escape sequences, no
interpolation, which is what lets an SQL fragment like `COALESCE(a.name, '')` be
written exactly as it will be used.

Tags are read back through [`reflect.fieldTag`](#reflect):

```dex
reflect.fieldTag(bk, 2, "db")      // "COALESCE(a.name, '')"
reflect.fieldTag(bk, 4, "json")    // "price"
reflect.fieldTag(bk, 0, "db")      // "" — that field has no tag
```

#### A tag is data, not a directive

The compiler stores a tag and never interprets it. Nothing in the language knows
what `db` or `json` means; a library decides what its own keys mean, and an
unrecognised key is simply one nobody asked about.

That is the distinction between a tag and an annotation. `#[owned]` is a
**directive** — it changes what the compiler emits, so the set of valid
annotations is closed and an unknown one is an error. A tag is **data**, so any
key is allowed and the compiler checks only that the pairs are well formed:

```dex
f: string `db:x`             // error: tag key 'db' must have a quoted value
f: string `db`               // error: tag key 'db' is missing its ':"value"'
f: string `db:"a" db:"b"`    // error: tag repeats key 'db'
f: string `db:"a"json:"b"`   // error: needs a space between keys
```

This catches the typo that would otherwise make a key silently invisible to
whatever was meant to read it.

The one annotation that is neither directive nor data is `#[allow(...)]`: it
suppresses a [lint](#linting) finding rather than changing what is emitted, so
it is accepted on any declaration and the rule it names is checked by lint.

#### Deriving a projection from the struct

Tags exist so that a mapping can be read off the type rather than written beside
it. Reading a tag is a read, so the read-only reflection above is enough:

```dex
fn columnsFor(bk: Book, alias: string): string[] {
    let out: string[] = []
    let i: int = 0
    while (i < reflect.fieldCount(bk)) {
        let name: string = reflect.fieldName(bk, i)
        let tagged: string = reflect.fieldTag(bk, i, "db")
        if (tagged.isEmpty()) {
            out.push(alias + "." + snake(name) + " AS " + name)
        } else {
            out.push(tagged + " AS " + name)
        }
        i = i + 1
    }
    return out
}
```

```
b.id AS id
COALESCE(a.name, '') AS authorName
b.price_minor AS priceMinor
```

Any struct may carry tags — there is no separate kind of struct for it. The key
is the discriminator: a type that means nothing to a database layer simply has
no `db` key, and `fieldTag` reports `""`.

## Enums

Enums define named sets of integer constants.

### Enum definition

```dex
enum Color {
    Red
    Green
    Blue
}
```

Variants are assigned sequential integer values starting at 0.

### Variants that carry values

A variant may carry fields, which makes the enum a **tagged union** — a closed
set of shapes, each with its own data:

```dex
enum Expr {
    Lit(long)
    Add(Expr, Expr)
    Mul(Expr, Expr)
    Neg(Expr)
}
```

A variant may name the enum itself, which is what makes this useful for trees.
Build one by calling the variant; a variant that carries nothing is still
written without parentheses:

```dex
let two: Expr = Expr.Lit(2)
let sum: Expr = Expr.Add(two, Expr.Lit(3))
```

### Matching a tagged union

`switch` destructures it, binding the payload for the arm's body. Use `_` for a
field the arm does not need:

```dex
fn eval(e: Expr): long {
    switch (e) {
        case Expr.Lit(v):    { return v }
        case Expr.Add(a, b): { return eval(a) + eval(b) }
        case Expr.Mul(a, b): { return eval(a) * eval(b) }
        case Expr.Neg(a):    { return 0 - eval(a) }
    }
}
```

**The match must be exhaustive.** A switch over a payload enum that omits a
variant is a compile error naming the ones it missed, unless it has a
`default`. That is the point of a closed set: adding a variant later makes
every incomplete match fail to compile instead of silently falling through.

A binding **borrows**. The value it names belongs to the enum being matched,
which outlives the arm, so nothing is copied and nothing is freed at the end of
the arm.

### The two representations

| | Plain enum | Payload enum |
|---|---|---|
| Representation | an integer constant | a refcounted tagged union |
| Allocation | none | one box per value |
| Comparison | `==` | `switch` with patterns |
| Recursive variants | n/a | yes — this is why it is boxed |

A plain enum is unchanged by any of this: still an integer, still comparable
with `==`, still usable as a map key or a struct field at no cost. The box
exists only because `Add(Expr, Expr)` has no finite size as a value — the same
reason Rust spells that case `Box<Expr>`.

Dispatch is a switch on the tag: one compare and a jump, with no vtable and no
indirect call. On a 40,010-node expression tree evaluated 5,000 times, the
tagged union evaluates in 176ms where the same AST modelled with interface
values takes 391ms.

### Restrictions

- A pattern binds names only. `case Expr.Lit(5)` — matching on the payload's
  value as well as its tag — is not supported; test the bound name in the body.
- One pattern per `case`. Two variants matched by one arm would carry different
  payloads, so there would be no consistent type for a name to have.
- A payload enum is not a map key or a `json` field; a plain enum still is.
- `Shape?` is an optional payload enum, absent as `null`.
- Interpolating a payload enum, concatenating one with a string, or printing
  one renders its variant and payload: `"${Expr.Add(Expr.Lit(2), Expr.Lit(3))}"`
  is `"Add(Lit(2), Lit(3))"`, and a variant with no payload is just its name. A
  plain enum is still an integer and renders as one.

### Enum usage

```dex
let c: Color = Color.Red

if (c == Color.Green) {
    fmt.println("green")
}

switch (c) {
    case Color.Red: {
        fmt.println("red")
    }
    case Color.Green, Color.Blue: {
        fmt.println("not red")
    }
}
```

Enums are value types (integers) — no heap allocation or refcounting needed.

### A local declaration shadows an imported one

A type declared in the file being compiled wins the bare name over one of the
same name in an imported module. The module's is still reached by qualifying
it, so nothing is hidden:

```dex
import "signupController"        // declares SignupController

struct SignupController {        // mine
    local: int
}

let mine: SignupController = SignupController { local: 1 }
let theirs: signupController.SignupController = signupController.new(7)
```

Two *modules* declaring the same name is still an ambiguity error when the
bare name is used, because neither is the file's own.

### Interfaces across modules

A module may declare an interface, and it belongs to that module the way its
structs and enums do. This is how a library states a contract its callers
implement without naming any concrete type:

```dex
// eventbus/bus.dx
interface Handler {
    fn onMessage(payload: string): bool
}

struct Consumer {
    fn dispatch(env: Envelope, h: Handler): string { ... }
}
```

```dex
// the consuming project
import "eventbus/bus"

struct PaidHandler {
    minimumMinor: long

    fn onMessage(payload: string): bool {
        return payload.len() > 0
    }
}

let ok: Handler = PaidHandler { minimumMinor: 1000 }
```

Structural typing does not care where either side was declared, so a struct in
the importing file satisfies a module's interface with no declaration of
intent. The bare name resolves when nothing else in scope declares it; two
modules may each declare a `Handler`, and then the bare name is an ambiguity
error naming both while `alpha.Handler` and `beta.Handler` stay distinct types.

### Enums across modules

An enum declared in one file is used from another through its module, in every
position a type or a value can appear:

```dex
import "type/finding"

fn rank(s: finding.Severity): int {
    switch (s) {
        case finding.Severity.Critical: { return 40 }
        default: { return 0 }
    }
}

let worst: finding.Severity = finding.Severity.Critical
let all: finding.Severity[] = [finding.Severity.Low, finding.Severity.High]
```

A struct may hold one as a field, including a struct declared in the module
that declares the enum.

---

## Interfaces

Interfaces define a contract — a set of method signatures that a type must have. DexLang uses **structural typing**: a struct satisfies an interface automatically if it has all the required methods with matching signatures. There is no `implements` keyword.

### Interface definition

```dex
interface Greeter {
    fn greet(): string
}
```

An interface lists method signatures only — no implementations, no fields.

### Satisfying an interface

A struct satisfies an interface if it defines methods that match every signature in the interface. The matching is checked at compile time.

```dex
interface Greeter {
    fn greet(): string
}

struct Person {
    name: string

    fn greet(): string {
        return "Hello, I'm " + name
    }
}
```

`Person` satisfies `Greeter` because it has a `greet()` method that returns `string`. No explicit declaration is needed.

### Using interfaces as types

An interface type can be a function parameter, a local, a return type, a struct
field, or an array element. Start with the parameter, which is the common case. Any struct that satisfies the interface can be passed:

```dex
fn printGreeting(g: Greeter): void {
    fmt.println(g.greet())
}

let p = Person{name: "Alice"}
printGreeting(p)    // "Hello, I'm Alice"
```

It can also be a local, bound to a struct that is already named:

```dex
let p = Person{name: "Alice"}
let g: Greeter = p
fmt.println(g.greet())    // "Hello, I'm Alice"
```

Assigning a different concrete type to the same local is fine — the vtable is
rebuilt with it:

```dex
let who: Greeter = p
who = r    // a Robot, also a Greeter
```

### Multiple methods

Interfaces can require multiple methods. All must be present with matching parameter and return types:

```dex
interface Shape {
    fn area(): double
    fn perimeter(): double
}

struct Circle {
    radius: double

    fn area(): double {
        return math.pi() * radius * radius
    }

    fn perimeter(): double {
        return 2.0 * math.pi() * radius
    }
}
```

`Circle` satisfies `Shape` because it has both `area(): double` and `perimeter(): double`.

### Compile-time checking

If a struct is missing a required method or the signature doesn't match, the compiler reports an error:

```dex
interface Stringer {
    fn toString(): string
}

struct Empty {
    x: int
}

fn show(s: Stringer): void {}

fn main(): void {
    let e = Empty{x: 1}
    show(e)
}
```

```
argument 1 of 'show': expected Stringer, got Empty (missing fn toString(): string)
```

The error names what is absent rather than only that the types differ. A method
present under the right name but the wrong signature is reported as a mismatch
instead, which is the case a bare "missing" would mislead on.

### An interface value owns its data

An interface value is a refcounted box holding a **copy** of the struct it was
built from, plus that struct's method table. Because it owns the copy rather
than pointing at someone else's struct, it can go anywhere a value can go:

```dex
fn greeterFor(robot: bool): Greeter {   // returned
    let p = Person{name: "Eve"}
    return p
}

struct Entry {
    key: string
    who: Greeter                        // stored in a field
}

let crowd: Greeter[] = [p, r]           // held in an array, mixed types
assert(crowd[0].greet() == "Hi Ada")    // dispatched through an element
```

Coercion costs one pooled allocation, and the struct's heap fields are retained
rather than copied, so a string in the original and a string in the box are the
same bytes with one more reference.

The consequence of copying is that **mutation through an interface does not
reach the original**:

```dex
let t = Tally{n: 0}
let counts: Counter = t
assert(counts.bump(3) == 3)
assert(counts.total() == 3)
assert(t.n == 0)        // the box was mutated, not t
```

Go behaves the same way for the same reason. To mutate the original, call the
method on the struct directly rather than through an interface.

### Where an interface still does not fit

Two containers refuse one, each for a reason of its own:

```dex
fn each(m: map[string, Greeter]): void {}   // compile error
fn maybe(g: Greeter?): void {}              // compile error
```

```
a map value must be int, bool, string, long, double or char, not the interface
Greeter

cannot make the interface Greeter optional: an interface value is a vtable pair
rather than a pointer, so it has no null. Hold the concrete struct type, or an
array
```

A map holds only the six scalar value types, interface or not — a struct value
is refused the same way. An optional needs a spelling for absent, and an
interface value is a vtable pair rather than a pointer, so it has none; an array
is the way to hold a varying number of them.

### How it works

Interfaces compile to C vtable structs. An interface value holds a pointer to the copy it owns (`_data`), the refcounted box that copy lives in (`_obj`), and a table of function pointers (`_vtable`). The compiler fills the table from the concrete struct type, which it knows at every coercion site, so all dispatch is resolved at compile time: an interface call costs one indirect call and carries no type metadata and no lookup. The box is released when the value goes out of scope, is overwritten, or is dropped by an exception unwinding past its frame.

Struct *fields* can be read by position at runtime through the [`reflect`](#reflect) module, but that is a separate, opt-in mechanism — the compiler emits a name table and typed accessors for the structs some `reflect` call names, and nothing for the rest. Interface dispatch does not use it.

### Why no `instanceof` or `typeof`

DexLang is statically typed. The compiler knows every variable's type at compile time, so there is no runtime type ambiguity to resolve. Languages that need `instanceof` typically have class inheritance hierarchies (Java, C#) where a parent-typed variable could hold different child types at runtime, or dynamic typing (JavaScript, Python) where types aren't known until execution. DexLang has neither — interfaces use structural typing resolved entirely at compile time.

---

## Maps

Maps are hash tables with typed keys and values. They are heap-allocated with reference counting.

### Supported key types

- `string`
- `int`

### Supported value types

- `int`, `bool`, `string`, `long`, `double`, `char`

### Map creation

```dex
let m: map[string, int] = {}
let m2: map[int, string] = {}
```

### Method API

```dex
m.set("alice", 100)
let v: int = m.get("alice")
let exists: bool = m.has("alice")
m.remove("alice")
let n: int = m.len()
m.clear()              // remove all entries
let k: string[] = m.keys()
let vals: int[] = m.values()
```

### Bracket syntax

```dex
m["bob"] = 200
let v2: int = m["bob"]
```

### Map methods

| Method         | Description                     | Returns      |
|----------------|---------------------------------|--------------|
| `.set(key, value)` | Set a key-value pair       | `void`       |
| `.get(key)`    | Get value by key (zero if missing) | value type |
| `.has(key)`    | Check if key exists             | `bool`       |
| `.remove(key)` | Remove a key-value pair         | `void`       |
| `.len()`       | Get number of entries           | `int`        |
| `.keys()`      | Get array of all keys           | `key[]`      |
| `.values()`    | Get array of all values         | `value[]`    |

---

## Arrays

### Array literals

```dex
let nums: int[] = [1, 2, 3]
let names: string[] = ["alice", "bob"]
let flags: bool[] = [true, false, true]
let empty: int[] = []
```

### Indexing (read)

```dex
let first: int = nums[0]
```

### Index assignment (write)

```dex
nums[0] = 99
```

The index must be `int`. The assigned value must match the array's element type.

### Slicing

Use `arr[start:end]` to create a new array from a sub-range. Start is inclusive, end is exclusive. Either bound can be omitted: `arr[:end]` starts from 0, `arr[start:]` goes to the end.

```dex
let nums: int[] = [10, 20, 30, 40, 50]
let mid: int[] = nums[1:4]     // [20, 30, 40]
let head: int[] = nums[:3]     // [10, 20, 30]
let tail: int[] = nums[3:]     // [40, 50]
```

The result is always a new array. Start and end are clamped to valid bounds.

### Array methods

| Method               | Description                                  | Returns |
|----------------------|----------------------------------------------|---------|
| `.push(value)`       | Append an element to the array               | `void`  |
| `.pop()`             | Remove and return the last element            | element type |
| `.len()`             | Get the number of elements                    | `int`   |
| `.remove(index)`     | Remove the element at the given index         | `void`  |
| `.contains(val)`     | Check if the value is in the array            | `bool`  |
| `.indexOf(val)`      | Return the index of the value, or -1          | `int`   |
| `.reverse()`         | Reverse the array in place                    | `void`  |
| `.sort(direction)`   | Sort the array (`"asc"` or `"desc"`)          | `void`  |

```dex
let items: int[] = []
items.push(10)
items.push(20)
items.push(30)
let count: int = items.len()       // 3
let last: int = items.pop()        // 30
let has: bool = items.contains(10) // true
let idx: int = items.indexOf(20)   // 1
items.remove(0)                    // removes 10
items.sort("asc")                  // sort ascending
items.reverse()                    // reverse in place
```

> **Note:** `.sort()` is supported on primitive-typed arrays (`int[]`, `long[]`, `double[]`, `string[]`, `char[]`). Struct arrays do not support sorting.

### String interpolation

A `${...}` inside a string literal substitutes the value of the expression
inside it:

```dex
let n = 7
let name = "dex"
let s = "n=${n} name=${name} sum=${n + 1}"   // "n=7 name=dex sum=8"
fmt.println("inline ${n}")
```

- Any expression goes inside, not only a name: `${n + 1}`, `${arr[0]}`,
  `${describe(n)}`. A call inside an interpolation runs exactly once.
- Every primitive converts without a cast — `int`, `long`, `double`, `bool`,
  `char` and `string`. A `double` renders the way `%g` does, so `1.5` is
  `"1.5"`.
- Any other type goes through the same conversion `as string` uses, so a struct
  renders as `Point{x: 1, y: 2}`.
- The parts are appended to a `StringBuilder` and built once, so a literal with
  several interpolations costs one allocation for the result rather than one per
  concatenation.

Interpolation is a string *literal* feature. A [tag](#tags) is raw and does not
interpolate.

### String methods

String variables have built-in methods for common text operations.

| Method                     | Description                                  | Returns    |
|----------------------------|----------------------------------------------|------------|
| `.len()`                   | Get the length of the string                 | `int`      |
| `.contains(sub)`           | Check if string contains a substring         | `bool`     |
| `.startsWith(prefix)`      | Check if string starts with prefix           | `bool`     |
| `.endsWith(suffix)`        | Check if string ends with suffix             | `bool`     |
| `.indexOf(sub)`            | Find index of substring (-1 if not found)    | `int`      |
| `.toLower()`               | Return lowercase copy                        | `string`   |
| `.toUpper()`               | Return uppercase copy                        | `string`   |
| `.trim()`                  | Remove leading/trailing whitespace           | `string`   |
| `.split(delimiter)`        | Split into array by delimiter                | `string[]` |
| `.substring(start, end)`   | Extract substring (start inclusive, end exclusive) | `string`   |
| `.replace(old, new)`       | Replace all occurrences                      | `string`   |
| `.charAt(index)`           | Get character at index                       | `char`     |
| `.isAlphanumeric()`        | Check if all characters are alphanumeric (non-empty) | `bool` |
| `.isAlpha()`               | Check if all characters are alphabetic (non-empty)   | `bool` |
| `.isDigit()`               | Check if all characters are digits (non-empty)       | `bool` |
| `.isNumeric()`             | Check if all characters are numeric or `.` (non-empty) | `bool` |
| `.isWhitespace()`          | Check if all characters are whitespace (non-empty)   | `bool` |
| `.isEmpty()`               | Check if the string is empty (length 0)              | `bool` |
| `.containsUppercase()`     | Check if the string contains an uppercase letter     | `bool` |
| `.containsLowercase()`     | Check if the string contains a lowercase letter      | `bool` |
| `.containsDigit()`         | Check if the string contains a digit                 | `bool` |

```dex
let s: string = "Hello, World"
let n: int = s.len()              // 12
let b: bool = s.contains("World") // true
let lo: string = s.toLower()      // "hello, world"
let parts: string[] = s.split(",")// ["Hello", " World"]
let sub: string = s.substring(0, 5) // "Hello"
let r: string = s.replace("World", "Dex") // "Hello, Dex"
let c: char = s.charAt(0)         // 'H'
```

#### String validation

```dex
let input: string = "Hello123"
let alnum: bool = input.isAlphanumeric() // true
let alpha: bool = input.isAlpha()        // false (contains digits)
let digit: bool = "42".isDigit()         // true
let empty: bool = "".isEmpty()           // true
let hasUp: bool = input.containsUppercase() // true
let hasLo: bool = input.containsLowercase() // true
let hasDg: bool = input.containsDigit()    // true
```

### StringBuilder

`StringBuilder` is a built-in type for efficiently building strings via a growable buffer. It avoids the O(n²) cost of repeated string concatenation in loops. No import is needed.

```dex
let sb = StringBuilder()
sb.append("hello")
sb.append(" ")
sb.append(42)
sb.append(" pi=")
sb.append(3.14)
let result: string = sb.toString() // "hello 42 pi=3.14"
```

The `append()` method is polymorphic and accepts `string`, `int`, `long`, `double`, `bool`, and `char` values.

| Method        | Description                              | Returns  |
|---------------|------------------------------------------|----------|
| `.append(v)`  | Append a value (string/int/long/double/bool/char) | `void`   |
| `.toString()` | Build the final string                   | `string` |
| `.len()`      | Current length of the buffer             | `int`    |
| `.clear()`    | Reset the buffer to empty                | `void`   |

`StringBuilder` is refcounted and automatically released at scope exit.

---

## Functions

Declared with `fn` or `function`. Parameters and return type require type annotations.

```dex
fn add(a: int, b: int): int {
  return a + b
}
```

### Calling functions

```dex
let result: int = add(5, 10)
```

### Module-qualified calls

```dex
fmt.println(42)
let doc: json.Value = json.decode(text)
```

### Default parameters

A parameter can declare a default with `= value`. A call may leave it out, and
the default is used in its place:

```dex
fn page(from: string, to: string, limit: int = 25, offset: int = 0): Row[] { ... }

page("2026-01-01", "2026-01-31")
page("2026-01-01", "2026-01-31", 10)
```

The first call pages with a limit of 25, the second with 10; both start at
offset 0.

A parameter with no default must always be passed.

### Named arguments

An argument can name the parameter it is for. Named arguments can be given in
any order, so a call reads as what it passes rather than where each value
sits:

```dex
page(from: start, to: end, limit: 10, offset: 0)
page(start, end, offset: 50)
sessionStore.list(db, filter, limit: 10, offset: 0)
p.page(number: 3, size: 20)
```

`page(start, end, offset: 50)` names only the offset, so `limit` keeps its
default.

Positional arguments come first and fill parameters from the left; named ones
fill the rest, and anything still unfilled takes its default. Passing a value
that is just `0`, `""` or `false` is where a name earns its place — `list(db, f,
10, 0)` gives no hint which number is the limit.

The compiler rejects a name that matches no parameter, one given twice, a
positional argument after a named one, and a required parameter left unfilled.

Named arguments work on any function or method declared in Dex — in the same
file, in an imported module, or on a struct. A standard library function, a C
function from an `external module`, a struct constructor, and a call through a
function value have no parameter names to match, so naming an argument there is
an error rather than being ignored.

### Method chaining

A call is not the end of an expression. Its result can be indexed, have a field
read, or have a method called on it, so a chain keeps going for as long as each
link has something to offer:

```dex
let n: int = sb.toString().len()
let short: int = str.fromInt(42).trim().len()
let count: int = csv.split(",").len()
let third: string = csv.split(",")[2]
let size: int = csv.split(",")[2].len()
let keys: int = settings.keys().len()
let shouted: string = "  mixed  ".trim().toUpper()
```

A string, array, map or `StringBuilder` is a receiver whether it is a named
variable, a literal, or the result of the call before it.

Each link's receiver is **evaluated once** and released if that link minted it.
`csv.split(",")[2].len()` splits once, not twice, and the array the split
produced is freed at the end of the statement even though only one of its
elements was wanted. A receiver that is simply a variable is used directly, with
no temporary, so chaining costs nothing where it is not used.

A chain of your own making works the same way — see
[Fluent builders](#fluent-builders).

---

### Fluent builders

A method receives the instance it was called on as a **pointer**, named `self`.
Assigning to one of its fields changes that instance, and returning `self` lets
the next call in a chain continue from it:

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

    fn text(): string {
        return "SELECT * FROM " + self.from
    }
}

let q: Query = Query { from: "", max: 0 }
q.table("users").limit(25)
// q.from is "users", q.max is 25
```

One instance is threaded through the whole chain; nothing is copied per step.
`&Query` is the return type that says so — the same `&T` that a mutable
parameter uses.

A chain may start from a temporary. The value is bound to a statement-scoped
variable for the length of the statement, so each link mutates the same thing
and its heap fields are released when the statement ends:

```dex
fn insertInto(name: string): Stmt {
    return Stmt { table: name, cols: "" }
}

let sql: string = insertInto("users").col("id").col("email").text()
```

The result of a chain can also be bound as a reference:

```dex
let r: &Counter = c.bump()
```

#### self

`self` names the receiver inside any method. It is readable and assignable:

```dex
fn col(name: string): &Stmt {
    if (self.cols.isEmpty()) {
        self.cols = name
    } else {
        self.cols = self.cols + ", " + name
    }
    return self
}
```

Bare field names still read through the receiver, so `cols` and `self.cols` mean
the same thing when read. **Assignment requires `self`** — a bare `cols = name`
is an undefined variable, because a method's locals and its fields are separate
namespaces.

Assigning a heap field through `self` follows the usual rule: the string being
replaced is released and the new one retained, so reassigning in a loop does not
grow.

`self` is **reserved**. It cannot be declared as a parameter, variable, constant,
field, loop variable, catch binding, function, struct, enum, interface or member
name:

```dex
fn shadow(self: int): int { ... }   // error: 'self' is reserved
let self: int = 1                   // error: 'self' is reserved
```

It is not a keyword, though, and that is deliberate — it has to remain an
ordinary expression for `self.cols` and `return self` to mean anything. So it is
refused where a name is *introduced*, and nowhere else.

#### self is only a method's

A module has no receiver, so `self` means nothing there. Module-level state is
reached by its own name:

```dex
let total: int = 0

fn bump(): int {
    total = total + 1    // not self.total
    return total
}
```

Note that this is the mirror of the rule inside a method: at module level a bare
*assignment* is the only form, because there is nothing to assign through.
Writing `self.total` there is an error that says so rather than one about an
undefined variable.

#### Methods on a reference

Because the receiver is a pointer, a method can be called on a `&T` just as on a
value, and a method reached from inside another method — `self.reg.attach(..)` —
threads the reference through without a copy.

A **method value** is still a copy. Taking `q.get` captures the receiver as it
was, so a mutating method reached that way changes the capture and not the
original:

```dex
let f: fn(): int = q.get   // copies q
```

#### A field and a method may share a name

`q.table` reads the field and `q.table(..)` calls the method, so the two do not
collide. The method value of a method shadowed this way is not reachable, since
`q.table` resolves to the field.

---

### Function values

A function can be held in a variable, passed to another function, or returned
from one. The type is written `fn(paramTypes): returnType`:

```dex
fn twice(x: int): int { return x * 2 }

fn apply(f: fn(int): int, x: int): int {
    return f(x)
}

let f: fn(int): int = twice
apply(twice, 4)                        // 8
```

A function value carries the state it was built with, so three things can fill
one: a plain function, a lambda, and a method bound to its receiver.

**Lambdas** are written like a function without a name, and may close over the
variables around them:

```dex
let base: int = 10
let addBase: fn(int): int = fn(x: int): int { return x + base }
addBase(5)                             // 15
```

A closure keeps whatever it captured alive, so it stays valid after the scope it
was made in has gone:

```dex
fn makeAdder(n: int): fn(int): int {
    return fn(x: int): int { return x + n }
}

let add7: fn(int): int = makeAdder(7)
add7(1)                                // 8
```

A module-level variable is not captured. A closure reaches it where it lives,
as any function does, so it sees writes made after it was built, and
`defer mu.unlock()` on a module-level mutex unlocks that mutex rather than a
copy of it. A local of the same name shadows the global and is captured as
usual.

**Method values** — naming a method without calling it gives that method bound to
the object it was read from. The receiver travels inside the value:

```dex
struct Counter {
    step: int

    fn bump(x: int): int { return x + step }
}

let c: Counter = Counter { step: 5 }
let bump: fn(int): int = c.bump        // bound to c
bump(10)                               // 15
apply(c.bump, 1)                       // 6
```

The receiver is copied into the value, so it is unaffected by later changes to
the variable it came from. Inside a method, a sibling method named without a
receiver refers to one on the same object.

This is what lets a handler carry its dependencies rather than reaching for
them. A struct holds what it needs, and its methods are registered directly:

```dex
struct Api {
    greeting: string

    fn hello(): string { return greeting }
}

let api: Api = Api { greeting: "hi" }
http.route("GET", "/hello", api.hello)
```

## Generics

A generic declaration takes type parameters in angle brackets. It is a
**template**, not code: each distinct set of type arguments produces one
concrete function or struct, named after them. There is no boxing, no type
metadata, and no runtime dispatch — an instantiation is indistinguishable from
the same code written out by hand.

### Generic functions

```dex
fn identity<T>(x: T): T {
    return x
}

fn pick<T>(a: T, b: T, first: bool): T {
    if (first) {
        return a
    }
    return b
}
```

Type arguments are **inferred from the call**, so a generic function is called
like any other:

```dex
identity(42)        // T = int
identity("hi")      // T = string
pick(1, 2, true)    // T = int
```

Several parameters may share a type parameter, and they have to agree:

```dex
pick(1, "a", true)
```

```
pick() binds T to int from an earlier argument, but argument 2 is string
```

Type parameters are independent of one another:

```dex
fn firstOfTwo<A, B>(a: A, b: B): A {
    return a
}

firstOfTwo(7, "ignored")    // A = int, B = string
```

Inference descends into a container, so the element type pins the parameter:

```dex
fn firstElem<T>(xs: T[]): T {
    return xs[0]
}

let nums: int[] = [10, 20]
firstElem(nums)    // T = int
```

A type parameter that appears in no parameter's type has nothing to be inferred
from, and the compiler says so rather than guessing:

```dex
fn makeDefault<T>(n: int): int { return n }
```

```
cannot infer type parameter T of makeDefault() from the arguments; it appears in
no parameter's type
```

A generic may call a generic, including itself:

```dex
fn twice<T>(x: T): T {
    return identity(identity(x))
}
```

### Generic structs

```dex
struct Box<T> {
    value: T

    fn get(): T {
        return value
    }

    fn replace(v: T): T {
        self.value = v
        return self.value
    }
}

struct Pair<K, V> {
    key: K
    value: V
}
```

A struct's type arguments are **written, not inferred** — a struct type has no
arguments to infer them from. Both the type and the literal name them:

```dex
let bi: Box<int> = Box<int> { value: 42 }
assert(bi.get() == 42)

let bs: Box<string> = Box<string> { value: "held" }
let pr: Pair<string, int> = Pair<string, int> { key: "a", value: 2 }
```

Two mentions of `Box<int>` are the **same type**, not two — the instantiation is
built once and reused.

Leaving the type arguments off names a template where a type is expected:

```dex
let b: Box = Box { value: 1 }
```

```
generic struct Box needs type arguments, as Box<...>
```

An instantiation is an ordinary struct, so everything that applies to one
applies to it: a heap-typed field is owned and released the same way, methods
work the same way, and it can be returned, nested, and held in an array.

### Declaration order

A generic struct must be declared **above** the code that instantiates it, the
same rule every type in DexLang follows.

### What is not supported yet

- **A user module cannot declare a generic.** The template lives in the parser
  that read it, and instantiating one needs that parser; a generic struct is
  harder still, because its instantiation happens while a file is parsed and a
  module is read after the main program already was. Declaring one is a clear
  error rather than a silent omission:

  ```
  helper.dx: 'gid' is generic, and a user module cannot declare a generic yet —
  move it into the main program
  ```

- **No type parameter bounds.** `fn show<T: Stringer>(x: T)` does not parse. A
  generic body may only do what every type supports, so an operation the type
  argument does not have is reported against the instantiation.
- **A generic struct cannot appear in a generic signature.** `fn unwrap<T>(b:
  Box<T>): T` is not supported: inference would have to match a type argument
  structurally through an instantiation.

### How it works

Generics are monomorphised by **re-parsing**. A generic declaration is captured
as the tokens it was written as and skipped rather than parsed; instantiating it
re-parses those tokens with its type parameters bound to concrete types and its
own name mangled to `Box_int` or `max_int`. What comes out is an ordinary
function or struct, which is why the code generator needs no knowledge of
generics at all — `Dex_Box_int` falls out of the existing struct path.

A struct's instantiation happens in the parser, where the type arguments are
written. A function's happens in the type checker, because inferring them needs
the argument types and only the checker has those; the checker asks the parser
to re-parse, registers the result, and appends it to the program, where the
body-checking pass reaches it like any other function.

---

## Operators

### Arithmetic (operands must be matching numeric type: `int`, `long`, or `double`)

| Operator | Description    |
|----------|----------------|
| `+`      | Addition        |
| `-`      | Subtraction     |
| `*`      | Multiplication  |
| `/`      | Division        |
| `%`      | Modulo (`int` and `long` only) |
| `++`     | Increment (statement only) |
| `--`     | Decrement (statement only) |
| `+=`     | Add and assign  |
| `-=`     | Subtract and assign |
| `*=`     | Multiply and assign |
| `/=`     | Divide and assign |
| `%=`     | Modulo and assign |

`+` also works for string concatenation when both operands are `string`.

#### Integer overflow and division

`int` and `long` arithmetic wraps on overflow, in two's complement. Dividing by
zero, with `/`, `%`, `/=` or `%=`, stops the program with
`runtime error: division by zero`, for `double` as well as the integer types.
The most negative value divided by `-1` is itself, and its remainder is `0`,
the same as in Go: on x86 that division faults in C, and on arm64 it quietly
produces the same answer, so Dex defines it rather than inheriting either.

```dex
let min: int = -2147483647 - 1
assert(min / -1 == min)
assert(min % -1 == 0)
```

#### Operands must already agree

Arithmetic never converts a number for you. Both operands must be the same
type, and the one exception is a **literal**, which adapts to the other side:

```dex
let d: double = 2.5
let ok: double = d * 2            // 2 adapts to double

let n: int = 3
let bad: double = n * d           // error
let good: double = num.toDouble(n) * d
```

```
cannot apply '*' to int and double: a number never widens on its own —
convert the left operand with num.toDouble(...)
```

A literal adapts only where the conversion is exact, so an `int` literal
reaches `long` and `double` but a `double` literal never reaches `int`:
`n + 2.0` is an error, not a silent truncation. `char` does not take part at
all — `num.toInt(c)` is how a character becomes a number.

This holds for `%` and for the compound forms, so `total += n` is refused for
the same reason `total = total + n` is.

The rule is Rust's, and the reason is that widening is not as safe as it looks:
`long` into `double` loses precision past 2^53, so the conversion that reads as
harmless is the one that quietly corrupts data. Requiring `num.*` costs one call
and keeps every conversion visible where it happens. Comparison is deliberately
looser — see below — because it yields a `bool` and cannot carry a wrong value
forward.

#### String coercion

When one operand of `+` is a `string` and the other is a non-string type, the non-string operand is automatically converted to its string representation. This works for all primitive types (`int`, `long`, `double`, `bool`, `char`), structs, enums, and arrays.

```dex
let n: int = 42
let msg: string = "value: " + n   // "value: 42"

let pi: double = 3.14
let s: string = "pi is " + pi     // "pi is 3.14"

let flag: bool = true
let t: string = "flag: " + flag   // "flag: true"
```

The non-string operand can appear on either side:

```dex
let x: int = 10
let s: string = x + " items"      // "10 items"
```

Structs are converted using the format `Name{field: value, ...}`:

```dex
struct Point { x: int  y: int }
let p = Point { x: 1, y: 2 }
let s: string = "pos: " + p       // "pos: Point{x: 1, y: 2}"
```

Coercion chains work naturally with multiple operands:

```dex
let s: string = "a" + 1 + "b"     // "a1b"
```

### Comparison

| Operator | Description           | Operand types                |
|----------|-----------------------|------------------------------|
| `==`     | Equal (relaxed)       | Same type, or cross-numeric (`int`/`long`/`double`) |
| `!=`     | Not equal (relaxed)   | Same type, or cross-numeric (`int`/`long`/`double`) |
| `===`    | Strict equal          | Operands must be the exact same type |
| `!==`    | Strict not equal      | Operands must be the exact same type |
| `<`      | Less than             | `int`, `long`, `double`      |
| `>`      | Greater than          | `int`, `long`, `double`      |
| `<=`     | Less than or equal    | `int`, `long`, `double`      |
| `>=`     | Greater than or equal | `int`, `long`, `double`      |

`==`/`!=` allow comparing different numeric types. The narrower type is implicitly widened (`int` → `long` → `double`). `===`/`!==` require both operands to be the exact same type.

### Logical (operands must be `bool`)

| Operator | Description |
|----------|-------------|
| `&&`     | Logical AND |
| `\|\|`   | Logical OR  |

### Unary

| Operator | Description        | Operand type             |
|----------|--------------------|--------------------------|
| `-`      | Negation           | `int`, `long`, `double`  |
| `!`      | Logical NOT        | `bool`                   |

### Precedence (low to high)

1. `||`
2. `&&`
3. `==`, `!=`, `===`, `!==`
4. `<`, `>`, `<=`, `>=`
5. `+`, `-`
6. `*`, `/`, `%`
7. Unary `-`, `!`

---

## Control Flow

### if / else

```dex
if (condition) {
  // ...
} else {
  // ...
}
```

`else if` chains are supported. The condition must be `bool` and enclosed in parentheses.

### while

```dex
while (condition) {
  // ...
}
```

The condition must be `bool` and enclosed in parentheses.

### for

C-style for loop with init, condition, and post statements:

```dex
for(let i: int = 0; i < 10; i++) {
  fmt.println(i)
}
```

The init statement can be a `let` declaration (with or without type inference) or an assignment. The post statement supports `++`, `--`, `+=`, `-=`, `*=`, `/=`, `%=`, or `=`.

```dex
for(let i = 0; i < 100; i += 2) {
  // iterate by 2
}
```

### foreach

Iterate over array elements:

```dex
let nums: int[] = [10, 20, 30]

// Value only
foreach(nums as n) {
  fmt.println(n)
}

// Index and value
foreach(nums as i, n) {
  fmt.println(i)  // 0, 1, 2
  fmt.println(n)  // 10, 20, 30
}
```

The iterable must be an array type. The index variable (if used) is always `int`. The value variable takes the element type of the array.

### break and continue

Exit a loop early with `break`, or skip to the next iteration with `continue`:

```dex
while (true) {
  if (done) { break }
}

for(let i = 0; i < 10; i++) {
  if (i % 2 == 0) { continue }
  fmt.println(i)  // prints odd numbers only
}
```

`break` and `continue` work inside `while`, `for`, and `foreach` loops. Using them outside a loop is a compile error.

### switch

Multi-way branching on a value. Supports `int`, `string`, `char`, `long`, `double`, and `bool` tag types. Cases use colon syntax with brace-delimited bodies. Multiple values per case are separated by commas.

```dex
switch (action) {
    case "BootNotification": {
        fmt.println("Boot!")
    }
    case "Heartbeat", "StatusNotification": {
        fmt.println("Alive")
    }
    default: {
        fmt.println("Unknown")
    }
}
```

Integer switch:

```dex
switch (code) {
    case 200: {
        fmt.println("OK")
    }
    case 404: {
        fmt.println("Not Found")
    }
    default: {
        fmt.println("Other")
    }
}
```

The `default` branch is optional. Each case body has its own scope. There is no fallthrough — only the matched case executes.

---

## Concurrency

DexLang provides lightweight concurrency with four primitives: `spawn`, `send`, `receive`, and `channel`, plus `mutex` for guarding state that threads share.

### spawn

Launch a concurrent task. Returns a task handle that can be used with `receive` to get results.

**Block form:**

```dex
let task = spawn {
    let result = expensive_computation()
    send(result)
}
let val = receive(task)
```

**Function call form:**

```dex
fn compute(n: int): int {
    return n * 2
}

let task = spawn compute(21)
let val = receive(task)  // 42
```

**Fire-and-forget:**

```dex
spawn { do_background_work() }
```

### send

Send a value from inside a spawn block.

```dex
send(value)            // send to the task's implicit handle (1-to-1)
send(channel, value)   // send to a shared channel (many-to-1)
```

### receive

Block until a value is available, then return it.

```dex
let val = receive(task)      // receive from a task handle
let val = receive(channel)   // receive from a channel
```

### channel

Create a shared channel for many-to-one communication. Takes a type argument.

```dex
let ch = channel(int)
```

### close

Close a channel. Subsequent sends are ignored; blocked receivers get zero values.

```dex
close(ch)
```

### Patterns

**1-to-1 (task sends result to parent):**

```dex
let task = spawn {
    send(42)
}
assert(receive(task) == 42)
```

**Many-to-one (shared channel):**

```dex
let ch = channel(int)
for (let i = 0; i < 10; i++) {
    spawn { send(ch, i * 10) }
}
let total = 0
for (let i = 0; i < 10; i++) {
    total += receive(ch)
}
```

### Channel type annotations

Use `chan` followed by an element type for explicit type annotations:

```dex
let ch: chan int = channel(int)
```

### mutex

Channels pass values *between* threads. When threads instead need to share the
same data, guard it with a `mutex`. Write `mutex` in value position to create
one, already unlocked:

```dex
let mu: mutex = mutex

mu.lock()
// critical section — one thread at a time
mu.unlock()
```

**Shared state needs a lock.** Module-level variables are shared by every
thread, and maps and arrays are not internally synchronised — two threads
writing one at the same time can corrupt it, not merely lose an update. A `map`
that resizes while another thread is reading it will follow freed memory.

```dex
let mu: mutex = mutex
let totals: map[int, int] = {}

fn record(key: int, amount: int): void {
    mu.lock()
    totals.set(key, amount)
    mu.unlock()
}

fn lookup(key: int): int {
    mu.lock()
    let found: int = -1
    if (totals.has(key)) {
        found = totals.get(key)
    }
    mu.unlock()          // unlock before returning, not after
    return found
}
```

Note the shape of `lookup`: read what you need into a local, unlock, *then*
return. Returning from inside the critical section would skip the `unlock` and
deadlock every other thread.

A mutex can also live in a struct, which keeps the lock next to the data it
protects — useful when you pass state to threads by reference rather than
reaching for a module-level variable:

```dex
struct Counter {
    mu: mutex
    value: int
}

fn increment(c: &Counter): void {
    c.mu.lock()
    c.value = c.value + 1
    c.mu.unlock()
}
```

**Use `defer` when the critical section can throw.** A bare `unlock()` is
skipped if an exception unwinds past it, leaving the mutex held forever and
every other thread blocked. `defer` runs on every way out of the function,
including an exception raised by something it called:

```dex
fn increment(c: &Counter): void {
    c.mu.lock()
    defer c.mu.unlock()   // runs however the function exits
    riskyOperation()      // safe: the unlock still happens if this throws
    c.value = c.value + 1
}
```

`try`/`finally` does the same job when the cleanup belongs to a block rather
than to the whole function.

Hold a lock for as little as possible, and avoid doing I/O — a database call, a
socket write — while holding one, since every other thread waits on you. Take
the value you need under the lock, release it, then do the slow work.

---

## Error Handling

DexLang supports structured exception handling with `try`, `catch`, `finally`, and `throw`.

### Exception Type

`Exception` is a built-in struct type, always available without imports:

```dex
// Built-in definition:
// struct Exception {
//     message: string
// }
```

### Throwing Exceptions

Use `throw` to raise an exception:

```dex
throw Exception("something went wrong")
```

If an exception is not caught, the program prints the message to stderr and exits with code 1.

### Throwable Types

`Exception` is the built-in exception type, but any struct can be thrown when it
carries a `message: string` and nothing that would need releasing:

```dex
struct ParseError(message: string, line: int) {}   // throwable
struct IOError(message: string) {}                 // throwable
struct Plain(code: int) {}                         // no message field
struct TwoStrings(message: string, detail: string) {}  // detail cannot survive
```

The restriction is the unwind. A throw is a `longjmp`, so the frames between it
and the catch never run their releases: the message is copied into the runtime
before the jump, and the remaining fields travel as a flat byte copy. An `int`,
`bool`, `double` or `char` survives that; a second string, an array or a nested
struct holding either could only arrive already freed, so the compiler refuses
it rather than hand you a dangling one.

Declaring the struct with constructor parameters — `struct ParseError(message:
string, line: int) {}` — is what makes `ParseError("bad digit", 12)` callable.
With the fields written in the body instead, throw a struct literal:
`throw ParseError { message: "bad digit", line: 12 }`.

### Declaring That a Function Throws

A `throws` after the return type says an exception may escape the function, so a
caller can see it in the signature rather than discover it at runtime. Naming
types after it says which:

```dex
fn load(path: string): int throws ParseError, IOError {
    if (path == "") {
        throw IOError("no path")
    }
    throw ParseError("bad digit", 12)
}

fn anything(): int throws {        // something escapes, unspecified
    throw Exception("whatever")
}
```

The declaration is checked against what the function actually does, by
[lint](#linting) rather than by the compiler: the program runs either way.

- An exception escaping a function that does not declare it, or reaching a call
  site with no matching `catch`, is reported as `uncaught-throw`.
- A type that escapes a function whose `throws` list leaves it out is reported
  as `undeclared-throw`.
- A `throws` naming a type the function cannot throw is reported as
  `unnecessary-throws`.
- Declaring `throws` is a contract. Callers are told the function may throw even
  if its body does not throw today, which is what lets it start throwing later
  without surprising them.
- A bare `throws` promises nothing specific, so only a catch-all can be sure of
  handling it.

Throwing is inferred transitively: a function that calls a throwing function
without catching can throw too. Two things stop that inference, because the
exception does not escape where it is written:

- a `throw` inside a lambda, which runs when the lambda is invoked, wherever
  that is;
- a `throw` inside a `spawn`, which unwinds on another thread.

A call through a function value stops it from the other side: the target is not
known statically, so it is assumed not to throw.

### Try-Catch-Finally

```dex
try {
    // code that may throw
    throw Exception("oops")
} catch (e: Exception) {
    // handle the exception
    fmt.println(e.message)
} finally {
    // always runs after try or catch
    fmt.println("cleanup")
}
```

- At least one of `catch` or `finally` is required.
- The `finally` block always executes, whether or not an exception was thrown.

### Catching by Type

A `try` may have any number of `catch` clauses. The first whose type matches the
thrown one runs:

```dex
try {
    let n = load(path)
    fmt.println("n=${n}")
} catch (e: ParseError) {
    fmt.println("line ${e.line}: ${e.message}")
} catch (e: IOError) {
    fmt.println("io: ${e.message}")
}
```

- A clause reads the fields of the type it names: `e.line` above is the value
  the throw carried.
- `catch (e: Exception)` is the **catch-all** — it handles every type, which is
  what a single catch clause always did. A clause after it can never run, and
  lint reports that as `unreachable-catch`.
- A catch-all handling some other type sees that type's `message` and zeroes
  for anything else: the fields of a type it does not name are not its to read.
- An exception **no clause matches carries on outward**, after this try's
  `finally` has run. Its message and fields are preserved as it goes.
- The heap values the skipped frames held are released when the exception is
  caught, and their deferred calls run, so throwing on an error path neither
  accumulates memory nor skips cleanup. What is still missed is narrow: the
  nested-struct and optional fields of a struct local.
- An exception that reaches the top of `main` unhandled prints
  `Unhandled <Type>: <message>` and exits with code 1.

### Deferred Calls

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

- The call is registered **when the `defer` statement runs**. One inside a
  branch that was not taken registers nothing; one inside a loop registers per
  iteration.
- Registered calls run at the function's exit, **most recent first** — a defer
  in a loop body runs after the statements that follow the loop, not at the end
  of each iteration.
- Whatever the call needs is **captured where the defer is written**, so it
  still works after the frame it came from is gone. That also means its
  arguments are evaluated at the `defer`, not at the exit.

### Try-Finally (Re-throw)

If a `try` block has only `finally` (no `catch`), the exception is re-thrown after the finally block runs:

```dex
try {
    throw Exception("error")
} finally {
    fmt.println("cleanup runs first")
}
// exception propagates here
```

### Exceptions From a Module

A module's exception types are named the way its other types are, qualified by
the module, in a `catch` and in a `throws` alike:

```dex
import "github.com/acme/sqlite"

fn load(): sqlite.Database throws sqlite.SqliteError {
    return sqlite.open("app.db")
}

try {
    load()
} catch (e: sqlite.SqliteError) {
    fmt.println(e.message)
}
```

### Cross-Function Exceptions

Exceptions propagate through function calls:

```dex
fn risky(): void {
    throw Exception("from risky")
}

fn main(): void {
    try {
        risky()
    } catch (e: Exception) {
        fmt.println(e.message)  // "from risky"
    }
}
```

---

## Comments

Single-line comments only:

```dex
// this is a comment
```

---

## Imports and Standard Library

```dex
import "module_name"
```

### User modules

A `.dx` file may be imported by path relative to the importing file. The module's
name is the file's base name, and its top-level functions, module-level values,
and types are reached through it:

```dex
import "service/users"        // module `users`
import "../wire/frame"        // module `frame`

let svc: users.Service = users.newService(db)
let text: string = frame.call(id, action, payload)
let kind: int = frame.CALL              // a module-level const
http.route("GET", "/items", users.list)      // a function as a value
```

What a file calls an import is local to that file. Each file is one module,
however many files share its base name: two libraries can each contain a
`util.dx` declaring the same functions, types and constants, and every file that
imports one of them reaches that one. Two imports **in one file** under the same
name are an error, because nothing in the file could say which one a reference
means — give one of them another name with `as`.

The same file imported by two names is one module, not two: its state is shared,
and `import "model/billing" as "bill"` in one file and `import "model/billing"`
in another both reach it.

### Aliasing an import

`as` gives an import a different module name, which is how two files with the
same base name are used together:

```dex
import "model/billing" as "bill"
import "service/billing" as "billing"

let e: bill.Entry = bill.Entry{}
let total: long = billing.total(e)
```

The alias is a **string**, not a bare identifier. It works in any file, not just
the program's entry point.

A name declared in the importing scope shadows a module of the same name, so a
local called `users` is a variable, not the module.

Declarations may appear in any order within a file — a `const` above the struct
it describes, a function above the type it returns.

A top-level function or variable may share a name with a C library function —
`read`, `write`, `open`, `time`. It is renamed in the generated C, so the name
is yours to use.

### Modules and dex.mod

A project that shares code with other projects has a `dex.mod` at its root. It
names the project, and lists the libraries it uses:

```
module github.com/acme/web

require github.com/acme/greet v0.1.0
```

Create one with `dex mod init github.com/acme/web` in the project's root
directory, and add a library with `dex get`:

```
dex mod init github.com/acme/web
dex get github.com/acme/greet
```

A project with no `dex.mod` keeps working exactly as before; it only
becomes necessary once an import names a module.

**The name is a URL.** `module github.com/acme/web` says what this project is
called, and it is where the code lives — the way Go names modules. Two people's
libraries cannot collide, because two people do not own the same URL.

**An import is either a path or a module.** An import whose first segment
looks like a host name — `github.com/...`, `example.org/...` — names a module.
Anything else is a file relative to the importing one, as it always was:

```dex
import "service/users"                      // service/users.dx beside this file
import "github.com/acme/web/service/users"  // the same file, from anywhere in the project
import "github.com/acme/greet"              // a library: greet.dx at its root
import "github.com/acme/greet/internal/shout"
```

The project's own module path reaches any file from any other, without `../`
chains. Importing a module path whole — `github.com/acme/greet` — reads the file
at that module's root named after its last segment, `greet.dx`, because an
import names a file and a module is a folder of them.

**`require` says which library, and which version.** `require
github.com/acme/greet v0.1.0` means this project uses that library at that
version. A version is a git tag the library's author pushed. `dex get` writes
the line; you can also write it by hand and run `dex mod download`.

**`replace` says where to read it from instead.** Normally a required library
is downloaded. `replace` says: don't download it, use this folder on my
machine. It is how you use a library that is
not published yet, or one you are changing alongside the program that uses it:

```
replace github.com/acme/greet => ../greet
```

means every `import "github.com/acme/greet..."` reads from the `greet` folder
next to this project. When the library is published, delete the line and the
same imports read the downloaded copy. A replaced module must still be required,
so that removing the `replace` leaves a complete `dex.mod`. The path is relative
to the `dex.mod`, written `./dir` or `../dir`.

**`dex` says which compiler it needs.** A library is released on its own
schedule, but it is written against some compiler, and a fix or feature it
relies on is missing from an older one:

```
module github.com/acme/greet

dex 0.9.14
```

means this module builds with dex 0.9.14 or later. The version is a release
without its `v`, and the line is optional. An import that reaches a module
needing a newer compiler stops there, before any of the module's files are
read:

```
main.dx:8:1: github.com/acme/greet needs dex 0.9.14 or later; this is dex 0.9.7
    upgrade with `dex update`, or `make install` in a compiler checkout
```

Without it, an old compiler fails *inside* the library, on code that is
correct, and the error reads as a bug in the library. The program's own
`dex.mod` is checked the same way. `dex mod init` writes the line for the
compiler running it, and `dex get` warns when it downloads a release that
needs a newer one.

A compiler's version ignores a `-dev` suffix, so `0.9.14-dev` satisfies
`dex 0.9.14`, and a compiler built with a plain `go build`, which reports
`dev`, is never refused.

#### Getting a library

```
dex get github.com/acme/greet              # the latest release
dex get github.com/acme/greet@v1.2.0       # that release
dex get github.com/acme/greet@latest       # move to the latest release
```

`dex get` finds the release, downloads it into the module cache, adds or updates
the `require` line, and records a checksum in `dex.sum`. Run it again with
another version to move to that version. Anything the library itself requires
is fetched too and added with an `// indirect` comment, because only the
program's `dex.mod` is read when building.

**A release is a git tag.** For a library at the root of its repository the tag
is the version, `v1.2.0`. A repository can hold several modules, one per
folder, each with its own `dex.mod` and its own releases; then the tag starts
with the folder, so `github.com/acme/libs/redis` at `v0.1.0` is the tag
`redis/v0.1.0`. A version without a `-` suffix wins `@latest`; `v1.3.0-beta` is
only chosen when asked for, or when it is the only release there is.

**The module cache** is `~/.dex/pkg/mod`, or wherever `DEX_MODCACHE` points,
holding one read-only folder per module and version —
`github.com/acme/greet@v1.2.0`. Every project shares it. A build never
downloads: a required module missing from the cache is an error at its import
that says to run `dex mod download`.

**`dex.sum`** holds a checksum of every module version the project has used,
and belongs in version control. A release that changes after it was first
downloaded — a tag moved, or a cached file edited — fails with both checksums
named, rather than building something nobody reviewed.

```
dex mod download   # fetch everything dex.mod requires — what CI and a Dockerfile run
dex mod verify     # check the cache against dex.sum
```

Fetching uses `git`, so it must be installed, and a private repository works
wherever `git clone` of it already works. `dex get` never asks for a password.

A required module with a `replace` is never downloaded: the folder wins, which
is what makes a monorepo, or a library being changed alongside the program that
uses it, work.

Only the main program's `dex.mod` decides where imports come from, including
imports inside the libraries it uses: a library's own module path, and anything
it requires, are resolved through the program's `require` and `replace` lines.
A library's own `dex.mod` is read for one thing only, its `dex` line.

### fmt

Print values to stdout. Accepts any type — primitives, arrays, and structs.

```dex
import "fmt"

fmt.println(42)            // 42
fmt.println("hello")       // hello
fmt.print("no newline")   // no newline (no trailing \n)
fmt.print(3.14)            // 3.140000

// Arrays print as [elem, elem, ...]
let nums: int[] = [1, 2, 3]
fmt.println(nums)          // [1, 2, 3]

// Structs print as Name{field: value, ...}
let p = Point{x: 10, y: 20}
fmt.println(p)             // Point{x: 10, y: 20}
```

| Function  | Signature                    | Description                          |
|-----------|------------------------------|--------------------------------------|
| `print`   | `print(value: any): void`    | Print any value without newline      |
| `println` | `println(value: any): void`  | Print any value with newline         |

### http

Build HTTP servers and make HTTP requests.

```dex
import "http"

http.route("GET", "/path", handler)
http.route("GET", "/users/:id", userHandler)   // route parameters
http.listen(8080)
```

#### Server

| Function   | Signature                                                      | Description                        |
|------------|----------------------------------------------------------------|------------------------------------|
| `route`    | `route(method: string, path: string, handler): void`           | Register a route handler. Path supports `:param` segments for dynamic routes (e.g. `"/users/:id"`). |
| `listen`   | `listen(port: int, workers?: int): void`                       | Start the server on a port. Optional `workers` forks multiple processes sharing it via `SO_REUSEPORT` (0 = auto-detect CPU cores). With a single worker the port is **not** shared, so starting a second instance fails with `Address already in use` rather than silently binding alongside the first. |
| `listenOn` | `listenOn(host: string, port: int, workers?: int): void`        | `listen` with the interface named: `"127.0.0.1"` binds loopback only, `"0.0.0.0"` every interface. For a service behind a proxy that should not also answer directly. A host that is not an IPv4 address is refused rather than widened to every interface. |
| `notFound` | `notFound(handler): void`                                      | Answer requests no route matched, instead of the built-in 404 body. The handler takes the shapes `route` accepts. Registering twice replaces the first. |
| `threads`  | `threads(count: int): void`                                    | How many threads run handlers; call before `listen`. `0`, the default, sizes the pool from the CPU count with a floor of four — the right hedge when a handler might block. Set it lower when handlers are CPU-bound and serialised behind a lock on a small box, where extra threads only add context switches. |
| `inline`   | `inline(): void` | Run every handler on the thread that owns the sockets instead of the pool; call before `listen`. A request then costs no thread handoff, lock or wakeup, and its response is written straight away — the fastest choice on one core when handlers are short and only touch local state such as an SQLite file. Handlers run one at a time, so one that blocks on an outbound request or a sleep stalls every connection. |
| `limits`   | `limits(maxBodyBytes: int, idleTimeoutSecs: int, maxConnections: int): void` | Bound what one client can cost the server; call before `listen`. Defaults are 8 MB, 30 seconds and 8192 connections, and a `0` for any argument keeps that default — so raising the body limit for uploads does not mean restating the others. The header block is capped at 32 KB and is not adjustable. A body over the limit is answered `413`, headers that never terminate `431`, and a connection that stops making progress for the idle timeout is closed. The connection limit is also held below the process's open-file limit: past fifteen sixteenths of it, responses carry `Connection: close` so idle clients reconnect for their next request, and at the limit new connections wait in the listen queue until one closes. |
| `response` | `response(statusCode: int, body: string, contentType: string): HttpResponse` | Create an HTTP response  |
| `responseWith` | `responseWith(statusCode: int, body: string, contentType: string, headers: string): HttpResponse` | The same, carrying extra response headers |

Handler functions can take **0 parameters** (backward-compatible) or **1 parameter** of type `http.HttpRequest`. They must return a non-void type (typically `HttpResponse` or `string`).

**HttpRequest struct** (available to handlers taking a request parameter):

```dex
struct HttpRequest {
  method: string              // "GET", "POST", etc.
  path: string                // "/api/foo"
  body: string                // request body (empty string if none)
  query: string               // raw query string after '?' (empty if none)
  params: map[string,string]  // route parameters from :param segments
  headers: map[string,string] // request headers, keys lower-cased
}
```

**Route Parameters:**

Paths can contain `:param` segments for dynamic routing. Extracted values are available via `req.params`:

```dex
http.route("GET", "/posts/:id", handleGetPost)
http.route("GET", "/posts/:postId/comments/:commentId", handleGetComment)

fn handleGetPost(req: http.HttpRequest): http.HttpResponse {
  let id: string = req.params.get("id")
  return http.response(200, id, "text/plain")
}

fn handleGetComment(req: http.HttpRequest): http.HttpResponse {
  let postId: string = req.params.get("postId")
  let commentId: string = req.params.get("commentId")
  return http.response(200, postId, "text/plain")
}
```

Routes without `:param` segments continue to use fast exact matching.

**Request headers:**

`req.headers` is a map of the request's headers. **Keys are lower-cased**, because
HTTP header names are case-insensitive and a handler should not have to guess how
the client capitalised them — read `"authorization"`, never `"Authorization"`.

```dex
fn handler(req: http.HttpRequest): http.HttpResponse {
  let token: string = req.headers.get("authorization")
  if (token.isEmpty()) {
    return http.response(401, "{\"error\":\"no token\"}", "application/json")
  }
  return http.response(200, "{}", "application/json")
}
```

A header the client did not send reads as the empty string, like any absent map
key.

**Response headers:**

`http.response` sends only `Content-Type`. To send more, use `responseWith` with
a headers block built by `http.header()` — the same builder the client functions
take:

```dex
fn handler(req: http.HttpRequest): http.HttpResponse {
  let h: string = http.header("", "Set-Cookie", "session=abc; HttpOnly; Path=/")
  h = http.header(h, "Set-Cookie", "theme=dark; Path=/")
  h = http.header(h, "Access-Control-Allow-Origin", "*")
  return http.responseWith(200, "{}", "application/json", h)
}
```

The block is newline-separated `Key: Value` pairs, and **a key may appear more
than once** — which is what `Set-Cookie` requires and what a map could not have
expressed. `Content-Type`, `Content-Length` and `Connection` are set by the
server; anything else is yours. A line without a colon is skipped rather than
emitted, so a malformed entry cannot corrupt the response framing.

**Examples:**

```dex
// Handler with request context
fn handlePost(req: http.HttpRequest): http.HttpResponse {
  let body: string = req.body
  let method: string = req.method
  return http.response(200, body, "text/plain")
}

// Handler without request context (backward-compatible)
fn handleGet(): string {
  return "{\"ok\": true}"
}

fn main(): void {
  http.route("POST", "/echo", handlePost)
  http.route("GET", "/api", handleGet)
  http.route("GET", "/users/:id", handleGetUser)
  http.listen(3000)
}
```

#### Client

HTTP client functions return an `HttpResponse` struct:

```dex
struct HttpResponse {
  statusCode: int
  body: string
  contentType: string
  headers: string             // extra response headers, newline-separated
}
```

The `contentType` field is set automatically for client responses (from the server's `Content-Type` header). In server-side struct literals it is optional — if omitted, defaults to `"application/json"`.

The `headers` field means one thing in each direction. On a response you build
for a client, it is the extra headers to send. On a response that came back
from a client call, it is the headers that arrived — the status line excluded,
since the status code is already its own field, and a repeated key kept rather
than collapsed, which is what reading `Set-Cookie` needs. Both directions use
the newline-separated `Key: Value` format that `http.header()` builds, so a
header read from one response can be passed straight into another.

Every field of a response is always a real string, including when the request
never completed. A refused connection, a timeout and a rejected certificate
all arrive as `statusCode == 0` with the reason in `body`, empty `headers` and
empty `contentType`.

**Convenience methods (headers optional):**

| Function   | Signature                            | Description                              |
|------------|--------------------------------------|------------------------------------------|
| `get`      | `get(url: string[, headers: string]): HttpResponse`     | GET request                              |
| `post`     | `post(url: string, body: string[, headers: string]): HttpResponse` | POST with `Content-Type: application/json` |
| `put`      | `put(url: string, body: string[, headers: string]): HttpResponse`  | PUT with `Content-Type: application/json`  |
| `patch`    | `patch(url: string, body: string[, headers: string]): HttpResponse` | PATCH with `Content-Type: application/json` |
| `delete`   | `delete(url: string[, headers: string]): HttpResponse`  | DELETE request                            |

**Full control:**

| Function   | Signature                                                              | Description                    |
|------------|------------------------------------------------------------------------|--------------------------------|
| `request`  | `request(method: string, url: string, body: string, headers: string): HttpResponse` | Custom method and headers      |
| `requestWith` | `requestWith(method: string, url: string, body: string, headers: string, options: HttpOptions): HttpResponse` | The same, under explicit timeouts, redirect and TLS options |

**Options:**

```dex
struct HttpOptions {
  timeoutMs: int              // whole request; 0 means the default, 30000
  connectTimeoutMs: int       // the connection alone; 0 means the default, 10000
  maxRedirects: int           // 0, the default, follows none
  insecureTls: bool           // false, the default, verifies certificates
}
```

Every field's zero value is its default, so `http.HttpOptions {}` behaves
exactly like `http.request`, and a literal names only what it changes:

```dex
let fast: http.HttpOptions = http.HttpOptions { timeoutMs: 2000 }
let r: http.HttpResponse = http.requestWith("GET", url, "", "", fast)
```

Three things are worth knowing about the defaults, because each of them is a
decision rather than an omission:

**Every request has a deadline**, 30 seconds overall and 10 to connect, on the
convenience functions too. `curl_easy_perform` blocks and has no timeout of its
own, so without one a host that accepts a connection and then says nothing
holds the calling thread until the process restarts.

**Redirects are not followed.** A 3xx is returned as itself, with its
`Location` in `headers`, which is what anything inspecting a redirect chain
wants and what anything re-checking where it is being sent needs. Set
`maxRedirects` to follow; the `headers` of the result then describe the final
hop only.

**`insecureTls` is named for the danger, not the safety**, so that a
zero-valued struct verifies. Set it only when a broken certificate is the thing
being inspected.

**Headers:**

Headers can be provided in two formats:
- **JSON object** (most intuitive): build with a `json.Value` object literal — auto-detected when the string starts with `{`
- **Header builder**: build with `http.header()` — produces newline-separated `Key: Value` pairs

| Function    | Signature                                              | Description                     |
|-------------|--------------------------------------------------------|---------------------------------|
| `header`    | `header(headers: string, key: string, value: string): string` | Append a header to the builder  |

**Multipart form data:**

| Function    | Signature                                              | Description                     |
|-------------|--------------------------------------------------------|---------------------------------|
| `formNew`   | `formNew(): string`                                    | Create empty form builder       |
| `formField` | `formField(form: string, key: string, value: string): string` | Add text field to form   |
| `formFile`  | `formFile(form: string, key: string, filePath: string): string` | Add file to form       |
| `postForm`  | `postForm(url: string, form: string[, headers: string]): HttpResponse`    | Submit form as multipart        |

**Examples:**

```dex
import "http"
import "fmt"
import "json"

fn main(): void {
  // Simple GET
  let resp: HttpResponse = http.get("https://api.example.com/data")
  fmt.println(resp.statusCode)
  fmt.println(resp.body)
  fmt.println(resp.contentType)

  // Parse JSON response
  let doc: json.Value = json.decode(resp.body)
  let name: string = doc["name"].asString()
  let count: int = doc["count"].asInt()
  let active: bool = doc["active"].asBool()

  // POST with JSON body
  let body: string = json.encode({ name: "Alice" })
  let r2: HttpResponse = http.post("https://api.example.com", body)

  // GET with headers (JSON approach)
  let headers: string = json.encode({
      "Authorization": "Bearer token",
      "Accept": "application/json"
  })
  let r3: HttpResponse = http.get("https://api.example.com/data", headers)

  // POST with headers (header builder approach)
  let h: string = http.header("", "Authorization", "Bearer token")
  h = http.header(h, "Content-Type", "application/json")
  let r4: HttpResponse = http.post("https://api.example.com", body, h)

  // Full control with custom headers
  let r5: HttpResponse = http.request("POST", "https://api.example.com", body, "Authorization: Bearer token\nX-Custom: value")

  // Multipart form upload
  let form: string = http.formNew()
  form = http.formField(form, "name", "Alice")
  form = http.formFile(form, "avatar", "/path/to/photo.jpg")
  let r6: HttpResponse = http.postForm("https://api.example.com/upload", form)
}
```

### json

Encode and decode JSON. The module has two functions — `json.encode` and
`json.decode` — and one type, `json.Value`.

| Function | Signature | Description |
|----------|-----------|-------------|
| `encode` | `encode(value: json.Value \| struct \| T[] \| map[string, V]): string` | Serialize a value to JSON text |
| `decode` | `decode(json: string \| json.Value): T` | Parse JSON into `json.Value`, a struct, or a struct array |

#### Structs

A struct encodes to a JSON object and decodes back, recursing through nested
struct fields and array fields to any depth:

```dex
struct Point {
    x: int
    y: int
}

struct Shape {
    name: string
    origin: Point
    sides: int[]
}

let s: Shape = Shape { name: "square", origin: Point { x: 1, y: 2 }, sides: [4, 4, 4, 4] }
let text: string = json.encode(s)
// {"name":"square","origin":{"x":1,"y":2},"sides":[4,4,4,4]}

let back: Shape = json.decode(text)
// back.origin.y == 2
```

Field names are the JSON keys. A key absent from the input leaves that field at
its zero value, and an absent array field yields an empty array rather than a
null one.

String values are escaped on the way out and unescaped on the way back, so a
field may hold anything — a quote, a backslash, a newline, a tab, a control
character, or another JSON document — and survive the round trip byte for byte:

```dex
let row: Row = Row { text: "{\"a\": \"b\"}" }
let text: string = json.encode(row)
// {"text":"{\"a\": \"b\"}"}
let back: Row = json.decode(text)
// back.text == "{\"a\": \"b\"}"
```

Escaping follows RFC 8259: `"` and `\` are escaped, the five shorthand controls
are spelled out, anything else below `0x20` becomes `\u00XX`, and valid UTF-8
above that passes through unchanged. `\uXXXX` on input is decoded, surrogate
pairs included.

#### Checked decoding

Annotating the target as optional (`T?`) makes `json.decode` report failure
instead of silently producing zero values. This is what you want at a trust
boundary such as an HTTP request body:

```dex
let p: Point? = json.decode(req.body)
if (p == null) {
    return http.response(400, json.encode(Error { message: "invalid JSON body" }), "application/json")
}
```

`T?` yields `null` when the input is not well-formed JSON, is not a JSON object,
or has a key whose type does not fit the corresponding field. A key that is
absent, or explicitly `null`, is not an error. A plain `T` target keeps the
lenient behaviour, decoding whatever it can and zeroing the rest.

A JSON array of objects decodes into a struct array, with the same distinction:

```dex
let items: Point[] = json.decode(raw)      // lenient — a non-array gives an empty array
let checked: Point[]? = json.decode(raw)   // null unless raw is an array of valid Points
```

For the checked form, a single bad element rejects the whole array.

#### json.Value

`json.Value` is a JSON document: null, a boolean, a number, a string, an array,
or an object. Reach for it when the shape is not a fixed struct — a wire
protocol that frames messages as arrays, a payload whose type you learn by
reading a field, a value you want to pass through untouched.

Write one with ordinary literal syntax. An object literal `{ key: value }` is
always a `json.Value`, and an array literal becomes one when the target says so,
which is what lets its elements have different types:

```dex
let config: json.Value = { name: "dex", version: 3, stable: true }
let mixed: json.Value = [1, "two", true, null, {}]
```

Keys may be bare identifiers or string literals, so keys that are not valid Dex
identifiers still work:

```dex
let record: json.Value = { "first-name": "Ada", "born": 1815 }
```

Literals nest to any depth, and a value built earlier drops straight into a
later one:

```dex
let origin: json.Value = { x: 0, y: 0 }
let scene: json.Value = { origin: origin, points: [{ x: 1, y: 2 }, { x: 3, y: 4 }] }
```

Structs, arrays and maps convert automatically when used inside a literal:

```dex
let p: Point = Point { x: 1, y: 2 }
let ids: int[] = [7, 8, 9]
let payload: json.Value = { point: p, ids: ids }
// {"point":{"x":1,"y":2},"ids":[7,8,9]}
```

#### Reading a json.Value

Index with an int to read an array position, or with a string to read an object
key. Both give back a `json.Value`, so a path is walked one step at a time:

```dex
let doc: json.Value = json.decode(text)
let name: string = doc["name"].asString()
let first: json.Value = doc["points"][0]
let x: int = doc["points"][0]["x"].asInt()
```

A missing key or an out-of-range index yields a null value rather than failing,
so reading down a path that does not exist gives a zero value instead of a
crash:

```dex
doc["absent"].isNull()      // true
doc["absent"].asInt()       // 0
doc["absent"].asString()    // ""
```

| Method | Returns | Description |
|--------|---------|-------------|
| `.asInt()` | `int` | Value as an int |
| `.asLong()` | `long` | Value as a long |
| `.asDouble()` | `double` | Value as a double |
| `.asString()` | `string` | String contents; any other value as its JSON text |
| `.asBool()` | `bool` | Value as a bool |
| `.isNull()` | `bool` | Is it null (or absent)? |
| `.isBool()` | `bool` | Is it a boolean? |
| `.isNumber()` | `bool` | Is it a number? |
| `.isString()` | `bool` | Is it a string? |
| `.isArray()` | `bool` | Is it an array? |
| `.isObject()` | `bool` | Is it an object? |
| `.len()` | `int` | Element count for an array or object, character count for a string |
| `.has(key: string)` | `bool` | Does this object have that key? |
| `.keys()` | `string[]` | This object's keys, in insertion order |

The accessors coerce rather than insisting on an exact match, because wire
formats routinely disagree about whether a number is quoted:

```dex
let v: json.Value = { quoted: "42", bare: 42 }
v["quoted"].asInt()      // 42
v["bare"].asString()     // "42"
```

Use the `is*` predicates when the distinction matters.

#### Moving between json.Value and structs

`json.decode` accepts a `json.Value` as well as text, so a nested payload goes
straight into a struct without being re-serialized by hand:

```dex
let envelope: json.Value = json.decode(raw)
let body: Point = json.decode(envelope["body"])
```

Malformed input decodes to a null value rather than failing the program, so
`isNull()` is the check for "this was not JSON":

```dex
let doc: json.Value = json.decode(untrusted)
if (doc.isNull()) {
    return
}
```

#### Removed functions

The module is `json.encode`, `json.decode` and the `json.Value` type. Fifteen
string-scanning functions that predated `json.Value` were removed in 0.9.3;
calling one is now an undefined function. The replacements:

| Removed | Use instead |
|---------|-------------|
| `json.new()`, `json.set`, `json.setObj`, `json.setArray` | An object literal: `{ key: value }` |
| `json.arrayNew()`, `json.arrayPush`, `json.arrayPushObj` | An array literal: `[a, b, c]` |
| `json.get`, `json.getInt`, `json.getBool`, `json.getDouble`, `json.getLong` | Indexing: `v["key"].asInt()` |
| `json.arrayGet`, `json.arrayGetRaw` | Indexing: `v[0]` |
| `json.arrayLen` | `v.len()` |

### db

Database access supporting SQLite, PostgreSQL, MySQL, and MongoDB.

```dex
import "db"

let conn: int = db.open("sqlite", "myapp.db")
db.exec(conn, "CREATE TABLE IF NOT EXISTS users (id INTEGER PRIMARY KEY, name TEXT, age INTEGER)")
db.exec(conn, "INSERT INTO users (name, age) VALUES ('Alice', 30)")

let rows: int = db.query(conn, "SELECT id, name, age FROM users")
while (db.next(rows)) {
    let id: int = db.col(rows, 0)
    let name: string = db.col(rows, 1)
    let age: int = db.col(rows, 2)
    fmt.println(name)
}
db.free(rows)
db.close(conn)
```

#### Filling a struct from a row

A row mapper written by hand names every field twice — once in the struct and
once in the mapper — and only the struct side is checked, so a renamed field or
a column inserted into the middle of a projection goes wrong silently.
`db.fill` reads column `i` into field `i`, so the struct is the only place the
shape is written down:

```dex
struct Person {
    id: long
    name: string
    age: int
}

let rows: int = db.query(conn, "SELECT id, name, age FROM people")
while (db.next(rows)) {
    let p: Person = Person {}
    db.fill(p, rows)
    fmt.println(p.name)
}
db.free(rows)
```

Field order is column order, so the projection must select one column per field
in declaration order — build it from the struct's own field list with
[`reflect.fields`](#reflect) and [`reflect.fieldTags`](#reflect) and the two
cannot drift apart. A field reflection cannot read (an array, a map, a nested
struct) is skipped but still consumes its column position, so the fields after
it stay aligned.

The subject is held to the same rules a [reflect setter](#writing-fields) is: a
`&Struct` or a local variable, never a by-value parameter, an array element, or
a temporary. A `string` field's old reference is released and the column's
freshly-read one moves in, so filling the same struct in a loop does not leak.

| Function | Signature                                     | Description                              |
|----------|-----------------------------------------------|------------------------------------------|
| `open`   | `open(driver: string, dsn: string): int`      | Open a connection, returns handle        |
| `exec`   | `exec(conn: int, sql: string): int`           | Execute a statement, returns affected rows |
| `query`  | `query(conn: int, sql: string): int`          | Execute a query, returns result handle   |
| `next`   | `next(rows: int): bool`                       | Advance to next row (a result or a prepared statement) |
| `col`    | `col(rows: int, index: int): int\|long\|string\|double\|bool` | Read a column value (type from context) |
| `fill`   | `fill(into: struct\|&struct, rows: int): void` | Fill a struct from the current row, column `i` into field `i` |
| `free`   | `free(rows: int): void`                       | Free a result set, or finalize a statement |
| `close`  | `close(conn: int): void`                      | Close a connection                       |

#### Bound parameters

`query` and `exec` take a finished string, so any value going into one has to be
escaped by hand — an argument you have to keep winning, on every call site,
forever. `queryParams` and `execParams` send the statement and its values to the
server separately instead. The server parses the SQL once and treats every
parameter as data, so a quote, a semicolon or a comment marker inside a value
cannot become syntax.

```dex
let args: string[] = ["O'Brien; DROP TABLE people; --", "44"]
db.execParams(conn, "INSERT INTO people (name, age) VALUES ($1, $2)", args)

let older: string[] = ["40"]
let rows: int = db.queryParams(conn, "SELECT name FROM people WHERE age > $1", older)
while (db.next(rows)) {
    let name: string = db.col(rows, 0)
}
db.free(rows)
```

| Function      | Signature                                                        | Description                              |
|---------------|------------------------------------------------------------------|------------------------------------------|
| `queryParams` | `queryParams(conn: int, sql: string, values: string[]): int`     | Query with bound parameters; returns a result handle |
| `execParams`  | `execParams(conn: int, sql: string, values: string[]): int`      | Statement with bound parameters; returns affected rows |

Placeholders are Postgres's own `$1`, `$2`, … numbered from one, and the array
is positional: `values[0]` is `$1`.

`queryParams` returns an ordinary result handle, so `next`, `col` and `free`
work exactly as they do after `query` — including for `RETURNING`.

Parameters are sent as text with no declared types, and the server infers each
one from where it appears: `WHERE id = $1` against a `bigint` column reads `$1`
as a bigint. That is why `string[]` is enough despite the language having no
heterogeneous array — stringify with `str.fromInt`, `str.fromLong` and friends,
and let the server do the rest.

SQL `NULL` is deliberately not expressible as a parameter value. Whether a value
is null is a structural fact the caller already knows, and it changes the
statement rather than just its data, so write the keyword into the SQL:

```dex
// not a parameter — a different statement
let sql: string = "UPDATE people SET manager_id = NULL WHERE id = $1"
```

Both functions are **PostgreSQL only**. SQLite and MySQL spell placeholders
differently, and silently accepting a statement written for one dialect on
another is how a query that looks bound stops being bound; against those drivers
these return `-1` and say so on stderr. Use
[`prepare`](#prepared-statements) there, whose placeholder is `?`.

#### Prepared statements

`queryParams` compiles its SQL on every call. A statement on a hot path can be
compiled once and re-run instead, which is what `prepare` is for: it returns a
handle you bind values into, step, and reset. It is the only bound-parameter
path for SQLite and MySQL, whose placeholder is `?`, positional from one.

```dex
let st: int = db.prepare(conn, "SELECT id, name FROM people WHERE age > ?")
db.bindInt(st, 1, 40)
while (db.next(st)) {
    let id: long = db.col(st, 0)
    let name: string = db.col(st, 1)
}

// Same plan, new values.
db.reset(st)
db.bindInt(st, 1, 60)
while (db.next(st)) {
    let name: string = db.col(st, 1)
}
db.finalize(st)
```

**A prepared statement is a row source, the same as a result.** `col`, `fill`,
`next` and `free` take either handle, so a projection does not have to know
which of the two produced its row — and `db.fill` works against a statement
exactly as it does against a query. `step` and `next` are the same operation on
a statement, and `free` and `finalize` are the same release; both spellings
exist because both read naturally at their own call sites.

Stepping past the last row answers `false` rather than re-running the statement,
so `while (db.next(st))` terminates. `reset` rewinds it and clears its
bindings, so every parameter has to be bound again.

| Function     | Signature                                            | Description                                 |
|--------------|------------------------------------------------------|---------------------------------------------|
| `prepare`    | `prepare(conn: int, sql: string): int`               | Compile a statement; returns a handle, `-1` on error |
| `bindInt`    | `bindInt(stmt: int, index: int, value: int): void`   | Bind an int (1-based index)                 |
| `bindLong`   | `bindLong(stmt: int, index: int, value: long): void` | Bind a 64-bit integer                       |
| `bindDouble` | `bindDouble(stmt: int, index: int, value: double): void` | Bind a double                           |
| `bindStr`    | `bindStr(stmt: int, index: int, value: string): void`| Bind a string                               |
| `step`       | `step(stmt: int): bool`                              | Run one step; true if a row is available    |
| `reset`      | `reset(stmt: int): void`                             | Rewind and clear bindings for a re-run      |
| `finalize`   | `finalize(stmt: int): void`                          | Destroy the statement                       |

`bindInt` truncates past 2^31, which an id column reaches, so bind one with
`bindLong` — the reason `db.col` hands ids back as `long` is the same.

A statement is not safe to step from two threads at once. Guard it with a
[`mutex`](#mutex) covering the column reads as well as the step, because a
column is only readable while the statement sits on its row.

`col` returns the type declared in the assignment — the compiler dispatches to the correct column reader:

```dex
let id: int = db.col(rows, 0)        // reads as int
let name: string = db.col(rows, 1)   // reads as string
let score: double = db.col(rows, 2)  // reads as double
let active: bool = db.col(rows, 3)   // reads as bool
```

An explicit type annotation is required — `let x = db.col(rows, 0)` is a compile error.

**Supported drivers:**

| Driver       | DSN format                                                        |
|--------------|-------------------------------------------------------------------|
| `"sqlite"`   | File path: `"myapp.db"`                                           |
| `"postgres"` | libpq conninfo: `"host=localhost dbname=mydb"`                    |
| `"mysql"`    | Key-value: `"host=localhost user=root dbname=mydb port=3306"`     |
| `"mongo"`    | URI: `"mongodb://localhost:27017/mydb"`                           |

### math

Mathematical functions and constants. All functions operate on `double` values.

```dex
import "math"

let pi: double = math.pi()
let root: double = math.sqrt(16.0)   // 4.0
let val: double = math.pow(2.0, 10.0) // 1024.0
```

| Function  | Signature                          | Description                        |
|-----------|------------------------------------|------------------------------------|
| `pi`      | `pi(): double`                     | Returns π (3.14159...)             |
| `e`       | `e(): double`                      | Returns Euler's number (2.71828...) |
| `sin`     | `sin(x: double): double`           | Sine                               |
| `cos`     | `cos(x: double): double`           | Cosine                             |
| `tan`     | `tan(x: double): double`           | Tangent                            |
| `asin`    | `asin(x: double): double`          | Arcsine                            |
| `acos`    | `acos(x: double): double`          | Arccosine                          |
| `atan`    | `atan(x: double): double`          | Arctangent                         |
| `sqrt`    | `sqrt(x: double): double`          | Square root                        |
| `pow`     | `pow(base: double, exp: double): double` | Exponentiation              |
| `exp`     | `exp(x: double): double`           | e^x                                |
| `floor`   | `floor(x: double): double`         | Floor (round down)                 |
| `ceil`    | `ceil(x: double): double`          | Ceiling (round up)                 |
| `round`   | `round(x: double): double`         | Round to nearest                   |
| `abs`     | `abs(x: double): double`           | Absolute value                     |
| `log`     | `log(x: double): double`           | Natural logarithm                  |
| `log2`    | `log2(x: double): double`          | Base-2 logarithm                   |
| `log10`   | `log10(x: double): double`         | Base-10 logarithm                  |
| `min`     | `min(a: double, b: double): double` | Minimum of two values             |
| `max`     | `max(a: double, b: double): double` | Maximum of two values             |

### time

Time functions for measuring, waiting, and scheduling.

```dex
import "time"

let start: long = time.now()
time.sleep(100)  // sleep 100ms
let elapsed: long = time.now() - start
```

| Function        | Signature                        | Description                                      |
|-----------------|----------------------------------|--------------------------------------------------|
| `now`           | `now(): long`                    | Current time in milliseconds                     |
| `nowNs`         | `nowNs(): long`                  | Current time in nanoseconds                      |
| `sleep`         | `sleep(ms: int): void`           | Sleep for specified milliseconds                 |
| `setTimeout`    | `setTimeout(fn, ms: int): void`  | Call a function once after `ms` milliseconds     |
| `setInterval`   | `setInterval(fn, ms: int): int`  | Call a function every `ms` milliseconds; returns timer ID |
| `clearInterval` | `clearInterval(id: int): void`   | Stop a repeating interval by timer ID            |
| `format`        | `format(timestamp: long, layout: string): string` | Format a Unix timestamp (seconds). Use `"iso8601"` for ISO 8601 format, or strftime patterns. |
| `isoNow`        | `isoNow(): string`               | Current time as ISO 8601 string (UTC)            |
| `unixNow`       | `unixNow(): long`                | Current Unix timestamp in seconds (wall clock)   |

**Timer/Interval example:**

```dex
import "time"
import "fmt"

fn heartbeat(): void {
  fmt.println("tick")
}

fn main(): void {
  time.setTimeout(heartbeat, 5000)              // call once after 5s
  let id: int = time.setInterval(heartbeat, 1000)  // call every 1s
  time.sleep(10000)
  time.clearInterval(id)                        // stop interval
}
```

Timer callbacks must take no parameters and return void. Each timer runs on a separate thread.

### ws

WebSocket module for persistent bidirectional connections (RFC 6455).

```dex
import "ws"
```

#### Types

**Conn** — WebSocket connection handle:

```dex
struct Conn {
  fd: int          // socket file descriptor
  isServer: int    // 1 if server-side, 0 if client-side
  ssl: long        // SSL pointer (0 if plain ws://)
}
```

#### Server

| Function           | Signature                         | Description                               |
|--------------------|-----------------------------------|-------------------------------------------|
| `handleMessage`    | `handleMessage(fn): void`         | Register message handler for this thread's server |
| `handleConnect`    | `handleConnect(fn): void`         | Register connect handler (called with path) |
| `handleDisconnect` | `handleDisconnect(fn): void`      | Register disconnect handler               |
| `setProtocol`      | `setProtocol(protocol: string): void` | Set the subprotocol offered by this thread's handshakes |
| `header`           | `header(conn: ws.Conn, name: string): string` | A header from the client's opening handshake, matched case-insensitively; `""` if absent |
| `listen`           | `listen(port: int): void`         | Start a server on `port`, capturing this thread's registration; blocks |

Message handlers must take `(conn: ws.Conn, msg: string)` and return `void`. Connect handlers take `(conn: ws.Conn, path: string)`. Disconnect handlers take `(conn: ws.Conn)`.

`ws.header` reads the request that opened a server-side connection, so a connect
handler can authenticate a client before accepting it (by its `Authorization`
header, say) and reject it with `ws.close`. On a client connection it returns
`""`.

```dex
import "ws"
import "fmt"

fn onConnect(conn: ws.Conn, path: string): void {
  fmt.println("client connected on path: " + path)
}

fn onMessage(conn: ws.Conn, msg: string): void {
  fmt.println("got: " + msg)
  ws.send(conn, "echo: " + msg)
}

fn onDisconnect(conn: ws.Conn): void {
  fmt.println("client disconnected")
}

fn main(): void {
  ws.setProtocol("chat.v1")
  ws.handleConnect(onConnect)
  ws.handleMessage(onMessage)
  ws.handleDisconnect(onDisconnect)
  ws.listen(9090)
}
```

##### Running several servers

Handler and subprotocol registration is **per-thread**, and `listen` captures
whatever is registered on its thread at the moment it starts. Each server then
owns that configuration — along with its own event loop and worker pool — so
registering on one thread can never disturb a server started on another.

To run more than one server, `spawn` each on its own thread and register inside
it. Registering from the parent and then spawning will not work: the spawned
thread starts with no handlers.

```dex
fn startChat(): void {
  ws.setProtocol("chat.v1")       // this thread's registration...
  ws.handleConnect(onChatConnect)
  ws.handleMessage(onChatMessage)
  ws.listen(9000)                 // ...captured by this server
}

fn startTelemetry(): void {
  ws.setProtocol("telemetry.v1")
  ws.handleConnect(onTelemetryConnect)
  ws.handleMessage(onTelemetryMessage)
  ws.listen(9001)
}

fn main(): void {
  spawn { startChat() }
  startTelemetry()                // the last one blocks, keeping main alive
}
```

Port 9000 negotiates `chat.v1` and dispatches to `onChatMessage`; port 9001
negotiates `telemetry.v1` and dispatches to `onTelemetryMessage`.

`ws.send` works from any thread — it queues the frame and wakes the event loop
belonging to that connection's server.

#### Client

| Function  | Signature                          | Description                                |
|-----------|------------------------------------|--------------------------------------------|
| `connect` | `connect(url: string): ws.Conn`    | Connect to a WebSocket server (`ws://` or `wss://`) |
| `send`    | `send(conn: ws.Conn, msg: string): void` | Send text message on connection      |
| `receive` | `receive(conn: ws.Conn): string`   | Receive text message (blocking)            |
| `close`   | `close(conn: ws.Conn): void`       | Close connection                           |

```dex
import "ws"

fn main(): void {
  let conn: ws.Conn = ws.connect("ws://localhost:9090")
  ws.send(conn, "hello")
  let reply: string = ws.receive(conn)
  ws.close(conn)
}
```

The `connect` function supports both `ws://` (plain) and `wss://` (TLS). TLS is handled via OpenSSL when available; if OpenSSL is not installed, `ws://` still works. For production deployments, a common pattern is to use a reverse proxy (nginx/caddy) for TLS termination.

The WebSocket implementation supports text frames, automatic ping/pong handling, and proper client-side frame masking per RFC 6455.

### redis

A Redis client. Speaks RESP over plain sockets, so it needs no library installed
to build or run.

```dex
import "redis"

let r: int = redis.connect("127.0.0.1", 6379, "")   // "" = no password
if (r != -1) {
    redis.setex(r, "session:ab12", "node-a", 30)
    let owner: string = redis.get(r, "session:ab12")    // "" when absent
    redis.close(r)
}
```

Each handle owns a pool of eight sockets: a Redis connection carries one request
at a time, so threads sharing a handle take a socket each rather than reading one
another's replies. A dropped socket is redialled on its next use.

| Function | Signature | Description |
|---|---|---|
| `connect` | `connect(host: string, port: int, password: string): int` | Open a pool. Returns a handle, or `-1` |
| `close` | `close(conn: int): void` | Close every socket the handle holds |
| `ping` | `ping(conn: int): bool` | True when the server answers |
| `set` | `set(conn: int, key: string, value: string): bool` | Set a key |
| `setex` | `setex(conn: int, key: string, value: string, seconds: int): bool` | Set a key that expires |
| `get` | `get(conn: int, key: string): string` | Read a key; missing reads as `""` |
| `exists` | `exists(conn: int, key: string): bool` | Is the key present |
| `del` | `del(conn: int, key: string): long` | Delete, returning how many went |
| `expire` | `expire(conn: int, key: string, seconds: int): bool` | Give a key a time to live |
| `ttl` | `ttl(conn: int, key: string): long` | Seconds left; `-1` never, `-2` gone |
| `incr` | `incr(conn: int, key: string): long` | Increment and return the new value |
| `keys` | `keys(conn: int, pattern: string): string[]` | Keys matching a glob |
| `hset` / `hget` / `hdel` | `(conn: int, key: string, field: string, ...)` | One field of a hash |
| `hkeys` | `hkeys(conn: int, key: string): string[]` | A hash's field names |
| `hgetall` | `hgetall(conn: int, key: string): string[]` | Flat: field, value, field, value |
| `sadd` / `srem` | `(conn: int, key: string, member: string): long` | Set membership |
| `sismember` | `sismember(conn: int, key: string, member: string): bool` | Is the member there |
| `smembers` | `smembers(conn: int, key: string): string[]` | Every member |
| `publish` | `publish(conn: int, channel: string, message: string): long` | Publish, returning receivers |
| `subscribe` | `subscribe(conn: int, channel: string): bool` | Subscribe on a socket of its own |
| `nextMessage` | `nextMessage(conn: int): string` | Block for the next message |
| `lastChannel` | `lastChannel(conn: int): string` | Which channel it came in on |
| `command` | `command(conn: int, args: string[]): string` | Any command, as its arguments |

Subscribing takes a connection out of request/reply for good, so it gets a socket
of its own and messages are pulled rather than pushed:

```dex
spawn {
    redis.subscribe(sub, "jobs:instance:node-a")
    while (true) {
        let msg: string = redis.nextMessage(sub)
        if (msg.isEmpty()) {
            break          // the subscriber connection dropped
        }
        handle(msg)
    }
}
```

Anything without a wrapper is spelled as its arguments:

```dex
let args: string[] = ["ZADD", "leaderboard", "42", "player-1"]
redis.command(r, args)
```

### kafka

A Kafka client, wrapping librdkafka. Unlike Redis, the protocol is too large to
speak by hand — consumer groups and partition leadership are most of it — so this
module needs the library at build time:

```sh
brew install librdkafka        # macOS
apt install librdkafka-dev     # Debian/Ubuntu
```

`dex build` finds it with `pkg-config`. Without it the module still compiles and
every call fails with a message saying so, so a program that only mentions Kafka
on a path it never takes still builds and runs.

```dex
import "kafka"

let p: int = kafka.producer("localhost:9092")
kafka.produce(p, "orders.created", "order-42", payload)   // key picks the partition
kafka.flush(p, 5000)
kafka.close(p)
```

```dex
let c: int = kafka.consumer("localhost:9092", "persister")
kafka.subscribe(c, "orders.created")
while (true) {
    let msg: string = kafka.poll(c, 1000)     // "" when nothing arrived
    if (!msg.isEmpty()) {
        store(kafka.lastKey(c), msg)
        kafka.commit(c)                        // committed after handling, not before
    }
}
```

| Function | Signature | Description |
|---|---|---|
| `available` | `available(): bool` | Did this build find librdkafka |
| `producer` | `producer(brokers: string): int` | Open a producer; `-1` on failure |
| `produce` | `produce(producer: int, topic: string, key: string, value: string): bool` | Queue a message |
| `flush` | `flush(producer: int, timeoutMs: int): int` | Wait for delivery; returns what is still queued |
| `consumer` | `consumer(brokers: string, groupId: string): int` | Open a consumer in a group |
| `subscribe` | `subscribe(consumer: int, topic: string): bool` | Add a topic |
| `poll` | `poll(consumer: int, timeoutMs: int): string` | Wait for one message |
| `lastTopic` | `lastTopic(consumer: int): string` | Topic of the last message |
| `lastKey` | `lastKey(consumer: int): string` | Key of the last message |
| `commit` | `commit(consumer: int): bool` | Commit polled offsets |
| `close` | `close(handle: int): void` | Flush or leave the group, then close |

Messages sharing a key land on the same partition and keep their order, which is
why a stable identity — an account id, a user id — makes a good key. Auto-commit
is off: a consumer commits once a message is safely handled, so a crash replays
it rather than losing it.

### smtp

Sends one email over SMTP. Like Redis this speaks the protocol directly rather
than linking a client library, so nothing needs installing to build it — but TLS
comes from OpenSSL, found the same way `wss://` finds it.

```dex
import "smtp"

let sent: bool = smtp.send(
    "mail.example.com", 587,          // 465 for TLS from the first byte
    "no-reply@example.com", password,
    "Example <no-reply@example.com>",
    "someone@elsewhere.test",
    "Reset your password",
    "Open this link within the hour:\nhttps://app.example/r/abc\n")
```

| Function | Signature | Description |
|---|---|---|
| `send` | `send(host: string, port: int, username: string, password: string, from: string, to: string, subject: string, body: string): bool` | Send one plain-text message and wait to be told it was accepted |

The port picks the encryption: **465** is TLS from the first byte, anything else
opens in the clear and upgrades with `STARTTLS` before authenticating. With a
username set and neither on offer the call fails rather than sending the password
in the clear; pass `""` for the username to skip authentication entirely, which
is what a relay that trusts this host by address wants.

`from` and `to` may carry a display name — `Example <no-reply@example.com>` —
and the bare address inside the angle brackets is what the envelope gets. The
headers written for you are `From`, `To`, `Subject`, `Date`, `Message-ID`,
`MIME-Version` and `Content-Type: text/plain; charset=UTF-8`; a subject that is
not ASCII is encoded per RFC 2047, and a body line beginning with `.` is stuffed
so it arrives whole.

**True means a mail server took responsibility for the message**, not that a
socket opened: it returns only once the server has answered the terminating dot.
Every refusal along the way — a rejected recipient, a wrong password, a message
over the size limit — returns false and writes the reply code to stderr. Sockets
carry a 30-second timeout in each direction so a silent host cannot pin the
thread that called.

Certificates are not verified, which matches the `ws` module and is what makes
this work against the mail host of a shared-hosting account, where the
certificate is usually the provider's rather than yours. One that does not verify
is reported on stderr rather than refused.

### crypto

Cryptographic utility functions.

```dex
import "crypto"

let id: string = crypto.uuid()
// e.g. "a1b2c3d4-e5f6-4789-abcd-ef0123456789"
```

| Function | Signature | Description |
|---|---|---|
| `uuid` | `uuid(): string` | A random UUID v4 |
| `base64Encode` | `base64Encode(data: string): string` | Base64-encode. Binary-safe |
| `base64Decode` | `base64Decode(encoded: string): string` | Decode base64. Binary-safe |
| `base64UrlDecode` | `base64UrlDecode(encoded: string): string` | Decode base64url — the `-`/`_` alphabet, padding optional. Binary-safe |
| `sha256Hex` | `sha256Hex(data: string): string` | SHA-256, lowercase hex |
| `hmacSha256Hex` | `hmacSha256Hex(key: string, message: string): string` | HMAC-SHA-256, lowercase hex |
| `hmacSha256B64Url` | `hmacSha256B64Url(key: string, message: string): string` | HMAC-SHA-256, unpadded base64url — the encoding a JWS signature is written in |
| `hmacSha1Hex` | `hmacSha1Hex(key: string, message: string): string` | HMAC-SHA-1, lowercase hex — only where a protocol requires it, such as TOTP |
| `base32Encode` | `base32Encode(data: string): string` | RFC 4648 base32, upper case, unpadded. Binary-safe |
| `base32Decode` | `base32Decode(text: string): string` | Decode base32, ignoring case, spaces, `-` and `=`; `""` for any other character. Binary-safe |
| `verifyRs256` | `verifyRs256(message: string, signature: string, modulus: string, exponent: string): bool` | Verify an RS256 signature against an RSA public key given as its two numbers |
| `hashPassword` | `hashPassword(password: string): string` | Hash a password for storing |
| `verifyPassword` | `verifyPassword(password: string, stored: string): bool` | Check a password against a stored hash, in constant time |
| `randomHex` | `randomHex(bytes: int): string` | Random bytes as hex, for session and invite tokens |
| `randomBase32` | `randomBase32(bytes: int): string` | Random bytes as base32, for a TOTP secret |
| `totp` | `totp(secret: string, unixSeconds: long): string` | The six-digit RFC 6238 code an authenticator app shows for a base32 secret |
| `encrypt` | `encrypt(keyHex: string, plaintext: string): string` | Encrypt a secret for storing, AES-256-GCM, as base64 |
| `decrypt` | `decrypt(keyHex: string, sealed: string): string` | Decrypt what `encrypt` produced; `""` if the key is wrong or the input was altered |

#### Passwords

`hashPassword` is the only thing a password should be stored as. It uses
PBKDF2-HMAC-SHA-256 with a fresh random salt and 600000 iterations, OWASP's
figure, and writes all three into the result:

```dex
let stored: string = crypto.hashPassword(password)
// pbkdf2-sha256$600000$5f0c...$9a1e...

if (crypto.verifyPassword(attempt, stored)) {
    // signed in
}
```

The iteration count travels with each hash, so raising it later still
verifies every password stored before. `verifyPassword` is false for every
reason: a wrong password, a malformed hash, a build without OpenSSL.

`randomHex(32)` is 32 bytes from the system's entropy source, 64 hex digits:
a session token or an invite link. It returns `""` rather than a guessable
value when the source cannot be read.

#### Secrets at rest

`encrypt` and `decrypt` keep a secret, such as an API token a user entrusts to
you, unreadable in a database or a backup. The key is 32 bytes as 64 hex
digits; make one with `randomHex(32)` and keep it outside the database:

```dex
let key: string = os.env("SECRET_KEY")
let sealed: string = crypto.encrypt(key, token)   // store this
let token: string = crypto.decrypt(key, sealed)   // "" if anything is wrong
```

It is AES-256-GCM with a fresh nonce on every call, so encrypting the same
secret twice gives different results, and the tag means altered or truncated
input decrypts to `""` rather than to something wrong.

#### Two-factor codes

`totp` gives the code an authenticator app (Google Authenticator, 1Password,
Authy) shows: RFC 6238 with the settings they all assume — 30-second steps,
HMAC-SHA-1, six digits. Make a secret once per account, store it sealed with
`encrypt`, and show it to the person as an `otpauth://` URI, usually as a QR
code:

```dex
let secret: string = crypto.randomBase32(20)   // 32 characters
let uri: string = "otpauth://totp/MyApp:" + email + "?secret=" + secret + "&issuer=MyApp"
```

The label and issuer should be percent-encoded if they can hold anything but
letters and digits. To check a code, accept the step before and the step after
as well, since phone clocks drift, compare with `secureEquals`, and remember
the last step used so a code cannot be replayed:

```dex
let now: long = time.unixNow()
let ok: bool = false

foreach ([now - 30, now, now + 30] as at) {
    if (crypto.secureEquals(crypto.totp(secret, at), code)) {
        ok = true
    }
}
```

`base32Decode` forgives the spaces and hyphens people type when copying a
secret by hand, and refuses anything else with `""`; `totp` returns `""` for a
secret that does not decode.

#### Verifying what somebody else signed

`hmacSha256Hex` covers a secret both sides already hold — a webhook from a
payment provider. It cannot cover a token from an identity provider, which is
signed with a private key you will never have and verified against a public one
published as a JWK: a modulus and an exponent, base64url, with no PEM anywhere in
the document.

`verifyRs256` takes the key in exactly that shape, so checking a Google or Apple
ID token is decode, decode, verify:

```dex
// jwt is "header.payload.signature"; `key` is one entry from the provider's JWKS
let parts: string[] = jwt.split(".")
// The signature covers the first two segments joined, as they arrived — not
// the decoded claims, so nothing here re-encodes anything. (`signed` is a
// reserved word, hence the longer name.)
let signingInput: string = parts[0] + "." + parts[1]
let signature: string = crypto.base64UrlDecode(parts[2])

let modulus: string = crypto.base64UrlDecode(key["n"].asString())
let exponent: string = crypto.base64UrlDecode(key["e"].asString())

if (crypto.verifyRs256(signingInput, signature, modulus, exponent)) {
    // The signature holds. The claims inside still have to be checked —
    // `aud`, `iss` and `exp` are not signature questions.
}
```

Use `base64UrlDecode` and not `base64Decode` on any of those three. The ordinary
decoder skips characters outside its alphabet, which is right for a PEM body
with newlines in it and silently wrong here: `-` and `_` would be dropped rather
than decoded, and the bytes would come back short instead of refused.

A false is a false for every reason — a bad signature, a malformed key, a build
without OpenSSL. There is deliberately no way to tell them apart from the return
value, because none of them is a reason to admit the caller.

#### Binary safety

A `string` is length-prefixed rather than NUL-terminated, so it can hold
arbitrary bytes — a decoded JPEG, a raw digest. These four functions are
declared to receive and return the string whole rather than as a C pointer, so
the bytes after a NUL survive:

```dex
let bytes: string = crypto.base64Decode("AAECA/8A")   // 00 01 02 03 FF 00
bytes.len()                                            // 6, not 0
crypto.base64Encode(bytes)                             // back to "AAECA/8A"
```

Most stdlib functions are not like this: they take the text up to the first NUL,
which is right for text and wrong for bytes. Where it matters, the table says
"binary-safe".

#### OpenSSL

`sha256Hex`, `hmacSha256Hex`, `hmacSha256B64Url`, `hmacSha1Hex` and `totp`
need OpenSSL, found with `pkg-config` the same way `wss://` finds it. Without
it the module still builds and every call to those returns an empty string
after saying so on stderr — so a program that
only mentions a digest on a path it never takes still runs.

```dex
crypto.sha256Hex("")
// e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
```

### file

Read and write files. `read`/`write`/`append` work in text and stop at the first
NUL byte; the `*Bytes` pair is binary-safe and is what an upload wants.

```dex
import "file"

let text: string = file.read("notes.txt")
file.write("notes.txt", "hello")

file.mkdirp("uploads/2026/08")
file.writeBytes("uploads/2026/08/photo.jpg", crypto.base64Decode(encoded))
file.size("uploads/2026/08/photo.jpg")
```

| Function | Signature | Description |
|---|---|---|
| `read` | `read(path: string): string` | Whole file as text. Stops at a NUL |
| `write` | `write(path: string, content: string): bool` | Overwrite with text |
| `append` | `append(path: string, content: string): bool` | Append text |
| `readBytes` | `readBytes(path: string): string` | Whole file. Binary-safe |
| `writeBytes` | `writeBytes(path: string, content: string): bool` | Overwrite. Binary-safe |
| `appendBytes` | `appendBytes(path: string, content: string): bool` | Append. Binary-safe |
| `size` | `size(path: string): int` | Bytes, or `-1` if unreadable |
| `exists` | `exists(path: string): bool` | Is there a file there |
| `remove` | `remove(path: string): bool` | Delete it |
| `rename` | `rename(oldPath: string, newPath: string): bool` | Rename |
| `move` | `move(src: string, dst: string): bool` | Move |
| `mkdirp` | `mkdirp(path: string): bool` | Create a directory and its parents |
| `sha256Hex` | `sha256Hex(path: string): string` | SHA-256 of the contents, streamed. Needs OpenSSL |
| `putUrl` | `putUrl(path: string, url: string, headers: string): int` | PUT the file's bytes to a URL; returns the HTTP status |

`putUrl` sends the bytes straight from disk to the socket without their ever
becoming a `string`, which is what makes uploading an arbitrary binary — to S3,
say — possible. `headers` is the newline-separated `Key: Value` block the
`http` client functions take, and `sha256Hex` gives the payload hash that
AWS SigV4 asks for:

```dex
let digest: string = file.sha256Hex(path)
let headers: string = http.header("", "x-amz-content-sha256", digest)
headers = http.header(headers, "Authorization", signature)
let status: int = file.putUrl(path, url, headers)
```

### io

Read user input from stdin.

```dex
import "io"

io.prompt("Enter name: ")
let name: string = io.readLine()

io.prompt("Enter age: ")
let age: int = io.readInt()
```

| Function     | Signature                       | Description                                         |
|--------------|---------------------------------|-----------------------------------------------------|
| `prompt`     | `prompt(message: string): void` | Print a message without newline and flush stdout     |
| `readLine`   | `readLine(): string`            | Read a line from stdin (strips trailing newline)     |
| `readInt`    | `readInt(): int`                | Read and parse an integer from stdin                 |
| `readDouble` | `readDouble(): double`          | Read and parse a double from stdin                   |
| `readBool`   | `readBool(): bool`              | Read a boolean from stdin ("true"/"false", case-insensitive) |

### os

System interaction: environment variables, process control, command execution.

```dex
import "os"

let home: string = os.env("HOME")
let result: ExecResult = os.exec("ls -la")
fmt.println(result.output)
os.exit(0)
```

`os.exec` returns an `ExecResult` struct:

```dex
struct ExecResult {
  exitCode: int
  output: string
  error: string
  timedOut: bool
}
```

`os.execWith` runs a command under `ExecOptions`, the way `http.requestWith`
runs a request under `HttpOptions`. Every field's zero value is its default, so
`os.ExecOptions {}` behaves exactly like `os.exec`:

```dex
struct ExecOptions {
  timeoutMs: int      // 0, the default, waits for ever
  appendTo: string    // "", the default, returns output and error
}

let r: os.ExecResult = os.execWith("docker build -t app .", os.ExecOptions {
    timeoutMs: 600000,
    appendTo: "build.log"
})
if (r.timedOut) {
    fmt.println("the build took over ten minutes and was stopped")
}
```

**`appendTo`** writes stdout and stderr to the file as they arrive, interleaved
in the order the command wrote them, so another thread can read the file while
the command runs. They are not also returned: `output` and `error` stay empty,
and a long build's output is held nowhere but the file. A file that cannot be
opened gives an exit code of `-1` and says so in `error`.

**`timeoutMs`** stops the command at the deadline, and everything it started
with it: the command runs in a process group of its own, which receives
`SIGTERM` and, two seconds later, `SIGKILL`. Without that, `sh -c "docker build"`
would leave docker running under a shell that had been stopped. A timed-out
command has `timedOut` set and an exit code of `124`, the code `timeout(1)` uses.

| Function | Signature                       | Description                                        |
|----------|---------------------------------|----------------------------------------------------|
| `args`   | `args(): string[]`              | Command-line arguments, excluding the program name |
| `env`    | `env(name: string): string`     | Read an environment variable (empty string if unset) |
| `exit`   | `exit(code: int): void`         | Exit the process with the given status code        |
| `exec`   | `exec(command: string): ExecResult` | Run a shell command and return the result      |
| `execWith` | `execWith(command: string, options: ExecOptions): ExecResult` | The same, with a deadline and output appended to a file as it arrives |

### Command-line arguments

```dex
import "os"

let a: string[] = os.args()
if (a.len() == 0) {
    fmt.println("usage: tool <path>")
    os.exit(1)
}
let path: string = a[0]
```

`args()` excludes argument zero, which is the path the program was invoked by
rather than something the caller typed. A program that never calls it is
generated exactly as before &mdash; `main` only takes the argument vector when
something asks for it.

### num

Explicit numeric conversion. Widening is deliberately **not** implicit for a
computed value — `let b: long = a` where `a` is an `int` is a type error, so
that a narrowing or a loss of precision is always something somebody wrote
down. A *literal* still widens on its own, because its type is chosen by where
it is used:

```dex
let a: long = 42        // fine: a literal takes the type of its context
let b: long = 'A'       // fine, same reason

let n: int = 42
let c: long = n         // error: expected long, got int
let d: long = num.toLong(n)   // this is how you say it
```

```dex
import "num"

num.toInt('7') - num.toInt('0')   // 7, by character code
num.toLong(s.len())               // len() is an int
num.toChar(65)                    // 'A'
num.toInt(3.99)                   // 3, truncated toward zero
```

| Function   | Signature                                   | Description |
|------------|---------------------------------------------|-------------|
| `toInt`    | `toInt(value: int\|long\|double\|char): int`     | To `int`. Truncates toward zero |
| `toLong`   | `toLong(value: int\|long\|double\|char): long`   | To `long`. Truncates toward zero |
| `toDouble` | `toDouble(value: int\|long\|double\|char): double` | To `double` |
| `toChar`   | `toChar(value: int\|long\|char): char`          | To `char`, by character code |

`toChar` does not accept a `double`: there is no character code for one, and
rounding it silently would be a conversion nobody asked for.

A `double` outside the target's range saturates to its largest or smallest
value, and NaN becomes `0`, so a number parsed from untrusted input cannot
produce an arbitrary integer. A `long` narrowed to `int` keeps its low 32 bits,
as a conversion does in Go.

### str

Type conversion utilities for converting between strings and numeric/boolean types.

```dex
import "str"

let s: string = str.fromInt(42)       // "42"
let n: int = str.toInt("42")          // 42
let d: double = str.toDouble("3.14")  // 3.14
let b: string = str.fromBool(true)    // "true"
```

| Function     | Signature                          | Description                           |
|--------------|------------------------------------|---------------------------------------|
| `fromInt`    | `fromInt(n: int): string`          | Convert an integer to string          |
| `fromLong`   | `fromLong(n: long): string`        | Convert a long to string              |
| `fromDouble` | `fromDouble(d: double): string`    | Convert a double to string            |
| `fromBool`   | `fromBool(b: bool): string`        | Convert a boolean to `"true"`/`"false"` |
| `toInt`      | `toInt(s: string): int`            | Parse a string as an integer          |
| `toLong`     | `toLong(s: string): long`          | Parse a string as a long              |
| `toDouble`   | `toDouble(s: string): double`      | Parse a string as a double            |

---

## String Escape Sequences

| Sequence | Character      |
|----------|----------------|
| `\n`     | Newline        |
| `\t`     | Tab            |
| `\\`     | Backslash      |
| `\"`     | Double quote   |

### reflect

Reads and writes a struct's fields by position, so a function can walk a value
whose shape it does not have written down — a row mapper, a validator, an audit
diff, a debug dump.

```dex
import "reflect"

struct User {
    id: long
    email: string
    score: double
    active: bool
}

fn main(): void {
    let unit: User = User { id: 42, email: "ada@example.com",
                            score: 150.0, active: true }

    let i: int = 0
    while (i < reflect.fieldCount(unit)) {
        fmt.println(reflect.fieldName(unit, i) + " (" +
                    reflect.fieldKind(unit, i) + ") = " +
                    reflect.toString(unit, i))
        i = i + 1
    }
}
```

```
id (long) = 42
email (string) = ada@example.com
score (double) = 150
active (bool) = true
```

| Function | Returns | Description |
|----------|---------|-------------|
| `reflect.typeName(value)` | `string` | The struct's type name. A compile-time constant. |
| `reflect.fieldCount(value)` | `int` | How many fields the struct declares. A compile-time constant. |
| `reflect.fields(value)` | `string[]` | Field names in declaration order. Allocates; prefer `fieldName` in a loop. |
| `reflect.fieldName(value, index)` | `string` | Name of the field at this index, `""` if out of range. |
| `reflect.fieldKind(value, index)` | `string` | `"int"`, `"long"`, `"double"`, `"bool"`, `"string"`, `"char"`, `"struct"`, `"array"`, or `"other"` for a field reflection cannot read (a map, a `json.Value`, an enum); `""` if out of range. |
| `reflect.fieldTag(value, index, key)` | `string` | The value this field's tag gives for `key`, or `""` if the field has no tag or no such key. See [Field tags](#field-tags). |
| `reflect.fieldTags(value, key)` | `string[]` | Every field's tag value for `key`, in declaration order, `""` where a field has none. The array form of `fieldTag`. |
| `reflect.indexOf(value, name)` | `int` | Index of the named field, `-1` if absent. |
| `reflect.has(value, name)` | `bool` | Whether the struct declares that field. |
| `reflect.asInt(value, index)` | `int` | The field as an `int`, `0` if the index is out of range or the field is another type. |
| `reflect.asLong(value, index)` | `long` | As above, for `long`. |
| `reflect.asDouble(value, index)` | `double` | As above, for `double`. |
| `reflect.asBool(value, index)` | `bool` | As above, for `bool`. |
| `reflect.asString(value, index)` | `string` | As above, for `string`. |
| `reflect.toString(value, index)` | `string` | The field rendered as text whatever its type. `""` for a `struct` or `array` field — use `json.encode` for those. |
| `reflect.setInt(value, index, to)` | `bool` | Write an `int` field. `false` without writing if the index is out of range or the field is another type. |
| `reflect.setLong(value, index, to)` | `bool` | As above, for `long`. |
| `reflect.setDouble(value, index, to)` | `bool` | As above, for `double`. |
| `reflect.setBool(value, index, to)` | `bool` | As above, for `bool`. |
| `reflect.setString(value, index, to)` | `bool` | As above, for `string`. The struct takes its own reference and releases what the field held, so the value may be borrowed or freshly built. |

The subject may be a struct value or a `&T`, so a helper that takes its argument
by reference reads the same as one that takes it by value:

```dex
fn describe(c: &User): string {
    let sb = StringBuilder()
    let i: int = 0
    while (i < reflect.fieldCount(c)) {
        sb.append(reflect.fieldName(c, i))
        sb.append("=")
        sb.append(reflect.toString(c, i))
        sb.append(";")
        i = i + 1
    }
    return sb.toString()
}
```

A nested struct field and an array element are each subjects in their own right:
`reflect.asString(unit.detail, 0)`, `reflect.asString(people[0], 1)`.

#### Describing a struct to a function that has no type for it

A function cannot take "any struct" as a parameter — a type parameter binds to
one concrete type per instantiation, not to "any struct" —
so the generic half of a metadata-driven library cannot be written as an
ordinary function. `fields()` and `fieldTags()` close that gap: together they
hand a plain function over two `string[]` everything it needs, and a function
over two string arrays needs no polymorphism at all.

```dex
// Serves every model in the program. No struct type in its signature.
fn projection(names: string[], tags: string[], alias: string): string[] {
    let out: string[] = []
    let i: int = 0
    while (i < names.len()) {
        if (tags[i] == "-") {
            // not selected
        } else if (tags[i].isEmpty()) {
            out.push(alias + "." + snake(names[i]))
        } else {
            out.push(tags[i])
        }
        i = i + 1
    }
    return out
}

// The one place that knows which struct this is, names it once.
let cols: string[] = projection(reflect.fields(row), reflect.fieldTags(row, "db"), "i")
```

Tag keys are the library's to define — `"-"` meaning "skip this field" above is
a convention of that `projection` function, not something the compiler knows.

#### Writing fields

The `set*` functions are the `as*` accessors with the assignment the other way
round. They exist for the shape that motivated them — filling a struct from a
result set, a form, or anything that hands over values in declaration order —
so that one loop replaces one assignment per field per struct:

```dex
fn fill(out: &Pending, rows: int): void {
    let i: int = 0
    while (i < reflect.fieldCount(out)) {
        let kind: string = reflect.fieldKind(out, i)
        if (kind == "int") {
            reflect.setInt(out, i, db.colInt(rows, i))
        } else if (kind == "long") {
            reflect.setLong(out, i, db.colLong(rows, i))
        } else if (kind == "string") {
            reflect.setString(out, i, db.colStr(rows, i))
        }
        i = i + 1
    }
}
```

Each returns whether it wrote. A setter whose index is out of range, or which
names a field of another type, writes nothing and reports `false` — a read in
that position returns a zero value, but a write that silently did nothing would
never be noticed, so it is reported instead.

A write is held to more than a read, because a read only has to be able to see
the subject while a write changes it. Three subjects the compiler refuses:

```dex
fn bad(p: Pending): void { reflect.setString(p, 4, "v") }  // by-value parameter
reflect.setString(list[0], 4, "v")                         // array element
reflect.setString(makePending(), 4, "v")                   // temporary
```

A **by-value struct parameter** is a copy that shares the caller's string
pointers without owning them, so writing one releases a reference the caller
still holds — and even for a value field the caller never sees the write. Take
the subject as `&Pending` instead, which is the form that means what was
intended. An **array element** has no in-place write in the language at all
(`list[0].currency = "v"` does not parse), and reflection is not a back door
into it: bind the element to a variable, write that, and put it back. A
**temporary** owns its heap fields and nobody would be left to release them.

A field of a *local* struct is fine, including a nested one —
`reflect.setString(outer.inner, 0, "v")` — because a local owns what it holds.

#### What it costs

Reflection here is not the usual trade of speed for flexibility, because the
metadata is generated rather than discovered.

**Nothing is emitted unless it is used.** The compiler records which structs a
`reflect` call names and emits a name table, a kind table and typed accessors
for exactly those. A program that does not import `reflect` is byte-for-byte
what it was without the module; a program that reflects over one struct carries
one struct's worth.

**A known field costs what a field access costs.** `fieldCount` and `typeName`
become literals. `indexOf` with a literal name resolves at compile time, so

```dex
reflect.asString(unit, reflect.indexOf(unit, "email"))
```

compiles to a constant index into a `switch` — the same load as
`unit.email`, plus the retain that ownership requires. Only a name computed
at runtime searches the name table, and that search is the one cost reflection
actually adds.

**A mismatched read cannot misread memory.** Each accessor is generated with a
case for the fields of its own type only, so `asLong` on a `string` field falls
through to the default and returns `0` rather than reinterpreting a pointer as
an integer. There is no offset arithmetic anywhere in the generated code.

#### Reading a string field mints a reference

`asString` and `toString` return an owned `string`, like any other call — the
accessor retains the struct's field rather than handing back a borrowed pointer,
so the ordinary release at the end of the statement is correct and the struct
keeps its own reference.

#### Limits

Reflection is **read-only**. There are no setters, so a struct cannot yet be
filled in field by field from reflection — writing would have to release the old
value and retain the new one through a generated setter, and that is deliberately
left for a later version rather than bolted on.

The subject must be a variable, field, or element, not a temporary:

```dex
let n: int = reflect.fieldCount(build())   // error: subject must not be a temporary
let u: User = build()
let n: int = reflect.fieldCount(u)         // fine
```

A temporary owns its heap fields, and reflecting one would leave nobody to
release them. Binding it to a variable first makes that ownership visible.

---

## Built-in Functions

### assert

```dex
assert(condition: bool)
```

If the condition is `false`, prints a failure message to stderr and exits with code 1. Used for testing.

```dex
assert(1 == 1)        // passes
assert(x > 0)         // passes if x > 0
```

The message carries the file, the line, and the condition as it was written, because a test file is a `main()` full of asserts and "assert failed" on its own meant bisecting by hand to find which one:

```
FAIL: examples/generics_test.dx:84: assert(identity(42) == 42)
```

### send, receive, channel, close

See [Concurrency](#concurrency) section.

---

## Module-Scoped Types

A type declared in a user module belongs to that module, the way a function
does. Two modules can each declare a `Response`, and they are two types:

```dex
// model/charging.dx
struct Response(ok: bool) {}

// model/billing.dx
struct Response(code: int) {}
```

```dex
import "model/charging"
import "model/billing"

let a: charging.Response = charging.make()
let b: billing.Response = billing.make(404)
```

A reference resolves in this order:

1. **Qualified** — `charging.Response` always names that module's type.
2. **Its own module's** — inside `charging`, a bare `Response` is charging's,
   even if an imported module declares one too. The same rule a function
   follows: inside a module you call `make()`, not `charging.make()`.
3. **Unqualified, one declarer** — a bare `Response` resolves when exactly one
   module in scope declares it. This is why existing code needs no changes.
4. **Unqualified, several declarers** — an error naming them:

```
app.dx:6:21: ambiguous type 'Response': declared by 'charging.Response' and
'billing.Response' — qualify it, as in charging.Response
```

Diagnostics and editor hovers show the qualified spelling, so a type reads the
way it was written rather than by the flat name it registers under.

The struct types a stdlib module provides — `http.HttpRequest`, `ws.Conn` — are
not module-scoped and keep working under either spelling.

## Formatting

`dex format` lays a file out the one canonical way. There are no options and
nothing to configure: layout stops being a thing anybody decides, the way
`gofmt` and Prettier settled it for Go and JavaScript.

```
dex format                 # every .dx file under the current directory
dex format src/ main.dx    # named files, and the .dx files under named directories
dex format --check         # list what is unformatted and exit non-zero; nothing is written
```

What it decides:

- **Four spaces per level**, and no trailing whitespace. Exactly one newline
  ends the file.
- **One space where a space belongs** — around binary operators, after a comma
  or a colon — and none where it does not: `f(x)`, `a.b`, `-x`, `xs[0]`,
  `Box<int>`. A struct or object literal is spaced inside its braces,
  `Point { x: 1 }`; a call or an index is not.
- **A line break is a consequence of the line not fitting**, never of where the
  last person pressed return. Anything that fits in 120 columns is one line. A
  call, array or literal that does not fit puts one element per line. An
  expression with no group to open breaks at its loosest-binding operator, and
  its operands line up with each other.
- **A method chain that does not fit breaks at its dots**, one `.method(..)`
  per line, one level in from the line its head is on. The head is everything
  before the first call that is chained onto another call, so `q.from("books")`
  stays together. It takes at least two such links, and a chain that fits stays
  on one line:

  ```dex
  let byAuthor: q.Query = q.from("books")
      .columns(["title"])
      .where(q.col("author_name").eq(q.param("who")))
      .build(LITE)
  ```
- **One blank line between declarations**, one after the import block, and at
  most one anywhere else. A comment directly above a declaration stays attached
  to it, with the blank line going above the comment.
- **A body is always a block.** `fn f() { return 1 }` becomes three lines; an
  empty one stays `{}`.

Formatting is editor-ready: the language server answers
`textDocument/formatting`, so format-on-save works with no extra configuration.
A file that does not parse is left exactly as it is rather than half-formatted.

The formatter works on the token stream rather than the AST, and checks itself:
it re-reads its own output and compares the two token streams before writing
anything, so a bug in it is a layout bug and never a lost line. A file it cannot
account for is reported and left alone.


## Calling C

An `external module` block declares C functions to call, the headers they
need, and the libraries to link against. The block *is* the declaration, so
there is no import:

```dex
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

A call compiles to a direct call to the C symbol — no wrapper, no marshalling
layer. `include` takes a system header in angle brackets or a bare name for one
beside the source; the directory holding the `.dx` file is on the include path,
so a header you wrote is found without configuration. In a library the header
is looked for beside the library's own file, wherever the program importing it
lives, so a library can ship the small C shims it needs next to its `.dx`. `link` takes `-l<name>`,
`-L<path>`, or a bare library name; anything else is refused, because an
external block that could pass arbitrary flags to the C compiler would be code
execution at build time.

A library outside the default search paths — everything Homebrew installs on
Apple Silicon, under `/opt/homebrew` — needs its header path as well as its
library path. `pkg` asks `pkg-config` for both, so the block does not have to
know where the library lives on whichever machine builds it:

```dex
external module ossl {
    include "<openssl/crypto.h>"
    pkg "openssl"

    fn hex(buf: string, len: long): owned string freed by "OPENSSL_free" = "OPENSSL_buf2hexstr"
}
```

Only the flags a library needs are passed on — `-I`, `-L`, `-l`, `-D`,
`-pthread` and `-framework` — and anything else `pkg-config` prints is an
error, for the same reason `link` is restricted. A library `pkg-config` does
not know is reported at the block, naming it.

### It is the C ABI, not the C language

Nothing here requires the library to be written in C — only that it exposes
C-ABI symbols. Anything that can do so can be bound, and a C++ library wrapped
in `extern "C"` is the common case:

```cpp
// counter.hpp
extern "C" {
typedef struct CounterOpaque CounterOpaque;
CounterOpaque* counter_new(int start);
void counter_free(CounterOpaque* c);
int counter_bump(CounterOpaque* c, int by);
}
```

```dex
external module counter {
    include "counter.hpp"
    link "-lcounter"
    link "-lc++"

    type Counter closed by "counter_free"

    fn make(start: int): Counter? = "counter_new"
    fn bump(c: Counter, by: int): int = "counter_bump"
}
```

The C++ object is held as a handle, so `delete` runs through `counter_free`
when the last reference goes — Dex's reference counting driving a C++
destructor. Rust works the same way (`#[no_mangle] pub extern "C"`, built as a
`staticlib`), as do Zig's `export fn`, Swift's `@_cdecl`, and Go built with
`-buildmode=c-archive`.

What does not cross is anything the C ABI has no word for: C++ classes,
methods, templates, overloads and exceptions, Rust traits and generics. Those
need a flat `extern "C"` shim on the far side, which is also the only form the
declaration syntax here can describe.

#### With no header at all

A library in another language often has no header to point at — a Rust
staticlib exports C-ABI symbols and nothing else. Name no `include` and the C
functions are declared from the signatures written in Dex:

```dex
external module ml {
    link "-lmathlib"

    fn add(a: int, b: int): int = "ml_add"
    fn name(): string = "ml_name"
}
```

```c
extern int ml_add(int, int);
extern const char* ml_name(void);
```

Naming a header is still the better path and stays the default, because the C
compiler then cross-checks your declaration against the real one and a
signature that disagrees is an error rather than a silent ABI mismatch.
Declaring no header is the opt-out: the signature is taken at its word, and
getting it wrong is an ABI mismatch nothing will catch.

The declared name need not match the C symbol. `= "symbol"` names it, which is
how a C function whose name reads badly in Dex gets a better one:

```dex
fn absInt(n: int): int = "abs"
```

### What can cross

`int`, `long`, `double`, `bool`, `char` and `string`, plus `void` as a return
type. Anything else is refused by name rather than left to the C compiler:

```
z.f() parameter 'a' is string[], which cannot cross the C boundary yet —
only int, long, double, bool, char and string can
```

Also an **opaque handle**, declared in the block — see below — and a scalar or
handle **written by C** through an out-parameter, declared `&T`. Containers and
callbacks are not supported: a callback cannot be unwound through, because
`throw` is `longjmp` and a C frame in between would have its cleanup skipped.

### Who owns a returned string

A `string` parameter crosses as a `const char*`. A `string` **return** is the
one place an external declaration has to say something, because C has two
conventions and guessing wrong corrupts the heap rather than merely leaking:

| Return type | The C function | What Dex does |
|-------------|----------------|---------------|
| `string` | returns a pointer it keeps — `getenv`, `strerror`, `zlibVersion` | copies the bytes, leaves the pointer alone |
| `owned string` | `malloc`'d the buffer and hands it over — `strdup` | copies the bytes, then frees the original |
| `owned string freed by "f"` | allocated it with its own allocator — `OPENSSL_buf2hexstr` | copies the bytes, then calls `f` on the original |

```dex
external module cstr {
    include "<string.h>"

    fn errorText(n: int): string = "strerror"        // copied
    fn dup(s: string): owned string = "strdup"       // taken over and freed
}
```

The default is the safe one, because the pointer-it-keeps convention is the
common one and freeing such a pointer corrupts the heap rather than leaking.

`owned` is a modifier on the return type rather than an annotation, for two
reasons. `#[owned]` already means something else — on a variable it forbids
aliasing — and a marker that decides whether `free` is called is semantics
rather than the advice an annotation carries. It is also **soft**: `owned` is
only a keyword in this position, so it stays usable as an ordinary name.
`owned` on anything but a string return is an error, since it has nothing to
say about an `int`.

Many libraries hand back memory that plain `free` must not touch, because they
allocated it themselves: sqlite's `sqlite3_free`, libxml2's `xmlFree`,
OpenSSL's `OPENSSL_free`. `freed by` names the function that does:

```dex
external module ossl {
    include "<openssl/crypto.h>"
    pkg "openssl"

    fn hex(buf: string, len: long): owned string freed by "OPENSSL_free" = "OPENSSL_buf2hexstr"
}

let h: string = ossl.hex("ab", 2)   // "61:62"
```

The function is called once per call, after the bytes are copied, and not at
all for a NULL return, which arrives as `""`. It may be a macro — `OPENSSL_free`
is one — because the call is emitted inline rather than taken as a function
pointer. `freed by` without `owned` is an error: without `owned` Dex never frees
the string, so there is nothing to free it with.

### Bytes, and why there is no raw marker

A `string` crosses as a NUL-terminated `const char*`, so a C function that
calls `strlen` on it stops at the first NUL. That is not a limitation to work
around: a C function handling arbitrary bytes already takes a pointer and a
length, so pass the length.

```dex
external module cstr {
    include "<string.h>"
    fn compare(a: string, b: string, n: int): int = "memcmp"
    fn length(s: string): long = "strlen"
}

let a: string = crypto.base64Decode("YWIAY2Q=")   // five bytes, NUL at index 2
assert(a.len() == 5)
assert(cstr.length(a) == 2)                        // strlen stops at the NUL
assert(cstr.compare(a, b, 5) != 0)                 // memcmp sees past it
```

`.len()` is the byte length, so nothing truncates. This is why there is no
`#[raw]` marker for parameters, though the stdlib has one internally: a stdlib
C helper is compiled into the same translation unit and can take a `DexString*`
directly, while a library you link against has no access to that layout. The
pointer-and-length form is the only one that could work, and it is the form
those functions are already written in.

### Opaque handles

Most C libraries are handle-based — `FILE*`, `sqlite3*`, `z_stream*`. Declare
one with the function that closes it:

```dex
external module cio {
    include "<stdio.h>"

    type File closed by "fclose"

    fn open(path: string, mode: string): File? = "fopen"
    fn readByte(f: File): int = "fgetc"
}
```

A handle is a pointer the program passes around and never dereferences. There
is no way to read through one, construct one, or do arithmetic on it; the only
thing that can be done with a handle is hand it back to the module that made
it.

**It is refcounted, not move-only.** The pointer itself gets no header — it
came from a C library and may not be memory we allocated — so it is held in a
box, which is an ordinary refcounted object. The closing function is the box's
destructor, which means it runs when the last reference goes: at scope exit,
when the variable is overwritten, when the container holding it is released,
and when an exception unwinds past the frame. That last one is the case a
handle most needs, and it is why refcounting rather than move semantics: a
`defer` can be forgotten, and this cannot.

Rust reaches move-only semantics here, but because move is its default for any
type with a destructor, not because handles call for it. Refcounting is this
language's default, so a handle follows it and no new ownership rule has to be
invented or enforced.

**A failed open is `null`.** A C function returning a handle usually returns
NULL on failure, so declare the return optional and NULL arrives as `null`
rather than a box wrapping nothing:

```dex
let f: File? = cio.open(path, "r")
if (f != null) {
    let b: int = cio.readByte(f)   // narrowed to File
}
```

A handle is a pointer, so the optional needs no extra representation: NULL is
the absent value, which is the same thing the C function already meant by it.

A return declared without the `?` is a promise that C never returns NULL. If it
does, the program stops at that call with
`runtime error: cio.open returned NULL for a handle declared non-optional`,
rather than carrying the NULL into the next C function that receives it.

#### What refcounting does not solve

Refcounting tracks how many references a handle has, not what it depends on. A
`sqlite3_stmt*` that outlives the `sqlite3*` it was prepared on is a
use-after-free, and keeping the statement alive will not keep the connection
alive. Rust expresses that with lifetimes; there is no way to say it here. So
nested handles remain a sharp edge, and the language will not catch it for you.

### Wrapping a C library

Most programs should never contain an `external module`. A library does it
once, in its own file, and exports ordinary Dex: a struct that holds the C
handle, and functions that take it. The external module is private to that
file, so programs using the library never see the C names, headers or link
flags — they import the library and call Dex functions.

```dex
// modules/sqlite.dx — the library
external module c {
    include "<sqlite3.h>"
    pkg "sqlite3"

    type Db closed by "sqlite3_close"
    fn open(path: string, db: &Db?): int = "sqlite3_open"
    // ...
}

struct Database {
    handle: Db?
}

fn open(path: string): Database {
    let db: Db? = null
    c.open(path, db)
    return Database { handle: db }
}
```

```dex
// a program using it
import "modules/sqlite"

let db: sqlite.Database = sqlite.open("app.db")
```

A handle can be a struct field like any heap value: the struct holds a
reference, copies of the struct share the connection, and it is closed when the
last one goes. `examples/modules/sqlite.dx` is the full library and
`examples/externalLibrary_test.dx` uses it.

### Out-parameters

Many C functions return a result through a pointer argument: `sqlite3_open`
writes the connection into a `sqlite3**`, `frexp` writes the exponent into an
`int*`. Declare such a parameter with `&`, the same spelling a Dex function uses
for a parameter it writes, and pass a variable. As with any `&` parameter, the
call site writes no `&` of its own:

```dex
external module sq {
    include "<sqlite3.h>"
    pkg "sqlite3"

    type Db closed by "sqlite3_close"
    type Stmt closed by "sqlite3_finalize"

    fn open(path: string, db: &Db?): int = "sqlite3_open"
    fn prepare(db: Db, sql: string, n: int, stmt: &Stmt?, tail: &Stmt?): int = "sqlite3_prepare_v2"
}

let db: Db? = null
let rc: int = sq.open("app.db", db)     // C writes the connection into db
```

What may be written is `&int`, `&long`, `&double`, `&bool`, `&char`, or a
handle — `&H` or, usually, `&H?`, since a C function may write NULL. A scalar
crosses as the variable's address, so a header still checks its type. A handle
is written by C as a raw pointer and then boxed into the variable exactly as an
assignment would store it: what the variable held before is released, and the
new handle is closed by its closing function when its last reference goes. A C
function that writes nothing leaves the variable `null`.

The argument must be a variable of exactly the declared type — not an
expression, not a `const`, and not one that has been narrowed from `Db?` to
`Db` inside a null check, because C may write NULL into it. An `int` variable
cannot stand in for a `&long` either: C writes the representation it was
declared with, and a wider variable would be half written.

`null` passes NULL, which is how a C function is told to skip an output the
caller does not want — `sqlite3_prepare_v2`'s `pzTail` above is the usual case.
A string cannot be written yet, because the declaration has no way to say who
allocated what C wrote.

### A known wart

A `string` crosses as `char*`. A C function declared to take `unsigned char*`
or `Bytef*` — zlib's `crc32` is one — compiles and works but draws a
`-Wpointer-sign` warning, because the signedness differs even though the
representation does not. There is no unsigned-char type to declare instead.

### What is checked

An external declaration is validated where it is written rather than at the
call site: a duplicate module or function, a name a stdlib module already has,
a malformed header, a disallowed link flag, a type that cannot cross, and a
C symbol — a `= "symbol"`, a `closed by` or a `freed by` — that is not a C
identifier. Those names are pasted into the generated C as written, so anything
wider would be code run at build time. The
signature is then checked at every call exactly as a stdlib function's is.

One thing is left to the C compiler and deliberately so: whether the C function
actually exists and has the signature you declared. `-Werror=implicit-function-declaration`
makes a missing header fatal, and a declaration that disagrees with the real one
is an incompatible-pointer error rather than a warning.

### Where a block may be written

In the main program or in a user module, and it is visible **only in the file
that declares it**. Two files can each declare an `external module cstr`, even
binding different C functions under the same names, and each reaches its own. A
file that wants C functions another file declared declares them too; a library
that wants to share a set of bindings puts them behind ordinary Dex functions in
a module and has its users import that.

That is what keeps a library's C bindings its own business. Without it, every
program that imported a library could call the library's raw C functions
directly, and two libraries that happened to pick the same module name would
break any program that used both.

---

## Linting

Lint reports what compiles but is probably not what was meant. It never
changes what is emitted, and a build reports it by default — the moment a
mistake is cheapest to hear about. Findings do not fail a build unless asked:

```
dex build <file.dx>             # build and report findings
dex build <file.dx> --no-lint   # build without them
dex build <file.dx> --lint=strict   # ... fail the build if anything is reported
dex lint <file.dx>              # front end only: no C generated, no cc run
dex lint --rules                # list the rules
dex build <file.dx> --lint-off=unused-param,shadow
```

`dex test` is the exception: it builds every test file, and a suite's output is
for its failures, so it reports findings only when given `--lint`.

The language server reports the same findings as you type, graded by whether
something is actually wrong. A warning is for a `throws` the body contradicts,
a `catch` that can never run, code nothing reaches, or an `#[allow(...)]`
naming no rule. Everything else is a hint — a quiet grey mark rather than a
squiggle.

`uncaught-throw` is a hint for that reason. An editor asks for every diagnostic
on every keystroke; since `throws` is
an optional declaration, every `throw` in a codebase that has not adopted it
would otherwise be underlined as though it were broken.

| Rule | Reports |
|------|---------|
| `unused-var` | a local or loop variable that is never read |
| `unused-param` | a parameter that the function body never reads |
| `unused-import` | an imported module that is never referenced |
| `unused-function` | a private function that nothing calls |
| `shadow` | a declaration that hides a name from an enclosing scope |
| `unreachable` | a statement after a `return`, `throw`, `break` or `continue` |
| `uncaught-throw` | an exception that escapes a function not declared `throws` |
| `undeclared-throw` | an exception type a `throws` declaration leaves out |
| `unnecessary-throws` | a `throws` declaration naming a type the function cannot throw |
| `unreachable-catch` | a `catch` clause that can never run |
| `unknown-allow` | an `#[allow(...)]` naming a rule that does not exist |
| `struct-padding` | a struct declared in an order the compiler rearranges to avoid padding |

### Struct size and field order

The compiler lays a struct out with its widest fields first, so the order you
declare fields in never costs padding: `struct Reading { ok: bool, at: long,
stale: bool }` is 16 bytes, laid out as `at, ok, stale`, where declared order
would be 24. Everything you can observe keeps declared order — a struct
literal, the auto-constructor's parameters, `json.encode`, printing,
`reflect` field indexes and `db.fill` — only the memory layout changes. A
standard library struct keeps its declared layout, which its C runtime relies
on.

Hovering a struct's name shows its size, alignment and padding, and when the
layout differs from declared order, the order used and what declared order
would have cost. `struct-padding` reports that difference as a finding, a hint
in the editor, for anyone who wants the source to read in layout order;
`#[allow(struct-padding)]` on the struct keeps an order chosen for another
reason. Sizes are for a 64-bit target.

### Suppressing a Finding

`#[allow(<rule>)]` on the declaration it fires on silences one rule there.
`#[allow(unused)]` covers the four `unused-*` rules and `#[allow(all)]` covers
everything:

```dex
#[allow(unused)]
let reserved = buildHandle()   // kept for its side effect, never read
```

An `#[allow(...)]` is the one annotation the compiler does not interpret: it
says what not to report, so it is accepted on any type rather than only on heap
types the way `#[owned]` is.

### Where an escaping exception is reported

Throwing is inferred exactly, but a finding follows a **declaration** rather
than the inference:

- An **undeclared** throw is reported where it is raised — once per type, at
  the one place that can fix it.
- Writing `throws` there is what tells its callers. A call to a function that
  declares it, unguarded and in a caller that does not declare it either, is
  the caller's finding; declaring it there moves the obligation one step
  further out, until something catches it.

That is the gradient Java gets from making the declaration mandatory, reached
by making the obligation follow the annotation instead. It also means a `throw`
used as an assertion stays one finding. Measured on a 211-file codebase: a
single guard in a shared query builder was reachable from 500 unguarded call
sites, and reporting each of them said nothing a reader could act on, because
the decision about where to catch is made once rather than 500 times.

### What It Does Not Do

- **Assignment is not use.** A variable that is only ever written to is reported
  as unused, which is the point — `let total = 0` followed by `total += n` and
  nothing else is dead arithmetic.
- **A bare `return` ends its block.** `return` with no value takes the next
  expression as its value, so nothing survives after it to be called
  unreachable. The rule fires after `return <value>`, `throw`, `break` and
  `continue`.
- **A module that declares types is never reported unused.** `import "json"`
  earns its keep through a `json.Value` annotation, and a type annotation is
  resolved while parsing — by the time lint runs there is no mention of the
  module left to find. Only function-only modules such as `str` and `math` are
  checked.
- **A call through a function value is assumed not to throw.** Which function a
  closure holds is a runtime fact, so the throw inference stops there rather
  than guessing.

---

## Testing

Test files use the `*_test.dx` naming convention. Use `assert()` to validate behavior.

```
dex test                   # discovers and runs all *_test.dx files in current directory
dex test myfile_test.dx    # runs a single test file
```

Each test file is compiled and executed. If the program exits with code 0, it passes. If `assert()` fails (or any non-zero exit), it fails.

Example test file (`math_test.dx`):

```dex
fn main(): void {
    assert(1 + 1 == 2)
    assert(10 % 3 == 1)
}
```

Output:

```
PASS: math_test.dx

1 passed, 0 failed
```

---

## Complete Example

```dex
import "fmt"
import "http"
import "json"

fn factorial(n: int): int {
  if (n <= 1) {
    return 1
  } else {
    return n * factorial(n - 1)
  }
}

fn handle_hello(): string {
  let scores: int[] = [10, 20, 30]
  scores.push(40)

  let obj: string = json.encode({
      message: "Hello from Dex!",
      count: scores.len(),
      scores: scores,
      success: true
  })
  return obj
}

fn main(): void {
  let result: int = factorial(5)
  fmt.println(result)

  http.route("GET", "/hello", "handle_hello")
  http.listen(8080)
}
```
