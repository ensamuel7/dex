// DexLang Reference Counting Runtime
// Core infrastructure for heap-managed objects.

#include <stdlib.h>
#include <string.h>

#ifdef DEX_MEM_CHECK
// Debug bookkeeping appended to every managed object's header. Every managed
// type embeds DexObjHeader by value, so widening it here shifts their own
// fields consistently and needs no change anywhere else.
#define DEX_MC_HEADER_FIELDS           \
    unsigned long long mc_magic;       \
    const char* mc_alloc_site;         \
    const char* mc_free_site;          \
    size_t mc_size;                    \
    struct DexObjHeader* mc_prev;      \
    struct DexObjHeader* mc_next;
#else
#define DEX_MC_HEADER_FIELDS
#endif

// The reference count is the only field whose representation depends on
// whether the program spawns threads: a single-threaded program pays nothing
// for atomics it cannot need. Everything below is written against these three
// macros so there is one copy of the retain/release logic rather than two that
// can drift apart.
#ifdef DEX_SINGLE_THREADED

typedef struct DexObjHeader {
    int rc;
    void (*destroy)(void*);
    DEX_MC_HEADER_FIELDS
} DexObjHeader;

#define DEX_RC_SET(o, v) ((o)->rc = (v))
#define DEX_RC_INC(o) ((void)((o)->rc++))
#define DEX_RC_DEC(o) ((o)->rc--) /* yields the count before the decrement */

#else

#include <stdatomic.h>

typedef struct DexObjHeader {
    _Atomic int rc;
    void (*destroy)(void*);
    DEX_MC_HEADER_FIELDS
} DexObjHeader;

#define DEX_RC_SET(o, v) atomic_store_explicit(&(o)->rc, (v), memory_order_relaxed)
#define DEX_RC_INC(o) ((void)atomic_fetch_add_explicit(&(o)->rc, 1, memory_order_relaxed))
#define DEX_RC_DEC(o) atomic_fetch_sub_explicit(&(o)->rc, 1, memory_order_acq_rel)

#endif

#ifdef DEX_MEM_CHECK
// Defined by the memory checker runtime, which is emitted immediately after
// this file so that it can see DexObjHeader.
static void dex_mc_on_alloc(void* ptr, size_t size);
static void dex_mc_on_free(void* ptr);
static void dex_mc_check(const void* ptr, const char* op);
static void dex_mc_rc_guard(const void* ptr);
#else
#define dex_mc_on_alloc(ptr, size) ((void)0)
#define dex_mc_on_free(ptr) free(ptr)
#define dex_mc_check(ptr, op) ((void)0)
#define dex_mc_rc_guard(ptr) ((void)0)
#define DEX_AT(site) ((void)0)
#endif

static inline void* dex_obj_alloc(size_t size, void (*destroy)(void*)) {
    DexObjHeader* obj = (DexObjHeader*)malloc(size);
    if (!obj) { fprintf(stderr, "runtime error: out of memory\n"); exit(1); }
    DEX_RC_SET(obj, 1);
    obj->destroy = destroy;
    dex_mc_on_alloc(obj, size);
    return obj;
}

static inline void dex_retain(void* ptr) {
    if (!ptr) return;
    DexObjHeader* obj = (DexObjHeader*)ptr;
    dex_mc_check(obj, "retain");
    DEX_RC_INC(obj);
}

static inline void dex_release(void* ptr) {
    if (!ptr) return;
    DexObjHeader* obj = (DexObjHeader*)ptr;
    dex_mc_check(obj, "release");
    dex_mc_rc_guard(obj);
    if (DEX_RC_DEC(obj) == 1) {
        if (obj->destroy) obj->destroy(ptr);
        dex_mc_on_free(ptr);
    }
}

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
    dex_mc_check(obj, "owned free");
    if (obj->destroy) obj->destroy(ptr);
    dex_mc_on_free(ptr);
}
