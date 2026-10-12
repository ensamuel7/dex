// DexLang Memory Checker — compiled in only for DEX_MEM_CHECK=1 builds.
//
// The expensive part of a refcounting bug is not the crash, it is the distance
// between the mistake and the crash. A reference released once too often
// corrupts the allocator and the program dies in the *next* allocation,
// pointing at a line that has nothing to do with it. A reference never
// released shows up as a number in a leak report with no idea which statement
// produced it.
//
// This closes both gaps. Every managed object carries a magic word, the .dx
// statement that allocated it, and the .dx statement that last released it,
// and is held in a quarantine ring after its final release instead of being
// handed straight back to malloc. So:
//
//   - releasing or retaining a freed object panics at that release, naming
//     both the allocating and the previously-releasing statement;
//   - a refcount that would go negative panics instead of wrapping;
//   - passing something that is not a managed object panics instead of
//     scribbling on it;
//   - reading through a freed pointer sees 0xDD rather than plausible data;
//   - at exit, every object still alive is reported grouped by the .dx
//     statement that allocated it.
//
// The quarantine is bounded, so a double release separated by more than
// DEX_MC_QUARANTINE intervening frees can still slip through to real freed
// memory — run DEX_SANITIZE=1 for that case. The two modes are complementary:
// this one knows about .dx statements and refcounts, ASan knows about the
// whole heap.

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

#define DEX_MC_MAGIC_LIVE 0xD3C0FFEE0000A11CULL
#define DEX_MC_MAGIC_DEAD 0xD3C0FFEE0000DEADULL

// Freed objects retained so that a later release through the same pointer sees
// DEAD rather than whatever malloc has since put there.
#define DEX_MC_QUARANTINE 2048
// Distinct allocation sites tracked in the leak report.
#define DEX_MC_SITES 128

#ifdef DEX_SINGLE_THREADED
#define DEX_MC_LOCK() ((void)0)
#define DEX_MC_UNLOCK() ((void)0)
#define DEX_MC_TLS
#define DEX_MC_RC(o) ((int)(o)->rc)
#else
#include <pthread.h>
static pthread_mutex_t dex_mc_mu = PTHREAD_MUTEX_INITIALIZER;
#define DEX_MC_LOCK() pthread_mutex_lock(&dex_mc_mu)
#define DEX_MC_UNLOCK() pthread_mutex_unlock(&dex_mc_mu)
#define DEX_MC_TLS _Thread_local
#define DEX_MC_RC(o) atomic_load_explicit(&(o)->rc, memory_order_relaxed)
#endif

// The .dx statement currently executing. Set by DEX_AT, which codegen emits
// ahead of every statement in a mem-check build.
static DEX_MC_TLS const char* dex_mc_site = "<program start>";

static DexObjHeader* dex_mc_live = NULL;
static DexObjHeader* dex_mc_quarantine[DEX_MC_QUARANTINE];
static int dex_mc_q_next = 0;
static long dex_mc_live_count = 0;
static long dex_mc_allocs = 0;
static long dex_mc_frees = 0;

static void dex_mc_describe(const DexObjHeader* o) {
    if (!o) return;
    fprintf(stderr, "  object       %p, %zu bytes, refcount %d\n",
            (const void*)o, o->mc_size, DEX_MC_RC(o));
    fprintf(stderr, "  allocated at %s\n",
            o->mc_alloc_site ? o->mc_alloc_site : "<unknown>");
    if (o->mc_free_site) {
        fprintf(stderr, "  freed at     %s\n", o->mc_free_site);
    }
}

static void dex_mc_fail(const char* what, const DexObjHeader* o) {
    fflush(stdout);
    fprintf(stderr, "\nmemory error: %s\n", what);
    fprintf(stderr, "  while at     %s\n", dex_mc_site);
    dex_mc_describe(o);
    fprintf(stderr, "  %ld allocated, %ld freed, %ld live\n",
            dex_mc_allocs, dex_mc_frees, dex_mc_live_count);
    fflush(stderr);
    // abort() is what a debugger wants, but on macOS it hands the process to
    // the crash reporter, which can take tens of seconds — long enough that a
    // test run looks hung rather than failed. So the default is a plain fast
    // exit, and DEX_MEM_ABORT=1 asks for the core dump.
    const char* mode = getenv("DEX_MEM_ABORT");
    if (mode && strcmp(mode, "1") == 0) abort();
    _exit(70);
}

// Validates that ptr is a live managed object. Reading the magic word of a
// pointer that was never one is itself undefined, but in practice it either
// reads a value that is not one of the two magics — which is the case this
// catches — or faults, which is still a far better failure than silently
// decrementing whatever sits at offset zero.
static void dex_mc_check(const void* ptr, const char* op) {
    const DexObjHeader* o = (const DexObjHeader*)ptr;
    if (o->mc_magic == DEX_MC_MAGIC_LIVE) return;
    char what[128];
    if (o->mc_magic == DEX_MC_MAGIC_DEAD) {
        snprintf(what, sizeof(what), "%s of an object that was already freed", op);
    } else {
        snprintf(what, sizeof(what), "%s of a pointer that is not a managed object", op);
        o = NULL;
    }
    dex_mc_fail(what, o);
}

