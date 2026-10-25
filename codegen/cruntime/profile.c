// DexLang Memory Profiler — compiled in only for profile builds (dex profile,
// or DEX_PROFILE=1 on build and run).
//
// The memory checker answers "is this program's memory *correct*". This
// answers "where does its memory *go*": which .dx line allocates the most,
// which lines hold memory at the peak, and whether the total keeps climbing.
//
// It rides on the same DEX_AT marker the checker uses, so an allocation is
// charged to the statement that was running when it happened. What it leaves
// out is the checker's quarantine and poisoning — freed memory goes straight
// back to the object pool as it would in a release build — so the totals are
// the program's own rather than inflated by the tool.
//
// Every managed object's header carries the site index that allocated it and
// how many bytes it accounts for. Bytes are the object's own size plus the
// backing buffer of an array, a map or a string builder, which the containers
// report through DEX_PF_BUF_SET whenever they grow; that is where most of a
// program's memory actually is. Memory a stdlib function mallocs for its own
// use and frees before it returns is not a managed object and is not counted.
//
// Sites are keyed on the pointer, not the text: each DEX_AT argument is a
// string literal, so the address identifies the statement, and a lookup is one
// hash and usually one probe. The memory checker's strcmp scan is fine once at
// exit and far too slow on every allocation.
//
// At exit the profile is written as JSON to DEX_PROFILE_OUT (default
// dex-profile.json). SIGUSR1 writes a snapshot without stopping the program,
// and SIGINT / SIGTERM write one before the program dies — both only when the
// program has not installed a handler of its own — so a server can be
// profiled and then stopped.

#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <signal.h>
#include <time.h>
#include <fcntl.h>
#include <unistd.h>

// Distinct statements that can be told apart. One past this, allocations are
// filed under "<other sites>" rather than dropped. A power of two, because the
// table is open-addressed with a mask.
#define DEX_PF_SITES 8192
// Timeline points kept. When it fills, adjacent points are merged and the
// interval doubles, so a long run keeps its whole history at a coarser grain.
#define DEX_PF_SAMPLES 4096

#ifdef DEX_SINGLE_THREADED
#define DEX_PF_LOCK() ((void)0)
#define DEX_PF_UNLOCK() ((void)0)
#define DEX_PF_TRYLOCK() 1
#define DEX_PF_TLS
#else
#include <pthread.h>
static pthread_mutex_t dex_pf_mu = PTHREAD_MUTEX_INITIALIZER;
#define DEX_PF_LOCK() pthread_mutex_lock(&dex_pf_mu)
#define DEX_PF_UNLOCK() pthread_mutex_unlock(&dex_pf_mu)
#define DEX_PF_TRYLOCK() (pthread_mutex_trylock(&dex_pf_mu) == 0)
#define DEX_PF_TLS _Thread_local
#endif

// The .dx statement currently executing, set by DEX_AT.
static DEX_PF_TLS const char* dex_pf_site = "<program start>";

typedef struct {
    const char* key;        // the DEX_AT literal; NULL for an empty slot
    long allocs;            // objects allocated here
    long frees;             // of those, how many have been freed
    long long alloc_bytes;  // bytes allocated here, buffer growth included
    long long live_bytes;   // bytes still held by objects allocated here
    long long peak_bytes;   // the most live_bytes ever was
    long long at_peak;      // live_bytes when the whole program was at its peak
} DexPfSite;

static DexPfSite dex_pf_sites[DEX_PF_SITES];
// Slot indices in first-seen order, so a walk skips the empty slots.
static unsigned dex_pf_used[DEX_PF_SITES];
static int dex_pf_nused = 0;
// Index of the "<other sites>" overflow entry once the table is nearly full.
static int dex_pf_overflow = -1;

static long dex_pf_allocs = 0;
static long dex_pf_frees = 0;
static long dex_pf_live_objs = 0;
static long long dex_pf_alloc_bytes = 0;
static long long dex_pf_live_bytes = 0;
static long long dex_pf_peak_bytes = 0;
static double dex_pf_peak_ms = 0;
// The peak at which the per-site at_peak column was last copied. Copying on
// every new high would be a walk over every site per allocation while a
// program is growing, so it is copied only once the peak has moved by 1%.
static long long dex_pf_snap_bytes = -1;

typedef struct {
    double t_ms;
    long long live;  // live bytes at the moment of the sample
    long long high;  // the most live bytes reached since the previous sample
} DexPfSample;

