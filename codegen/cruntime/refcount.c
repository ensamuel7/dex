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

#ifdef DEX_PROFILE
// Profiler bookkeeping: the allocating statement's slot in the profiler's site
// table (0 once freed, or for an object it never saw), the object's own size,
// and the size of the backing buffer it owns, if it is a container.
#define DEX_PF_HEADER_FIELDS           \
    unsigned pf_site;                  \
    size_t pf_bytes;                   \
    size_t pf_buf;
#else
#define DEX_PF_HEADER_FIELDS
#endif

// The reference count is the only field whose representation depends on
// whether the program spawns threads: a single-threaded program pays nothing
// for atomics it cannot need. Everything below is written against these three
// macros so there is one copy of the retain/release logic rather than two that
// can drift apart.
#ifdef DEX_SINGLE_THREADED

typedef struct DexObjHeader {
    int rc;
    unsigned sclass;
    void (*destroy)(void*);
    DEX_MC_HEADER_FIELDS
    DEX_PF_HEADER_FIELDS
} DexObjHeader;

#define DEX_RC_SET(o, v) ((o)->rc = (v))
#define DEX_RC_GET(o) ((o)->rc)
#define DEX_RC_INC(o) ((void)((o)->rc++))
#define DEX_RC_DEC(o) ((o)->rc--) /* yields the count before the decrement */

#else

#include <stdatomic.h>

typedef struct DexObjHeader {
    _Atomic int rc;
    unsigned sclass;
    void (*destroy)(void*);
    DEX_MC_HEADER_FIELDS
    DEX_PF_HEADER_FIELDS
} DexObjHeader;

#define DEX_RC_SET(o, v) atomic_store_explicit(&(o)->rc, (v), memory_order_relaxed)
#define DEX_RC_GET(o) atomic_load_explicit(&(o)->rc, memory_order_relaxed)
#define DEX_RC_INC(o) ((void)atomic_fetch_add_explicit(&(o)->rc, 1, memory_order_relaxed))
#define DEX_RC_DEC(o) atomic_fetch_sub_explicit(&(o)->rc, 1, memory_order_acq_rel)

#endif

// Small objects are recycled through a per-thread free list instead of being
// handed back to malloc. A DexString is allocated and freed once per
// concatenation, and on an allocation-heavy loop malloc and free were the
// dominant cost by an order of magnitude over the reference counting around
// them — this took such a loop about 2.25x faster on top of the literal pool.
//
// Nothing about ownership changes. An object is still individually owned,
// individually released, and destroyed exactly when its count reaches zero;
// only the source of its memory differs. That is what separates this from a
// region or an arena, which change *when* memory dies and therefore need
// escape analysis to be safe.
//
// It is off under both memory debuggers, and that is not a detail: recycling
// hands a freed block straight back out, which is precisely the state the
// memory checker's quarantine and ASan's redzones exist to detect. Leaving it
// on would make a use-after-free look like a successful allocation.
#if defined(DEX_MEM_CHECK)
#define DEX_USE_POOL 0
#elif defined(__SANITIZE_ADDRESS__)
#define DEX_USE_POOL 0
#elif defined(__has_feature)
#if __has_feature(address_sanitizer)
#define DEX_USE_POOL 0
#endif
#endif
#ifndef DEX_USE_POOL
#define DEX_USE_POOL 1
#endif

#if DEX_USE_POOL

// Six classes — 16 through 512 bytes — 64 deep. Anything larger goes straight
// to malloc, where the allocator's own cost is small beside the work of
// filling the object. Widening either bound measured no faster.
#define DEX_POOL_CLASSES 6
#define DEX_POOL_DEPTH 64
#define DEX_POOL_LANES 64

// The pool lives in plain globals rather than in thread-local storage, and
// that is a correctness requirement rather than a style choice. A pooled block
// is freed as far as the program is concerned but still malloc'd as far as the
// system is concerned, so it has to stay *reachable* or a leak checker will
// report it. `leaks` roots its scan at thread stacks, registers and the global
// data segment — not at thread-local storage — so a thread-local pool made
// every block sitting in it at exit a reported leak. One lane per thread in a
// global array keeps them reachable and needs no drain at thread exit at all.
//
// n is sized to fill a cache line so that two threads writing their own counts
// never share one.
typedef struct {
    void* slots[DEX_POOL_CLASSES][DEX_POOL_DEPTH];
    int n[16];
} DexPoolLane;

