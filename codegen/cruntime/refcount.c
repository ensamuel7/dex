// DexLang Reference Counting Runtime
// Core infrastructure for heap-managed objects.

#include <stdatomic.h>
#include <stdlib.h>
#include <string.h>

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

static inline void dex_owned_free(void* ptr) {
    if (!ptr) return;
    DexObjHeader* obj = (DexObjHeader*)ptr;
    if (obj->destroy) obj->destroy(ptr);
    free(ptr);
}