static DexPfSample dex_pf_samples[DEX_PF_SAMPLES];
static int dex_pf_nsamples = 0;
static double dex_pf_interval_ms = 1;
static double dex_pf_last_ms = 0;
static long long dex_pf_high = 0;
static unsigned dex_pf_tick = 0;
static struct timespec dex_pf_t0;

// Set while the profile is being written, so a signal arriving mid-write
// does not start a second write into the same buffer.
static volatile sig_atomic_t dex_pf_writing = 0;
static const char* dex_pf_out = "dex-profile.json";

static double dex_pf_now_ms(void) {
    struct timespec t;
    clock_gettime(CLOCK_MONOTONIC, &t);
    return (double)(t.tv_sec - dex_pf_t0.tv_sec) * 1e3 + (double)(t.tv_nsec - dex_pf_t0.tv_nsec) / 1e6;
}

// Returns the slot for a site, claiming one on first sight. Called with the
// lock held.
static unsigned dex_pf_slot(const char* key) {
    uintptr_t h = (uintptr_t)key;
    h ^= h >> 17;
    h *= 0x9E3779B97F4A7C15ULL;
    unsigned i = (unsigned)(h >> 32) & (DEX_PF_SITES - 1);
    for (;;) {
        DexPfSite* s = &dex_pf_sites[i];
        if (s->key == key) return i;
        if (!s->key) break;
        i = (i + 1) & (DEX_PF_SITES - 1);
    }
    // Keep the table at most seven-eighths full so a probe always ends; the
    // last slot claimed becomes the bucket for everything after it.
    if (dex_pf_nused >= DEX_PF_SITES - DEX_PF_SITES / 8) {
        if (dex_pf_overflow < 0) {
            dex_pf_sites[i].key = "<other sites>";
            dex_pf_used[dex_pf_nused++] = i;
            dex_pf_overflow = (int)i;
        }
        return (unsigned)dex_pf_overflow;
    }
    dex_pf_sites[i].key = key;
    dex_pf_used[dex_pf_nused++] = i;
    return i;
}

static void dex_pf_snapshot_peak(void) {
    for (int k = 0; k < dex_pf_nused; k++) {
        DexPfSite* s = &dex_pf_sites[dex_pf_used[k]];
        s->at_peak = s->live_bytes;
    }
    dex_pf_snap_bytes = dex_pf_live_bytes;
}

static void dex_pf_sample(double now) {
    if (dex_pf_nsamples == DEX_PF_SAMPLES) {
        // Halve the resolution rather than stop recording: merge each pair,
        // keeping the later moment's live total and the higher of the highs.
        int n = 0;
        for (int i = 0; i + 1 < DEX_PF_SAMPLES; i += 2) {
            DexPfSample a = dex_pf_samples[i], b = dex_pf_samples[i + 1];
            dex_pf_samples[n].t_ms = b.t_ms;
            dex_pf_samples[n].live = b.live;
            dex_pf_samples[n].high = a.high > b.high ? a.high : b.high;
            n++;
        }
        dex_pf_nsamples = n;
        dex_pf_interval_ms *= 2;
    }
    DexPfSample* s = &dex_pf_samples[dex_pf_nsamples++];
    s->t_ms = now;
    s->live = dex_pf_live_bytes;
    s->high = dex_pf_high > dex_pf_live_bytes ? dex_pf_high : dex_pf_live_bytes;
    dex_pf_high = dex_pf_live_bytes;
    dex_pf_last_ms = now;
}

// Bookkeeping common to every change in live bytes. Called with the lock held.
static void dex_pf_moved(void) {
    if (dex_pf_live_bytes > dex_pf_high) dex_pf_high = dex_pf_live_bytes;
    if (dex_pf_live_bytes > dex_pf_peak_bytes) {
        dex_pf_peak_bytes = dex_pf_live_bytes;
        // Reading the clock on every new high would cost more than everything
        // else here put together while a program grows; every 64th is plenty
        // for a time the viewer draws at the pixel level.
        if ((dex_pf_tick & 63) == 0) dex_pf_peak_ms = dex_pf_now_ms();
        if (dex_pf_live_bytes > dex_pf_snap_bytes + dex_pf_snap_bytes / 100 + 4096) {
            dex_pf_peak_ms = dex_pf_now_ms();
            dex_pf_snapshot_peak();
        }
    }
    if ((++dex_pf_tick & 63) == 0) {
        double now = dex_pf_now_ms();
        if (now - dex_pf_last_ms >= dex_pf_interval_ms) dex_pf_sample(now);
    }
}