static DexPoolLane _dex_lanes[DEX_POOL_LANES];

// DEX_POOL_CLASSES for anything too large to pool, which _dex_obj_free reads
// back as "return this one to malloc".
static inline unsigned _dex_pool_class(size_t size) {
    unsigned c = 0;
    size_t cap = 16;
    while (cap < size) {
        if (++c >= DEX_POOL_CLASSES) return DEX_POOL_CLASSES;
        cap <<= 1;
    }
    return c;
}

#ifdef DEX_SINGLE_THREADED

#define _dex_lane() (&_dex_lanes[0])

#else

// A lane is claimed on first use and never handed back: a thread that outlives
// its lane's usefulness retains at most 512 bytes times the depth, and a
// program churning through more than DEX_POOL_LANES threads simply stops
// pooling rather than sharing a lane unsynchronised. _dex_lane returns NULL in
// that case and both paths fall back to malloc.
static _Atomic int _dex_lane_next;
static _Thread_local DexPoolLane* _dex_lane_self;
static _Thread_local int _dex_lane_tried;

static DexPoolLane* _dex_lane_claim(void) {
    _dex_lane_tried = 1;
    int i = atomic_fetch_add_explicit(&_dex_lane_next, 1, memory_order_relaxed);
    if (i >= DEX_POOL_LANES) return NULL;
    _dex_lane_self = &_dex_lanes[i];
    return _dex_lane_self;
}

static inline DexPoolLane* _dex_lane(void) {
    if (_dex_lane_self) return _dex_lane_self;
    if (_dex_lane_tried) return NULL;
    return _dex_lane_claim();
}

#endif

// Only a block that came from dex_obj_alloc may arrive here: sclass is read
// back out of its header, so anything else contributes a garbage class and is
// filed under a size it does not have, which later hands it out for an object
// that does not fit. The two callers are dex_release and dex_owned_free, and
// both are reached only through a managed object's count. A managed object that
// was *not* malloc'd — a pooled string literal, an arena string — is immortal,
// and the immortal guard returns before either of them gets this far.
static inline void _dex_obj_free(void* ptr) {
    unsigned c = ((DexObjHeader*)ptr)->sclass;
    if (c < DEX_POOL_CLASSES) {
        DexPoolLane* lane = _dex_lane();
        if (lane && lane->n[c] < DEX_POOL_DEPTH) {
            lane->slots[c][lane->n[c]++] = ptr;
            return;
        }
    }
    free(ptr);
}

#else
#define _dex_obj_free(ptr) free(ptr)
#endif

// An immortal object is one that was never malloc'd: a statically initialised
// string literal, emitted once into the binary rather than allocated at every
// evaluation. Its count is a sentinel no live object can reach, and retain and
// release are no-ops on it.
//
// Three things make that sound, and each is why the check sits where it does:
//
//   - A relaxed read suffices. The sentinel is written at compile time by the
//     static initialiser and never again, so there is nothing to synchronise
//     with; a mortal object's count is read here too, but only ever compared
//     against a value it cannot hold.
//   - The count must not be decremented, not merely spared the free. Release
//     outnumbers retain on a literal by one per evaluation, so a counter that
//     drifted would reach zero after ~2^31 of them and free a static.
//   - The test precedes dex_mc_check. An immortal object is absent from the
//     memory checker's registry by construction, and the checker reports a
//     release of something it does not know about as an error.
#define DEX_RC_IMMORTAL __INT_MAX__
#define DEX_IS_IMMORTAL(o) (DEX_RC_GET(o) == DEX_RC_IMMORTAL)

