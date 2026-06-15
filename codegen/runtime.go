package codegen

import _ "embed"

//go:embed cruntime/safety.c
var SafetyRuntime string

//go:embed cruntime/strings.c
var StringRuntime string

//go:embed cruntime/arrays.c
var ArrayRuntime string

//go:embed cruntime/concurrency.c
var ConcurrencyRuntime string
