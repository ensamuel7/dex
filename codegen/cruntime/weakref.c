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