#ifdef DEX_MEM_CHECK
// Defined by the memory checker runtime, which is emitted immediately after
// this file so that it can see DexObjHeader.
static void dex_mc_on_alloc(void* ptr, size_t size);
static void dex_mc_on_free(void* ptr);
static void dex_mc_check(const void* ptr, const char* op);
static void dex_mc_rc_guard(const void* ptr);
#else
#define dex_mc_on_alloc(ptr, size) ((void)0)
#define dex_mc_on_free(ptr) _dex_obj_free(ptr)
#define dex_mc_check(ptr, op) ((void)0)
#define dex_mc_rc_guard(ptr) ((void)0)
#ifndef DEX_PROFILE
#define DEX_AT(site) ((void)0)
#endif
#endif

#ifdef DEX_PROFILE
// Defined by the profiler runtime, emitted after this file for the same reason
// as the memory checker's hooks.
static void dex_pf_on_alloc(void* ptr, size_t size);
static void dex_pf_on_free(void* ptr);
static void dex_pf_buf_set(void* ptr, size_t bytes);
#define DEX_PF_BUF_SET(obj, bytes) dex_pf_buf_set((obj), (bytes))
// Writes the profile now. For a runtime that ends the process with _exit,
// which skips the atexit handler the profile is otherwise written from.
static void dex_pf_shutdown(void);
#define DEX_PF_FLUSH() dex_pf_shutdown()
#else
#define dex_pf_on_alloc(ptr, size) ((void)0)
#define dex_pf_on_free(ptr) ((void)0)
#define DEX_PF_BUF_SET(obj, bytes) ((void)0)
#define DEX_PF_FRAME() ((void)0)
#define DEX_PF_FLUSH() ((void)0)
#endif

static inline void* dex_obj_alloc(size_t size, void (*destroy)(void*)) {
    DexObjHeader* obj = NULL;
#if DEX_USE_POOL
    unsigned c = _dex_pool_class(size);
    if (c < DEX_POOL_CLASSES) {
        DexPoolLane* lane = _dex_lane();
        if (lane && lane->n[c] > 0) obj = (DexObjHeader*)lane->slots[c][--lane->n[c]];
    }
    if (!obj) {
        // Allocated at the class's full size rather than the requested size, so
        // a block recycled for a later request in the same class always fits.
        obj = (DexObjHeader*)malloc(c < DEX_POOL_CLASSES ? ((size_t)16 << c) : size);
        if (!obj) { fprintf(stderr, "runtime error: out of memory\n"); exit(1); }
    }
    obj->sclass = c;
#else
    obj = (DexObjHeader*)malloc(size);
    if (!obj) { fprintf(stderr, "runtime error: out of memory\n"); exit(1); }
    obj->sclass = 0;
#endif
    DEX_RC_SET(obj, 1);
    obj->destroy = destroy;
    dex_mc_on_alloc(obj, size);
    dex_pf_on_alloc(obj, size);
    return obj;
}

static inline void dex_retain(void* ptr) {
    if (!ptr) return;
    DexObjHeader* obj = (DexObjHeader*)ptr;
    if (DEX_IS_IMMORTAL(obj)) return;
    dex_mc_check(obj, "retain");
    DEX_RC_INC(obj);
}

static inline void dex_release(void* ptr) {
    if (!ptr) return;
    DexObjHeader* obj = (DexObjHeader*)ptr;
    if (DEX_IS_IMMORTAL(obj)) return;
    dex_mc_check(obj, "release");
    dex_mc_rc_guard(obj);
    if (DEX_RC_DEC(obj) == 1) {
        if (obj->destroy) obj->destroy(ptr);
        dex_pf_on_free(ptr);
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

static inline void* dex_optbox_new(size_t size, void (*destroy)(void*)) {
    return (char*)dex_obj_alloc(sizeof(DexObjHeader) + size, destroy) + sizeof(DexObjHeader);
}

static inline void dex_optbox_retain(void* value) {
    if (value) dex_retain((char*)value - sizeof(DexObjHeader));
}

static inline void dex_optbox_release(void* value) {
    if (value) dex_release((char*)value - sizeof(DexObjHeader));
}

static inline void dex_owned_free(void* ptr) {
    if (!ptr) return;
    DexObjHeader* obj = (DexObjHeader*)ptr;
    if (DEX_IS_IMMORTAL(obj)) return;
    dex_mc_check(obj, "owned free");
    if (obj->destroy) obj->destroy(ptr);
    dex_pf_on_free(ptr);
    dex_mc_on_free(ptr);
}
