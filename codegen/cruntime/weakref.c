// DexLang Weak Reference Runtime
// A weak reference does not increment the reference count, so it cannot
// prevent deallocation. Used to break reference cycles (e.g., parent/child
// back-pointers).

#include <stdatomic.h>
#include <stdlib.h>

typedef struct {
    DexObjHeader header;
    void* target;          // the weakly-referenced object (may become NULL)
    _Atomic int* target_rc; // pointer to the target's rc for liveness check
} DexWeakRef;

static void dex_weak_destroy(void* ptr) {
    // DexWeakRef itself has no owned resources beyond the struct
    (void)ptr;
}

static inline DexWeakRef* dex_weak_new(void* target) {
    DexWeakRef* w = (DexWeakRef*)dex_obj_alloc(sizeof(DexWeakRef), dex_weak_destroy);
    w->target = target;
    if (target) {
        w->target_rc = &((DexObjHeader*)target)->rc;
    } else {
        w->target_rc = NULL;
    }
    return w;
}

// dex_weak_deref returns the target if still alive (rc > 0), or NULL.
// Does NOT retain — caller gets a borrowed reference.
//
// UNFINISHED, and not reachable: codegen never emits a call to this, and
// `weak<T>` has no dereference syntax. Do not wire one up without replacing
// the check below, which reads the count out of an object that may already be
// freed. That was merely undefined while a freed block sat in malloc's own
// free list with a zeroed count; with the object pool in refcount.c the block
// is handed straight back out and its count reset to 1, so a dead target now
// reads as alive and this returns a pointer to whatever took its place. The
// fix is state that cannot be recycled into agreement — a generation counter
// in DexObjHeader, bumped on reuse and compared here (sclass already shows
// there is room beside it), or a registry of weak references that destroy
// walks and nulls.
static inline void* dex_weak_deref(DexWeakRef* w) {
    if (!w || !w->target || !w->target_rc) return NULL;
    int rc = atomic_load_explicit(w->target_rc, memory_order_acquire);
    if (rc <= 0) {
        w->target = NULL;
        w->target_rc = NULL;
        return NULL;
    }
    return w->target;
}
