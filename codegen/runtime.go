package codegen

import _ "embed"

//go:embed cruntime/refcount.c
var RefcountRuntime string

//go:embed cruntime/safety.c
var SafetyRuntime string

//go:embed cruntime/strings.c
var StringRuntime string

//go:embed cruntime/arrays.c
var ArrayRuntime string

//go:embed cruntime/concurrency.c
var ConcurrencyRuntime string

//go:embed cruntime/weakref.c
var WeakRefRuntime string

//go:embed cruntime/arena.c
var ArenaRuntime string

//go:embed cruntime/cycles.c
var CycleDebugRuntime string

//go:embed cruntime/optionals.c
var OptionalRuntime string

//go:embed cruntime/exceptions.c
var ExceptionRuntime string

//go:embed cruntime/string_methods.c
var StringMethodsRuntime string

//go:embed cruntime/maps.c
var MapRuntime string

//go:embed cruntime/threadpool.c
var ThreadPoolRuntime string

//go:embed cruntime/event_loop.c
var EventLoopRuntime string

//go:embed cruntime/stringbuilder.c
var StringBuilderRuntime string

//go:embed cruntime/closure.c
var ClosureRuntime string