static void dex_pf_on_alloc(void* ptr, size_t size) {
    DexObjHeader* o = (DexObjHeader*)ptr;
    o->pf_bytes = size;
    o->pf_buf = 0;
    DEX_PF_LOCK();
    unsigned i = dex_pf_slot(dex_pf_site);
    o->pf_site = i + 1;
    DexPfSite* s = &dex_pf_sites[i];
    s->allocs++;
    s->alloc_bytes += (long long)size;
    s->live_bytes += (long long)size;
    if (s->live_bytes > s->peak_bytes) s->peak_bytes = s->live_bytes;
    dex_pf_allocs++;
    dex_pf_live_objs++;
    dex_pf_alloc_bytes += (long long)size;
    dex_pf_live_bytes += (long long)size;
    dex_pf_moved();
    DEX_PF_UNLOCK();
}

static void dex_pf_on_free(void* ptr) {
    DexObjHeader* o = (DexObjHeader*)ptr;
    if (!o->pf_site) return;
    DEX_PF_LOCK();
    DexPfSite* s = &dex_pf_sites[o->pf_site - 1];
    long long n = (long long)o->pf_bytes + (long long)o->pf_buf;
    s->frees++;
    s->live_bytes -= n;
    dex_pf_frees++;
    dex_pf_live_objs--;
    dex_pf_live_bytes -= n;
    dex_pf_moved();
    DEX_PF_UNLOCK();
    o->pf_site = 0;
}

// A container's backing buffer is now `bytes` long. The change is held by the
// site that allocated the container — that is what keeps it alive — and the
// growth is charged as allocation to the statement doing the growing, which
// is the one to look at when a loop pushes too much.
static void dex_pf_buf_set(void* ptr, size_t bytes) {
    DexObjHeader* o = (DexObjHeader*)ptr;
    if (!o->pf_site) return;
    long long delta = (long long)bytes - (long long)o->pf_buf;
    if (delta == 0) return;
    o->pf_buf = bytes;
    DEX_PF_LOCK();
    DexPfSite* owner = &dex_pf_sites[o->pf_site - 1];
    owner->live_bytes += delta;
    if (owner->live_bytes > owner->peak_bytes) owner->peak_bytes = owner->live_bytes;
    if (delta > 0) {
        dex_pf_sites[dex_pf_slot(dex_pf_site)].alloc_bytes += delta;
        dex_pf_alloc_bytes += delta;
    }
    dex_pf_live_bytes += delta;
    dex_pf_moved();
    DEX_PF_UNLOCK();
}

// ---- Writing the profile ---------------------------------------------------
//
// The writer may run inside a signal handler, so it uses neither stdio nor
// malloc: a static buffer flushed with write(2), and snprintf only into a
// small stack buffer for numbers.

static int dex_pf_fd = -1;
static char dex_pf_wbuf[16384];
static size_t dex_pf_wlen = 0;

static void dex_pf_flush(void) {
    size_t off = 0;
    while (off < dex_pf_wlen) {
        ssize_t n = write(dex_pf_fd, dex_pf_wbuf + off, dex_pf_wlen - off);
        if (n <= 0) break;
        off += (size_t)n;
    }
    dex_pf_wlen = 0;
}

static void dex_pf_puts(const char* s) {
    while (*s) {
        if (dex_pf_wlen == sizeof(dex_pf_wbuf)) dex_pf_flush();
        dex_pf_wbuf[dex_pf_wlen++] = *s++;
    }
}

static void dex_pf_putq(const char* s) {
    char esc[8];
    dex_pf_puts("\"");
    for (; *s; s++) {
        unsigned char c = (unsigned char)*s;
        if (c == '"' || c == '\\') {
            esc[0] = '\\'; esc[1] = (char)c; esc[2] = 0;
            dex_pf_puts(esc);
        } else if (c < 0x20) {
            snprintf(esc, sizeof(esc), "\\u%04x", c);
            dex_pf_puts(esc);
        } else {
            esc[0] = (char)c; esc[1] = 0;
            dex_pf_puts(esc);
        }
    }
    dex_pf_puts("\"");
}

