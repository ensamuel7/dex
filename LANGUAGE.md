# DexLang Language Reference

DexLang is a statically-typed language that compiles to C. It is designed for building web servers and applications. Source files use the `.dx` extension.

## Compilation

```
dex build <file.dx>       # compiles to native binary
dex run <file.dx>         # compiles and runs
dex test [file_test.dx]   # runs test files (*_test.dx)
```

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

Interface types can be used as function parameters. Any struct that satisfies the interface can be passed:

```dex
fn printGreeting(g: Greeter): void {
    fmt.println(g.greet())
}

let p = Person{name: "Alice"}
printGreeting(p)    // "Hello, I'm Alice"
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
    show(e)    // compile error: expected Stringer, got Empty
}
```

### How it works

Interfaces compile to C vtable structs. Each interface value holds a pointer to the underlying data (`_data`) and a table of function pointers (`_vtable`). The compiler wires up the vtable at the call site based on the concrete struct type. All dispatch is resolved at compile time — there is no runtime type metadata or reflection.

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
json.new()
```

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

DexLang provides lightweight concurrency with four primitives: `spawn`, `send`, `receive`, and `channel`.

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
- Only a single `catch` clause is supported, and it must use the `Exception` type.
- The `finally` block always executes, whether or not an exception was thrown.

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
| `listen`   | `listen(port: int, workers?: int): void`                       | Start the server on a port. Optional `workers` forks multiple processes via `SO_REUSEPORT` (0 = auto-detect CPU cores). |
| `response` | `response(statusCode: int, body: string, contentType: string): HttpResponse` | Create an HTTP response  |

Handler functions can take **0 parameters** (backward-compatible) or **1 parameter** of type `http.HttpRequest`. They must return a non-void type (typically `HttpResponse` or `string`).

**HttpRequest struct** (available to handlers taking a request parameter):

```dex
struct HttpRequest {
  method: string              // "GET", "POST", etc.
  path: string                // "/api/foo"
  body: string                // request body (empty string if none)
  query: string               // raw query string after '?' (empty if none)
  params: map[string,string]  // route parameters from :param segments
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
}
```

The `contentType` field is set automatically for client responses (from the server's `Content-Type` header). In server-side struct literals it is optional — if omitted, defaults to `"application/json"`.

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

**Headers:**

Headers can be provided in two formats:
- **JSON object** (most intuitive): build with `json.new()` / `json.set()` — auto-detected when the string starts with `{`
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
  let name: string = json.get(resp.body, "name")
  let count: int = json.getInt(resp.body, "count")
  let active: bool = json.getBool(resp.body, "active")

  // POST with JSON body
  let body: string = json.new()
  body = json.set(body, "name", "Alice")
  let r2: HttpResponse = http.post("https://api.example.com", body)

  // GET with headers (JSON approach)
  let headers: string = json.new()
  headers = json.set(headers, "Authorization", "Bearer token")
  headers = json.set(headers, "Accept", "application/json")
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

Build JSON objects from strings.

```dex
import "json"

let obj: string = json.new()
obj = json.set(obj, "name", "Dex")
obj = json.set(obj, "version", 1)
obj = json.set(obj, "stable", true)
obj = json.set(obj, "bignum", 1000000)
obj = json.set(obj, "pi", 3.14)
// obj is now: {"name":"Dex","version":1,"stable":true,"bignum":1000000,"pi":3.14}
```

| Function     | Signature                                                                       | Description                    |
|--------------|---------------------------------------------------------------------------------|--------------------------------|
| `new`        | `new(): string`                                                                 | Create empty `{}`              |
| `set`        | `set(obj: string, key: string, value: string\|int\|bool\|long\|double): string` | Set a value (type auto-detected) |
| `setArray`   | `setArray(obj: string, key: string, arr: T[]): string`                          | Set an array value             |
| `setObj`     | `setObj(obj: string, key: string, value: string): string`                       | Set a key to a raw JSON object/array value (not quoted) |
| `stringify`  | `stringify(value: T[]\|struct): string`                                         | Convert an array or struct to JSON string |
| `objectify`  | `objectify(json: string): T`                                                    | Convert a JSON string into a typed struct |
| `get`        | `get(json: string, key: string): string`                                        | Get a string value by key      |
| `getInt`     | `getInt(json: string, key: string): int`                                        | Get an integer value by key    |
| `getBool`    | `getBool(json: string, key: string): bool`                                      | Get a boolean value by key     |
| `getDouble`  | `getDouble(json: string, key: string): double`                                  | Get a double value by key      |
| `getLong`    | `getLong(json: string, key: string): long`                                      | Get a long integer value by key |
| `arrayNew`     | `arrayNew(): string`                                                          | Create empty JSON array `[]`   |
| `arrayLen`     | `arrayLen(json: string): int`                                                 | Get length of a JSON array     |
| `arrayGet`     | `arrayGet(json: string, index: int): string`                                  | Get element at index (strings unquoted) |
| `arrayGetRaw`  | `arrayGetRaw(json: string, index: int): string`                               | Get raw element at index (strings keep quotes) |
| `arrayPush`    | `arrayPush(arr: string, value: string\|int\|bool\|long\|double): string`      | Append a primitive value to a JSON array |
| `arrayPushObj` | `arrayPushObj(arr: string, obj: string): string`                              | Append a JSON object/array to a JSON array |

`set` accepts any primitive type as the value — the compiler dispatches to the correct implementation based on the argument type. `setArray` and `stringify` work with any array type (`int[]`, `long[]`, `double[]`, `bool[]`, `string[]`). `stringify` also accepts struct types, serializing all fields to a JSON object. `objectify` converts a JSON string back into a typed struct — the target type must be specified via type annotation (e.g., `let p: Person = json.objectify(str)`). `setObj` inserts raw JSON without quoting, which is essential for nesting objects. The `get*` functions return the default zero value (empty string, 0, false, 0.0) if the key is not found.

**JSON Arrays** — for working with JSON arrays as strings (useful for protocols like OCPP that use JSON arrays as message format):

```dex
// Build a JSON array
let arr: string = json.arrayNew()
arr = json.arrayPush(arr, 2)
arr = json.arrayPush(arr, "abc123")
arr = json.arrayPush(arr, "BootNotification")
let payload: string = json.new()
payload = json.set(payload, "vendor", "Dex")
arr = json.arrayPushObj(arr, payload)
// arr is now: [2, "abc123", "BootNotification", {"vendor": "Dex"}]

// Parse a JSON array
let msgType: string = json.arrayGet(arr, 0)    // "2"
let msgId: string = json.arrayGet(arr, 1)      // "abc123"
let action: string = json.arrayGet(arr, 2)     // "BootNotification"
let body: string = json.arrayGet(arr, 3)       // {"vendor": "Dex"}
let len: int = json.arrayLen(arr)              // 4
```

**JSON Objects:**

```dex
let nums: int[] = [1, 2, 3]
let obj: string = json.new()
obj = json.setArray(obj, "numbers", nums)
// obj is now: {"numbers": [1, 2, 3]}

let s: string = json.stringify(nums)
// s is now: [1, 2, 3]
```

**Struct serialization** — `json.stringify` converts a struct to a JSON object string, and `json.objectify` parses a JSON string back into a typed struct:

```dex
struct Person {
    name: string
    age: int
    active: bool
}

let p: Person = Person { name: "Alice", age: 30, active: true }
let jsonStr: string = json.stringify(p)
// jsonStr is now: {"name": "Alice", "age": 30, "active": true}

let p2: Person = json.objectify(jsonStr)
// p2.name == "Alice", p2.age == 30, p2.active == true
```

**Nested JSON objects** — use `json.setObj` to embed one JSON object inside another without quoting:

```dex
let inner: string = json.new()
inner = json.set(inner, "status", "Accepted")
inner = json.set(inner, "expiryDate", "2025-12-31T23:59:59Z")

let resp: string = json.new()
resp = json.set(resp, "action", "Authorize")
resp = json.setObj(resp, "idTagInfo", inner)
// resp is now: {"action":"Authorize","idTagInfo":{"status":"Accepted","expiryDate":"2025-12-31T23:59:59Z"}}
```

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

| Function | Signature                                     | Description                              |
|----------|-----------------------------------------------|------------------------------------------|
| `open`   | `open(driver: string, dsn: string): int`      | Open a connection, returns handle        |
| `exec`   | `exec(conn: int, sql: string): int`           | Execute a statement, returns affected rows |
| `query`  | `query(conn: int, sql: string): int`          | Execute a query, returns result handle   |
| `next`   | `next(rows: int): bool`                       | Advance to next row                      |
| `col`    | `col(rows: int, index: int): int\|string\|double\|bool` | Read a column value (type from context) |
| `free`   | `free(rows: int): void`                       | Free a result set                        |
| `close`  | `close(conn: int): void`                      | Close a connection                       |

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
| `handleMessage`    | `handleMessage(fn): void`         | Register message handler                  |
| `handleConnect`    | `handleConnect(fn): void`         | Register connect handler (called with path) |
| `handleDisconnect` | `handleDisconnect(fn): void`      | Register disconnect handler               |
| `setProtocol`      | `setProtocol(protocol: string): void` | Set WebSocket subprotocol for handshakes |
| `listen`           | `listen(port: int): void`         | Start WebSocket server on port            |

Message handlers must take `(conn: ws.Conn, msg: string)` and return `void`. Connect handlers take `(conn: ws.Conn, path: string)`. Disconnect handlers take `(conn: ws.Conn)`.

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
  ws.setProtocol("ocpp1.6")
  ws.handleConnect(onConnect)
  ws.handleMessage(onMessage)
  ws.handleDisconnect(onDisconnect)
  ws.listen(9090)
}
```

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

### crypto

Cryptographic utility functions.

```dex
import "crypto"

let id: string = crypto.uuid()
// e.g. "a1b2c3d4-e5f6-4789-abcd-ef0123456789"
```

| Function | Signature         | Description                          |
|----------|-------------------|--------------------------------------|
| `uuid`   | `uuid(): string`  | Generate a random UUID v4 string     |

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
}
```

| Function | Signature                       | Description                                        |
|----------|---------------------------------|----------------------------------------------------|
| `env`    | `env(name: string): string`     | Read an environment variable (empty string if unset) |
| `exit`   | `exit(code: int): void`         | Exit the process with the given status code        |
| `exec`   | `exec(command: string): ExecResult` | Run a shell command and return the result      |

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

### send, receive, channel, close

See [Concurrency](#concurrency) section.

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

  let obj: string = json.new()
  obj = json.set(obj, "message", "Hello from Dex!")
  obj = json.set(obj, "count", scores.len())
  obj = json.setArray(obj, "scores", scores)
  obj = json.set(obj, "success", true)
  return obj
}

fn main(): void {
  let result: int = factorial(5)
  fmt.println(result)

  http.route("GET", "/hello", "handle_hello")
  http.listen(8080)
}
```
