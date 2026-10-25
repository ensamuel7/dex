
#include <time.h>
#include <unistd.h>

long dex_time_now() {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (long)ts.tv_sec * 1000L + (long)ts.tv_nsec / 1000000L;
}

long dex_time_now_ns() {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (long)ts.tv_sec * 1000000000L + (long)ts.tv_nsec;
}

#include <errno.h>

static void dex_sleep_ms(long ms) {
    if (ms <= 0) return;
    struct timespec ts;
    ts.tv_sec = ms / 1000;
    ts.tv_nsec = (ms % 1000) * 1000000L;
    while (nanosleep(&ts, &ts) == -1 && errno == EINTR) {}
}

void dex_time_sleep(int ms) {
    dex_sleep_ms(ms);
}

// --- Timer/Interval support ---
#include <pthread.h>
#include <stdlib.h>
#include <stdatomic.h>

typedef struct {
    void (*fn)(void);
    int ms;
    atomic_int cancelled;
} DexTimerCtx;

#define DEX_MAX_TIMERS 256
static DexTimerCtx* dex_timers[DEX_MAX_TIMERS];
static int dex_timer_count = 0;
static pthread_mutex_t dex_timers_lock = PTHREAD_MUTEX_INITIALIZER;

static void* _dex_timeout_worker(void* arg) {
    DexTimerCtx* ctx = (DexTimerCtx*)arg;
    dex_sleep_ms(ctx->ms);
    if (!atomic_load(&ctx->cancelled)) {
        ctx->fn();
    }
    free(ctx);
    return NULL;
}

static void* _dex_interval_worker(void* arg) {
    DexTimerCtx* ctx = (DexTimerCtx*)arg;
    while (!atomic_load(&ctx->cancelled)) {
        dex_sleep_ms(ctx->ms);
        if (!atomic_load(&ctx->cancelled)) {
            ctx->fn();
        }
    }
    return NULL;
}

void dex_time_set_timeout(void (*fn)(void), int ms) {
    DexTimerCtx* ctx = (DexTimerCtx*)dex_xmalloc(sizeof(DexTimerCtx));
    if (!ctx) return;
    ctx->fn = fn;
    ctx->ms = ms;
    atomic_init(&ctx->cancelled, 0);
    pthread_t tid;
    if (pthread_create(&tid, NULL, _dex_timeout_worker, ctx) != 0) {
        free(ctx);
        return;
    }
    pthread_detach(tid);
}

int dex_time_set_interval(void (*fn)(void), int ms) {
    DexTimerCtx* ctx = (DexTimerCtx*)dex_xmalloc(sizeof(DexTimerCtx));
    if (!ctx) return -1;
    ctx->fn = fn;
    ctx->ms = ms;
    atomic_init(&ctx->cancelled, 0);
    pthread_mutex_lock(&dex_timers_lock);
    if (dex_timer_count >= DEX_MAX_TIMERS) {
        pthread_mutex_unlock(&dex_timers_lock);
        free(ctx);
        return -1;
    }
    int id = dex_timer_count;
    dex_timers[id] = ctx;
    dex_timer_count++;
    pthread_mutex_unlock(&dex_timers_lock);
    pthread_t tid;
    if (pthread_create(&tid, NULL, _dex_interval_worker, ctx) != 0) {
        atomic_store(&ctx->cancelled, 1);
        return -1;
    }
    pthread_detach(tid);
    return id;
}

void dex_time_clear_interval(int id) {
    pthread_mutex_lock(&dex_timers_lock);
    if (id >= 0 && id < dex_timer_count && dex_timers[id]) {
        atomic_store(&dex_timers[id]->cancelled, 1);
    }
    pthread_mutex_unlock(&dex_timers_lock);
}

// --- Time formatting ---
#include <stdio.h>

const char* dex_time_format(long timestamp, const char* layout) {
    time_t t = (time_t)timestamp;
    struct tm* tm_info = gmtime(&t);
    if (!tm_info) return dex_xstrdup("");
    char* result = (char*)dex_xmalloc(64);
    if (!result) return dex_xstrdup("");
    if (strcmp(layout, "iso8601") == 0 || strcmp(layout, "ISO8601") == 0) {
        strftime(result, 64, "%Y-%m-%dT%H:%M:%SZ", tm_info);
    } else {
        strftime(result, 64, layout, tm_info);
    }
    return result;
}

const char* dex_time_iso_now(void) {
    time_t t = time(NULL);
    struct tm* tm_info = gmtime(&t);
    if (!tm_info) return dex_xstrdup("");
    char* result = (char*)dex_xmalloc(64);
    if (!result) return dex_xstrdup("");
    strftime(result, 64, "%Y-%m-%dT%H:%M:%SZ", tm_info);
    return result;
}

long dex_time_unix_now(void) {
    return (long)time(NULL);
}
