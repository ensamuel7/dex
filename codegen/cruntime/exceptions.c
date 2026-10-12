// DexLang exception runtime — setjmp/longjmp-based exception handling

#include <setjmp.h>
#include <stdio.h>
#include <stdlib.h>

#define DEX_MAX_EXCEPTION_DEPTH 64
#define DEX_EXCEPTION_MESSAGE_MAX 512

typedef struct {
    jmp_buf env;
    const char* message;
    int active;
} DexExceptionFrame;

static __thread DexExceptionFrame _dex_exc_stack[DEX_MAX_EXCEPTION_DEPTH];
static __thread int _dex_exc_top = -1;

// A thrown message built at runtime — `throw Exception("no " + name)` — is a
// DexString nothing can own across the throw: longjmp skips every release the
// unwound frames would have run, and the catch site only reads the text.
//
// So the text is copied here first, the thrower releases its string, and the
// longjmp happens afterwards. Staging is a separate call from the throw for
// exactly that reason: there is no way to release anything after a longjmp.
//
// Bounded, and truncation is preferable to the alternatives: an allocation here
// would need an owner across the unwind, which is the problem being solved.
static __thread char _dex_exc_msg[DEX_EXCEPTION_MESSAGE_MAX];

static void _dex_exc_stage(const char* msg) {
    int n = 0;
    if (msg) {
        while (msg[n] && n + 1 < DEX_EXCEPTION_MESSAGE_MAX) {
            _dex_exc_msg[n] = msg[n];
            n++;
        }
    }
    _dex_exc_msg[n] = '\0';
}

static void _dex_throw_staged(void) {
    if (_dex_exc_top < 0) {
        fprintf(stderr, "Unhandled exception: %s\n", _dex_exc_msg);
        exit(1);
    }
    _dex_exc_stack[_dex_exc_top].message = _dex_exc_msg;
    _dex_exc_stack[_dex_exc_top].active = 1;
    longjmp(_dex_exc_stack[_dex_exc_top].env, 1);
}

// A literal message needs no staging: it is static storage and outlives the
// unwind on its own.
static void _dex_throw(const char* msg) {
    if (_dex_exc_top < 0) {
        fprintf(stderr, "Unhandled exception: %s\n", msg);
        exit(1);
    }
    _dex_exc_stack[_dex_exc_top].message = msg;
    _dex_exc_stack[_dex_exc_top].active = 1;
    longjmp(_dex_exc_stack[_dex_exc_top].env, 1);
}