static void dex_pf_putn(long long n) {
    char num[32];
    snprintf(num, sizeof(num), "%lld", n);
    dex_pf_puts(num);
}

static void dex_pf_putf(double d) {
    char num[32];
    snprintf(num, sizeof(num), "%.3f", d);
    dex_pf_puts(num);
}

// Writes the profile. `locked` says whether to take the lock: a signal handler
// only tries, because the thread it interrupted may be the one holding it, and
// a snapshot read without the lock is at worst a little inconsistent.
static void dex_pf_write(int locked) {
    if (dex_pf_writing) return;
    dex_pf_writing = 1;
    int have = locked ? (DEX_PF_LOCK(), 1) : DEX_PF_TRYLOCK();
    double now = dex_pf_now_ms();
    // The at-peak column is copied at 1% steps, so bring it up to date if the
    // final peak is past the last copy — and never when the program is not at
    // its peak right now, which would overwrite a true copy with a later state.
    if (dex_pf_live_bytes == dex_pf_peak_bytes && dex_pf_snap_bytes != dex_pf_peak_bytes) {
        dex_pf_snapshot_peak();
    }
    dex_pf_fd = open(dex_pf_out, O_WRONLY | O_CREAT | O_TRUNC, 0644);
    if (dex_pf_fd < 0) {
        if (have) DEX_PF_UNLOCK();
        dex_pf_writing = 0;
        return;
    }
    dex_pf_wlen = 0;
    dex_pf_puts("{\"version\":1,\"duration_ms\":");
    dex_pf_putf(now);
    dex_pf_puts(",\"interval_ms\":");
    dex_pf_putf(dex_pf_interval_ms);
    dex_pf_puts(",\"total\":{\"allocs\":");
    dex_pf_putn(dex_pf_allocs);
    dex_pf_puts(",\"frees\":");
    dex_pf_putn(dex_pf_frees);
    dex_pf_puts(",\"alloc_bytes\":");
    dex_pf_putn(dex_pf_alloc_bytes);
    dex_pf_puts(",\"live_objects\":");
    dex_pf_putn(dex_pf_live_objs);
    dex_pf_puts(",\"live_bytes\":");
    dex_pf_putn(dex_pf_live_bytes);
    dex_pf_puts(",\"peak_bytes\":");
    dex_pf_putn(dex_pf_peak_bytes);
    dex_pf_puts(",\"peak_ms\":");
    dex_pf_putf(dex_pf_peak_ms);
    dex_pf_puts("},\"sites\":[");
    for (int k = 0; k < dex_pf_nused; k++) {
        DexPfSite* s = &dex_pf_sites[dex_pf_used[k]];
        if (k) dex_pf_puts(",");
        dex_pf_puts("\n{\"site\":");
        dex_pf_putq(s->key);
        dex_pf_puts(",\"allocs\":");
        dex_pf_putn(s->allocs);
        dex_pf_puts(",\"frees\":");
        dex_pf_putn(s->frees);
        dex_pf_puts(",\"alloc_bytes\":");
        dex_pf_putn(s->alloc_bytes);
        dex_pf_puts(",\"live_bytes\":");
        dex_pf_putn(s->live_bytes);
        dex_pf_puts(",\"peak_bytes\":");
        dex_pf_putn(s->peak_bytes);
        dex_pf_puts(",\"at_peak\":");
        dex_pf_putn(s->at_peak);
        dex_pf_puts("}");
    }
    dex_pf_puts("\n],\"timeline\":[");
    for (int i = 0; i < dex_pf_nsamples; i++) {
        DexPfSample* s = &dex_pf_samples[i];
        if (i) dex_pf_puts(",");
        dex_pf_puts("[");
        dex_pf_putf(s->t_ms);
        dex_pf_puts(",");
        dex_pf_putn(s->live);
        dex_pf_puts(",");
        dex_pf_putn(s->high);
        dex_pf_puts("]");
    }
    // A closing point at the moment of writing, so the chart reaches the end
    // of the run even when nothing allocated during its last stretch.
    if (dex_pf_nsamples) dex_pf_puts(",");
    dex_pf_puts("[");
    dex_pf_putf(now);
    dex_pf_puts(",");
    dex_pf_putn(dex_pf_live_bytes);
    dex_pf_puts(",");
    dex_pf_putn(dex_pf_high > dex_pf_live_bytes ? dex_pf_high : dex_pf_live_bytes);
    dex_pf_puts("]]}\n");
    dex_pf_flush();
    close(dex_pf_fd);
    dex_pf_fd = -1;
    if (have) DEX_PF_UNLOCK();
    dex_pf_writing = 0;
}