static void dex_mc_rc_guard(const void* ptr) {
    const DexObjHeader* o = (const DexObjHeader*)ptr;
    if (DEX_MC_RC(o) <= 0) {
        dex_mc_fail("reference count underflow: released an object that had no references left", o);
    }
}

static void dex_mc_on_alloc(void* ptr, size_t size) {
    DexObjHeader* o = (DexObjHeader*)ptr;
    o->mc_magic = DEX_MC_MAGIC_LIVE;
    o->mc_alloc_site = dex_mc_site;
    o->mc_free_site = NULL;
    o->mc_size = size;
    DEX_MC_LOCK();
    o->mc_prev = NULL;
    o->mc_next = dex_mc_live;
    if (dex_mc_live) dex_mc_live->mc_prev = o;
    dex_mc_live = o;
    dex_mc_live_count++;
    dex_mc_allocs++;
    DEX_MC_UNLOCK();
}

// Replaces the free() in dex_release and dex_owned_free: unlinks the object,
// marks it dead, poisons its payload and parks it in the quarantine ring. The
// object evicted from the ring is the one actually handed back to malloc.
static void dex_mc_on_free(void* ptr) {
    DexObjHeader* o = (DexObjHeader*)ptr;
    DEX_MC_LOCK();
    if (o->mc_prev) o->mc_prev->mc_next = o->mc_next;
    else dex_mc_live = o->mc_next;
    if (o->mc_next) o->mc_next->mc_prev = o->mc_prev;
    o->mc_prev = NULL;
    o->mc_next = NULL;
    o->mc_magic = DEX_MC_MAGIC_DEAD;
    o->mc_free_site = dex_mc_site;
    dex_mc_live_count--;
    dex_mc_frees++;
    // Everything past the header is the object's own payload — a string's
    // bytes, an array's length and data pointer. Poisoning it turns a read
    // through a stale pointer into visible garbage instead of stale-but-valid
    // data, and the header is left intact so the magic and both sites survive.
    if (o->mc_size > sizeof(DexObjHeader)) {
        memset((char*)o + sizeof(DexObjHeader), 0xDD, o->mc_size - sizeof(DexObjHeader));
    }
    DexObjHeader* evicted = dex_mc_quarantine[dex_mc_q_next];
    dex_mc_quarantine[dex_mc_q_next] = o;
    dex_mc_q_next = (dex_mc_q_next + 1) % DEX_MC_QUARANTINE;
    DEX_MC_UNLOCK();
    if (evicted) free(evicted);
}

typedef struct {
    const char* site;
    long count;
    size_t bytes;
} DexMcSiteTally;

// Reports every object still alive at exit, grouped by the .dx statement that
// allocated it.
//
// "Still alive" is not the same as "leaked", which is why this reports rather
// than fails: a handler registered with http.route or ws.handleMessage is
// owned by a table that lives as long as the process and is never torn down,
// so it is correctly alive here. DEX_MEM_LEAKS=error turns the report into a
// failing exit status once the expected-live set for a program is known. A
// memory *error* needs no such judgement and is always fatal.
static void dex_mc_report(void) {
    DEX_MC_LOCK();
    long leaked = dex_mc_live_count;
    DexMcSiteTally tally[DEX_MC_SITES];
    int sites = 0;
    long untallied = 0;
    size_t total = 0;
    for (DexObjHeader* o = dex_mc_live; o; o = o->mc_next) {
        const char* site = o->mc_alloc_site ? o->mc_alloc_site : "<unknown>";
        total += o->mc_size;
        int i = 0;
        for (; i < sites; i++) {
            if (strcmp(tally[i].site, site) == 0) break;
        }
        if (i == sites) {
            if (sites == DEX_MC_SITES) { untallied++; continue; }
            tally[sites].site = site;
            tally[sites].count = 0;
            tally[sites].bytes = 0;
            sites++;
        }
        tally[i].count++;
        tally[i].bytes += o->mc_size;
    }
    DEX_MC_UNLOCK();

    const char* verbose = getenv("DEX_MEM_VERBOSE");
    if (leaked == 0) {
        if (verbose) {
            fflush(stdout);
            fprintf(stderr, "memory check: nothing alive at exit (%ld allocated, %ld freed)\n",
                    dex_mc_allocs, dex_mc_frees);
        }
        return;
    }

    fflush(stdout);
    fprintf(stderr, "\nmemory check: %ld object%s still alive at exit (%zu bytes, %ld allocated, %ld freed)\n",
            leaked, leaked == 1 ? "" : "s", total, dex_mc_allocs, dex_mc_frees);
    for (int i = 0; i < sites; i++) {
        fprintf(stderr, "  %6ld object%s (%7zu bytes) allocated at %s\n",
                tally[i].count, tally[i].count == 1 ? " " : "s",
                tally[i].bytes, tally[i].site);
    }
    if (untallied) {
        fprintf(stderr, "  %6ld more from sites beyond the first %d\n", untallied, DEX_MC_SITES);
    }
    fflush(stderr);

    const char* mode = getenv("DEX_MEM_LEAKS");
    if (!mode || strcmp(mode, "error") != 0) return;
    // exit() is not callable from an atexit handler, and the status has to
    // change for a test run to notice.
    _exit(70);
}

static void dex_mc_init(void) {
    atexit(dex_mc_report);
}

#define DEX_AT(site) (dex_mc_site = (site))
