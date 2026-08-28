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
