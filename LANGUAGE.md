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
        fmt.print(name)
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
| `private` | Restricted to the defining module           |

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

In single-file mode, privacy is tracked in the AST but not enforced (since all code is in the same file). Enforcement will be added with multi-file compilation support.

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
    fmt.print("green")
}

switch (c) {
    case Color.Red: {
        fmt.print("red")
    }
    case Color.Green, Color.Blue: {
        fmt.print("not red")
    }
}
```

Enums are value types (integers) — no heap allocation or refcounting needed.

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

### Array methods

| Method   | Description                          | Returns |
|----------|--------------------------------------|---------|
| `.push(value)` | Append an element to the array | `void`  |
| `.len()`       | Get the number of elements     | `int`   |

```dex
let items: int[] = []
items.push(10)
items.push(20)
let count: int = items.len()  // 2
```

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
fmt.print(42)
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

`+` also works for string concatenation when both operands are `string`.

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
  fmt.print(i)
}
```

The init statement can be a `let` declaration (with or without type inference) or an assignment. The post statement supports `++`, `--`, `+=`, `-=`, or `=`.

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
  fmt.print(n)
}

// Index and value
foreach(nums as i, n) {
  fmt.print(i)  // 0, 1, 2
  fmt.print(n)  // 10, 20, 30
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
  fmt.print(i)  // prints odd numbers only
}
```

`break` and `continue` work inside `while`, `for`, and `foreach` loops. Using them outside a loop is a compile error.

### switch

Multi-way branching on a value. Supports `int`, `string`, `char`, `long`, `double`, and `bool` tag types. Cases use colon syntax with brace-delimited bodies. Multiple values per case are separated by commas.

```dex
switch (action) {
    case "BootNotification": {
        fmt.print("Boot!")
    }
    case "Heartbeat", "StatusNotification": {
        fmt.print("Alive")
    }
    default: {
        fmt.print("Unknown")
    }
}
```

Integer switch:

```dex
switch (code) {
    case 200: {
        fmt.print("OK")
    }
    case 404: {
        fmt.print("Not Found")
    }
    default: {
        fmt.print("Other")
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
    fmt.print(e.message)
} finally {
    // always runs after try or catch
    fmt.print("cleanup")
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
    fmt.print("cleanup runs first")
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
        fmt.print(e.message)  // "from risky"
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

Print values to stdout (with newline).

```dex
import "fmt"

fmt.print(42)            // print an int
fmt.print("hello")       // print a string
fmt.print(100000)        // print a long
fmt.print(3.14)          // print a double
fmt.print(true)          // print a bool ("true" or "false")
```

| Function | Signature                                              | Description                       |
|----------|--------------------------------------------------------|-----------------------------------|
| `print`  | `print(value: int\|long\|double\|string\|bool): void` | Print any primitive with newline  |

### http

Build HTTP servers and make HTTP requests.

```dex
import "http"

http.route("GET", "/path", "handler_function_name")
http.listen(8080)
```

#### Server

| Function | Signature                                              | Description                        |
|----------|--------------------------------------------------------|------------------------------------|
| `route`  | `route(method: string, path: string, handler: string): void` | Register a route handler    |
| `listen` | `listen(port: int): void`                              | Start the server on a port         |

Handler functions must take no parameters and return `string`. They are referenced by name as a string literal:

```dex
fn my_handler(): string {
  return "{\"ok\": true}"
}

fn main(): void {
  http.route("GET", "/api", "my_handler")
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
  fmt.print(resp.statusCode)
  fmt.print(resp.body)
  fmt.print(resp.contentType)

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
| `stringify`  | `stringify(arr: T[]): string`                                                   | Convert an array to JSON string |
| `get`        | `get(json: string, key: string): string`                                        | Get a string value by key      |
| `getInt`     | `getInt(json: string, key: string): int`                                        | Get an integer value by key    |
| `getBool`    | `getBool(json: string, key: string): bool`                                      | Get a boolean value by key     |
| `getDouble`  | `getDouble(json: string, key: string): double`                                  | Get a double value by key      |
| `getLong`    | `getLong(json: string, key: string): long`                                      | Get a long integer value by key |

`set` accepts any primitive type as the value — the compiler dispatches to the correct implementation based on the argument type. `setArray` and `stringify` work with any array type (`int[]`, `long[]`, `double[]`, `bool[]`, `string[]`). The `get*` functions return the default zero value (empty string, 0, false, 0.0) if the key is not found.

```dex
let nums: int[] = [1, 2, 3]
let obj: string = json.new()
obj = json.setArray(obj, "numbers", nums)
// obj is now: {"numbers": [1, 2, 3]}

let s: string = json.stringify(nums)
// s is now: [1, 2, 3]
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
    fmt.print(name)
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

Time functions for measuring and waiting.

```dex
import "time"

let start: long = time.now()
time.sleep(100)  // sleep 100ms
let elapsed: long = time.now() - start
```

| Function  | Signature               | Description                          |
|-----------|-------------------------|--------------------------------------|
| `now`     | `now(): long`           | Current time in milliseconds         |
| `nowNs`  | `nowNs(): long`        | Current time in nanoseconds          |
| `sleep`   | `sleep(ms: int): void`  | Sleep for specified milliseconds     |

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
fmt.print(result.output)
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
  fmt.print(result)

  http.route("GET", "/hello", "handle_hello")
  http.listen(8080)
}
```