static void dex_pf_fmt_bytes(char* out, size_t n, long long b) {
    if (b >= 1024LL * 1024 * 1024) snprintf(out, n, "%.1f GB", (double)b / (1024.0 * 1024 * 1024));
    else if (b >= 1024LL * 1024) snprintf(out, n, "%.1f MB", (double)b / (1024.0 * 1024));
    else if (b >= 1024) snprintf(out, n, "%.1f KB", (double)b / 1024.0);
    else snprintf(out, n, "%lld B", b);
}

static void dex_pf_at_exit(void) {
    dex_pf_write(1);
    if (getenv("DEX_PROFILE_QUIET")) return;
    char peak[32], total[32], live[32];
    dex_pf_fmt_bytes(peak, sizeof(peak), dex_pf_peak_bytes);
    dex_pf_fmt_bytes(total, sizeof(total), dex_pf_alloc_bytes);
    dex_pf_fmt_bytes(live, sizeof(live), dex_pf_live_bytes);
    fflush(stdout);
    fprintf(stderr, "\nprofile: peak %s at %.0f ms, %s allocated in %ld objects, %s live at exit\n",
            peak, dex_pf_peak_ms, total, dex_pf_allocs, live);
    // The few lines that held the most at the peak are the usual answer.
    int top[5] = {-1, -1, -1, -1, -1};
    for (int k = 0; k < dex_pf_nused; k++) {
        int i = (int)dex_pf_used[k];
        long long v = dex_pf_sites[i].at_peak;
        if (v <= 0) continue;
        for (int j = 0; j < 5; j++) {
            if (top[j] < 0 || v > dex_pf_sites[top[j]].at_peak) {
                memmove(&top[j + 1], &top[j], sizeof(int) * (size_t)(4 - j));
                top[j] = i;
                break;
            }
        }
    }
    for (int j = 0; j < 5 && top[j] >= 0; j++) {
        char b[32];
        dex_pf_fmt_bytes(b, sizeof(b), dex_pf_sites[top[j]].at_peak);
        fprintf(stderr, "  %10s at peak  %s\n", b, dex_pf_sites[top[j]].key);
    }
    fprintf(stderr, "  written to %s\n", dex_pf_out);
    fflush(stderr);
}

static void dex_pf_on_signal(int sig) {
    // Written here rather than deferred to the next allocation: a server
    // parked in its event wait may not allocate again for a long time.
    dex_pf_write(0);
    if (sig == SIGUSR1) return;
    signal(sig, SIG_DFL);
    raise(sig);
}

// Installs a handler only where the program has none, so the profiler never
// changes what a signal does to a program that cares.
static void dex_pf_catch(int sig) {
    struct sigaction cur;
    if (sigaction(sig, NULL, &cur) != 0) return;
    if (cur.sa_handler != SIG_DFL) return;
    struct sigaction sa;
    memset(&sa, 0, sizeof(sa));
    sa.sa_handler = dex_pf_on_signal;
    sigemptyset(&sa.sa_mask);
    sa.sa_flags = SA_RESTART;
    sigaction(sig, &sa, NULL);
}

static void dex_pf_init(void) {
    clock_gettime(CLOCK_MONOTONIC, &dex_pf_t0);
    const char* out = getenv("DEX_PROFILE_OUT");
    if (out && *out) dex_pf_out = out;
    const char* iv = getenv("DEX_PROFILE_INTERVAL_MS");
    if (iv && atof(iv) > 0) dex_pf_interval_ms = atof(iv);
    dex_pf_sample(0);
    dex_pf_catch(SIGUSR1);
    dex_pf_catch(SIGINT);
    dex_pf_catch(SIGTERM);
    atexit(dex_pf_at_exit);
}

static inline void dex_pf_restore(const char** saved) { dex_pf_site = *saved; }

#define DEX_AT(site) (dex_pf_site = (site))
// Emitted at the top of every function: puts the caller's statement back when
// the function returns, so whatever the rest of the calling statement
// allocates is charged to it rather than to the callee's last line.
#define DEX_PF_FRAME() \
    const char* _dex_pf_caller __attribute__((cleanup(dex_pf_restore), unused)) = dex_pf_site
