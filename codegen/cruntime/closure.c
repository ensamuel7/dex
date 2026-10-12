// DexLang Closure Runtime
//
// A function value is code plus the state it carries. The environment is always
// passed as a hidden first argument, so a plain function, a lambda over locals
// and a method bound to its receiver are all invoked the same way.

typedef struct DexClosure {
    DexObjHeader hdr;
    void* fn;   // ret (*)(void* env, params...)
    void* env;  // refcounted, or NULL for a plain function
} DexClosure;

static void dex_closure_destroy(void* ptr) {
    DexClosure* c = (DexClosure*)ptr;
    if (c->env) dex_release(c->env);
}

// Takes ownership of env.
static DexClosure* dex_closure_new(void* fn, void* env) {
    DexClosure* c = (DexClosure*)dex_obj_alloc(sizeof(DexClosure), dex_closure_destroy);
    c->fn = fn;
    c->env = env;
    return c;
}

// Refcounted so a closure can be copied and stored freely. destroy is generated
// per environment type and releases whatever heap values were captured.
static void* dex_closure_env_alloc(size_t size, void (*destroy)(void*)) {
    return dex_obj_alloc(size, destroy);
}

// The defer stack: calls to run when a frame is left, however it is left.
//
// A deferred call is registered when its `defer` statement runs, which is what
// makes it conditional — a defer inside an `if` that was not taken registers
// nothing — and what makes one in a loop run per iteration. It is also what
// lets an unwind run them: the frames between a throw and the catch are
// skipped, so the catch runs everything registered above its own height.
//
// Entries are closures, so whatever the deferred call needs is captured and
// owned by the closure rather than read out of a frame that may be gone.
#define DEX_DEFER_MAX 1024
static __thread DexClosure* _dex_defer_stack[DEX_DEFER_MAX];
static __thread size_t _dex_defer_top = 0;

static void _dex_defer_push(DexClosure* c) {
    if (!c) {
        return;
    }
    if (_dex_defer_top >= DEX_DEFER_MAX) {
        // Degrades to not running the call rather than overrunning the stack.
        dex_release(c);
        return;
    }
    _dex_defer_stack[_dex_defer_top++] = c;
}

// Runs and releases everything registered above a height, most recent first.
// Used at every way out of a frame: a return, falling off the end, and an
// unwind landing at a catch.
static void _dex_defer_run_to(size_t mark) {
    while (_dex_defer_top > mark) {
        DexClosure* c = _dex_defer_stack[--_dex_defer_top];
        if (!c) {
            continue;
        }
        ((void (*)(void*))c->fn)(c->env);
        dex_release(c);
    }
}
