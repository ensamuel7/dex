// DexLang exception runtime — setjmp/longjmp-based exception handling

#include <setjmp.h>
#include <stdio.h>
#include <stdlib.h>

#define DEX_MAX_EXCEPTION_DEPTH 64

typedef struct {
    jmp_buf env;
    const char* message;
    int active;
} DexExceptionFrame;

static __thread DexExceptionFrame _dex_exc_stack[DEX_MAX_EXCEPTION_DEPTH];
static __thread int _dex_exc_top = -1;

static void _dex_throw(const char* msg) {
    if (_dex_exc_top < 0) {
        fprintf(stderr, "Unhandled exception: %s\n", msg);
        exit(1);
    }
    _dex_exc_stack[_dex_exc_top].message = msg;
    _dex_exc_stack[_dex_exc_top].active = 1;
    longjmp(_dex_exc_stack[_dex_exc_top].env, 1);
}
