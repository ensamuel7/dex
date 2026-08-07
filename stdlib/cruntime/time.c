
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

void dex_time_sleep(int ms) {
    usleep((useconds_t)(ms * 1000));
}

// --- Timer/Interval support ---
#include <pthread.h>
#include <stdlib.h>

typedef struct {
    void (*fn)(void);
    int ms;
    volatile int cancelled;
} DexTimerCtx;

#define DEX_MAX_TIMERS 256
static DexTimerCtx* dex_timers[DEX_MAX_TIMERS];
static int dex_timer_count = 0;

static void* _dex_timeout_worker(void* arg) {
    DexTimerCtx* ctx = (DexTimerCtx*)arg;
    usleep((useconds_t)(ctx->ms * 1000));
    if (!ctx->cancelled) {
        ctx->fn();
    }
    free(ctx);
    return NULL;
}

static void* _dex_interval_worker(void* arg) {
    DexTimerCtx* ctx = (DexTimerCtx*)arg;
    while (!ctx->cancelled) {
        usleep((useconds_t)(ctx->ms * 1000));
        if (!ctx->cancelled) {
            ctx->fn();
        }
    }
    return NULL;
}

void dex_time_set_timeout(void (*fn)(void), int ms) {
    DexTimerCtx* ctx = (DexTimerCtx*)malloc(sizeof(DexTimerCtx));
    ctx->fn = fn;
    ctx->ms = ms;
    ctx->cancelled = 0;
    pthread_t tid;
    pthread_create(&tid, NULL, _dex_timeout_worker, ctx);
    pthread_detach(tid);
}

int dex_time_set_interval(void (*fn)(void), int ms) {
    if (dex_timer_count >= DEX_MAX_TIMERS) return -1;
    DexTimerCtx* ctx = (DexTimerCtx*)malloc(sizeof(DexTimerCtx));
    ctx->fn = fn;
    ctx->ms = ms;
    ctx->cancelled = 0;
    int id = dex_timer_count;
    dex_timers[id] = ctx;
    dex_timer_count++;
    pthread_t tid;
    pthread_create(&tid, NULL, _dex_interval_worker, ctx);
    pthread_detach(tid);
    return id;
}

void dex_time_clear_interval(int id) {
    if (id >= 0 && id < dex_timer_count && dex_timers[id]) {
        dex_timers[id]->cancelled = 1;
    }
}
