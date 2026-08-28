// DexLang Reference Counting Runtime
// Core infrastructure for heap-managed objects.

#include <stdlib.h>
#include <string.h>

#ifdef DEX_SINGLE_THREADED

typedef struct {
    int rc;
    void (*destroy)(void*);
} DexObjHeader;

static inline void* dex_obj_alloc(size_t size, void (*destroy)(void*)) {
    DexObjHeader* obj = (DexObjHeader*)malloc(size);
    if (!obj) { fprintf(stderr, "runtime error: out of memory\n"); exit(1); }
    obj->rc = 1;
    obj->destroy = destroy;
    return obj;
}

static inline void dex_retain(void* ptr) {
    if (!ptr) return;
    DexObjHeader* obj = (DexObjHeader*)ptr;
    obj->rc++;
}

static inline void dex_release(void* ptr) {
    if (!ptr) return;
    DexObjHeader* obj = (DexObjHeader*)ptr;
    if (--obj->rc == 0) {
        if (obj->destroy) obj->destroy(ptr);
        free(ptr);
    }
}

#else

#include <stdatomic.h>

typedef struct {
    _Atomic int rc;
    void (*destroy)(void*);
} DexObjHeader;

static inline void* dex_obj_alloc(size_t size, void (*destroy)(void*)) {
    DexObjHeader* obj = (DexObjHeader*)malloc(size);
    if (!obj) { fprintf(stderr, "runtime error: out of memory\n"); exit(1); }
    atomic_store_explicit(&obj->rc, 1, memory_order_relaxed);
    obj->destroy = destroy;
    return obj;
}

static inline void dex_retain(void* ptr) {
    if (!ptr) return;
    DexObjHeader* obj = (DexObjHeader*)ptr;
    atomic_fetch_add_explicit(&obj->rc, 1, memory_order_relaxed);
}

static inline void dex_release(void* ptr) {
    if (!ptr) return;
    DexObjHeader* obj = (DexObjHeader*)ptr;
    if (atomic_fetch_sub_explicit(&obj->rc, 1, memory_order_acq_rel) == 1) {
        if (obj->destroy) obj->destroy(ptr);
        free(ptr);
    }
}

#endif

// dex_retained retains a pointer and hands it back, so a borrowed value can be
// stored into a field inside an initialiser expression where a statement would
// not fit.
static inline void* dex_retained(void* ptr) {
    dex_retain(ptr);
    return ptr;
}

static inline void dex_owned_free(void* ptr) {
    if (!ptr) return;
    DexObjHeader* obj = (DexObjHeader*)ptr;
    if (obj->destroy) obj->destroy(ptr);
    free(ptr);
}
