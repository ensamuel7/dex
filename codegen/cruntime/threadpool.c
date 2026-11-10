/* threadpool.c — Worker thread pools for spawn and the servers
 *
 * Provides:
 *   - DexThreadPool: worker thread pool with condvar work queue. Fixed-size
 *     (dex_pool_create, the http and ws handler pools) or elastic
 *     (dex_pool_create_elastic, the spawn pool: grows a thread whenever no
 *     worker is idle, so a spawn starts even if every earlier one never
 *     returns; extra threads exit after DEX_POOL_IDLE_EXIT_SEC idle)
 *   - Global spawn pool: lazily initialized, used by all spawn expressions
 *
 * All symbols are static to avoid collisions when linked into a single translation unit.
 */

#include <stdlib.h>
#include <string.h>
#include <pthread.h>
#include <unistd.h>
#include <stdio.h>
#include <time.h>
#include <sys/time.h>

/* Elastic (spawn) pool: hard ceiling on threads, and how long a thread above
 * the base size waits for work before exiting. */
#define DEX_SPAWN_MAX_THREADS 256
#define DEX_POOL_IDLE_EXIT_SEC 60

/* ── Worker thread pool ──────────────────────────────────────────── */

typedef struct DexWorkItem {
    void (*func)(void*);
    void* arg;
    struct DexWorkItem* next;
} DexWorkItem;

typedef struct {
    pthread_t*      threads;
    int             num_threads;
    pthread_mutex_t mutex;
    pthread_cond_t  cond;
    DexWorkItem*    head;
    DexWorkItem*    tail;
    int             shutdown;
    /* Elastic pools only; everything below is guarded by mutex. */
    int             elastic;
    int             base_threads; /* slots [0, base) never idle-exit */
    int             max_threads;  /* capacity of threads/slot_state */
    int             live;         /* threads started and not exited */
    int             idle;         /* threads waiting for work */
    int             pending;      /* items queued, not yet taken */
    int             warned;       /* cap warning printed */
    unsigned char*  slot_state;   /* 0 empty, 1 running, 2 exited (join me) */
} DexThreadPool;

static void* dex_pool_worker(void* arg) {
    DexThreadPool* pool = (DexThreadPool*)arg;
    char alt[DEX_ALTSTACK_SIZE];
    size_t stack = 0;
    pthread_attr_t attr;
    pthread_attr_init(&attr);
    pthread_attr_getstacksize(&attr, &stack);
    pthread_attr_destroy(&attr);
    dex_guard_thread((char*)&stack, stack, alt);
    for (;;) {
        pthread_mutex_lock(&pool->mutex);
        while (!pool->head && !pool->shutdown) {
            pthread_cond_wait(&pool->cond, &pool->mutex);
        }
        if (pool->shutdown && !pool->head) {
            pthread_mutex_unlock(&pool->mutex);
            break;
        }
        DexWorkItem* item = pool->head;
        pool->head = item->next;
        if (!pool->head) pool->tail = NULL;
        pthread_mutex_unlock(&pool->mutex);

        item->func(item->arg);
        free(item);
    }
    return NULL;
}

static int dex_pool_default_threads(int num_threads) {
    if (num_threads <= 0) {
#ifdef _SC_NPROCESSORS_ONLN
        long ncpu = sysconf(_SC_NPROCESSORS_ONLN);
        num_threads = (int)(ncpu > 0 ? ncpu : 4);
#else
        num_threads = 4;
#endif
        if (num_threads > 16) num_threads = 16;
        if (num_threads < 4) num_threads = 4;
    }
    return num_threads;
}

static DexThreadPool* dex_pool_create(int num_threads) {
    num_threads = dex_pool_default_threads(num_threads);

    DexThreadPool* pool = (DexThreadPool*)dex_xcalloc(1, sizeof(DexThreadPool));
    pool->num_threads = num_threads;
    pool->threads = (pthread_t*)dex_xcalloc(num_threads, sizeof(pthread_t));
    pthread_mutex_init(&pool->mutex, NULL);
    pthread_cond_init(&pool->cond, NULL);
    pool->head = NULL;
    pool->tail = NULL;
    pool->shutdown = 0;

    for (int i = 0; i < num_threads; i++) {
        pthread_create(&pool->threads[i], NULL, dex_pool_worker, pool);
    }
    return pool;
}

/* ── Elastic pool ────────────────────────────────────────────────── */

typedef struct {
    DexThreadPool* pool;
    int            slot;
} DexElasticArg;

static void* dex_pool_elastic_worker(void* arg) {
    DexElasticArg a = *(DexElasticArg*)arg;
    free(arg);
    DexThreadPool* pool = a.pool;
    char alt[DEX_ALTSTACK_SIZE];
    size_t stack = 0;
    pthread_attr_t attr;
    pthread_attr_init(&attr);
    pthread_attr_getstacksize(&attr, &stack);
    pthread_attr_destroy(&attr);
    dex_guard_thread((char*)&stack, stack, alt);
    int extra = a.slot >= pool->base_threads;
    pthread_mutex_lock(&pool->mutex);
    for (;;) {
        int timed_out = 0;
        pool->idle++;
        while (!pool->head && !pool->shutdown && !timed_out) {
            if (extra) {
                struct timeval now;
                struct timespec until;
                gettimeofday(&now, NULL);
                until.tv_sec = now.tv_sec + DEX_POOL_IDLE_EXIT_SEC;
                until.tv_nsec = (long)now.tv_usec * 1000;
                if (pthread_cond_timedwait(&pool->cond, &pool->mutex, &until) != 0 && !pool->head)
                    timed_out = 1;
            } else {
                pthread_cond_wait(&pool->cond, &pool->mutex);
            }
        }
        pool->idle--;
        if (!pool->head) {
            /* Shutdown with an empty queue: the destroyer joins this slot.
             * Idle timeout: mark the slot for the next grow (or the destroyer)
             * to join. Either way, touch nothing of pool after unlocking. */
            pool->live--;
            if (!pool->shutdown) pool->slot_state[a.slot] = 2;
            pthread_mutex_unlock(&pool->mutex);
            break;
        }
        DexWorkItem* item = pool->head;
        pool->head = item->next;
        if (!pool->head) pool->tail = NULL;
        pool->pending--;
        pthread_mutex_unlock(&pool->mutex);

        item->func(item->arg);
        free(item);

        pthread_mutex_lock(&pool->mutex);
    }
    return NULL;
}

/* Start one more worker. Caller holds pool->mutex. At the cap, or if the OS
 * refuses a thread, the item waits in the queue as on a fixed pool. */
static void dex_pool_grow_locked(DexThreadPool* pool) {
    int slot = -1;
    if (pool->live < pool->max_threads) {
        for (int i = 0; i < pool->max_threads; i++) {
            if (pool->slot_state[i] == 2) {
                /* Exited after an idle timeout; it no longer needs the mutex. */
                pthread_join(pool->threads[i], NULL);
                pool->slot_state[i] = 0;
            }
            if (pool->slot_state[i] == 0) { slot = i; break; }
        }
    }
    if (slot >= 0) {
        DexElasticArg* a = (DexElasticArg*)dex_xmalloc(sizeof(DexElasticArg));
        a->pool = pool;
        a->slot = slot;
        if (pthread_create(&pool->threads[slot], NULL, dex_pool_elastic_worker, a) == 0) {
            pool->slot_state[slot] = 1;
            pool->live++;
            return;
        }
        free(a);
    }
    if (!pool->warned) {
        pool->warned = 1;
        if (pool->live >= pool->max_threads)
            fprintf(stderr, "dex: %d spawned tasks are running at once (the limit); "
                            "further spawns wait until one finishes\n", pool->live);
        else
            fprintf(stderr, "dex: could not start a thread for spawn (%d running); "
                            "further spawns wait until one finishes\n", pool->live);
    }
}

static DexThreadPool* dex_pool_create_elastic(int base_threads, int max_threads) {
    base_threads = dex_pool_default_threads(base_threads);
    if (max_threads < base_threads) max_threads = base_threads;

    DexThreadPool* pool = (DexThreadPool*)dex_xcalloc(1, sizeof(DexThreadPool));
    pool->elastic = 1;
    pool->base_threads = base_threads;
    pool->max_threads = max_threads;
    pool->num_threads = max_threads;
    pool->threads = (pthread_t*)dex_xcalloc(max_threads, sizeof(pthread_t));
    pool->slot_state = (unsigned char*)dex_xcalloc(max_threads, 1);
    pthread_mutex_init(&pool->mutex, NULL);
    pthread_cond_init(&pool->cond, NULL);

    pthread_mutex_lock(&pool->mutex);
    for (int i = 0; i < base_threads; i++) dex_pool_grow_locked(pool);
    pthread_mutex_unlock(&pool->mutex);
    return pool;
}

static void dex_pool_submit(DexThreadPool* pool, void (*func)(void*), void* arg) {
    DexWorkItem* item = (DexWorkItem*)dex_xmalloc(sizeof(DexWorkItem));
    item->func = func;
    item->arg = arg;
    item->next = NULL;

    pthread_mutex_lock(&pool->mutex);
    if (pool->tail) {
        pool->tail->next = item;
    } else {
        pool->head = item;
    }
    pool->tail = item;
    if (pool->elastic) {
        pool->pending++;
        /* Every idle worker already has a queued item to take: start one more. */
        if (pool->pending > pool->idle) dex_pool_grow_locked(pool);
    }
    pthread_cond_signal(&pool->cond);
    pthread_mutex_unlock(&pool->mutex);
}

static void dex_pool_destroy(DexThreadPool* pool) {
    if (!pool) return;
    pthread_mutex_lock(&pool->mutex);
    pool->shutdown = 1;
    pthread_cond_broadcast(&pool->cond);
    pthread_mutex_unlock(&pool->mutex);

    if (pool->elastic) {
        /* Workers drain the queue before exiting, and a task still running
         * may spawn (and so start a thread) meanwhile: claim and join slots
         * until none is left. A running task's own slot is never empty. */
        for (;;) {
            pthread_t t;
            int found = 0;
            pthread_mutex_lock(&pool->mutex);
            for (int i = 0; i < pool->max_threads; i++) {
                if (pool->slot_state[i]) {
                    t = pool->threads[i];
                    pool->slot_state[i] = 0;
                    found = 1;
                    break;
                }
            }
            pthread_mutex_unlock(&pool->mutex);
            if (!found) break;
            pthread_join(t, NULL);
        }
    } else {
        for (int i = 0; i < pool->num_threads; i++) {
            pthread_join(pool->threads[i], NULL);
        }
    }
    /* Free remaining items */
    DexWorkItem* item = pool->head;
    while (item) {
        DexWorkItem* next = item->next;
        free(item);
        item = next;
    }
    pthread_mutex_destroy(&pool->mutex);
    pthread_cond_destroy(&pool->cond);
    free(pool->threads);
    free(pool->slot_state);
    free(pool);
}

/* ── Global spawn pool ───────────────────────────────────────────── */

static DexThreadPool* _dex_spawn_pool = NULL;
static pthread_once_t _dex_spawn_pool_once = PTHREAD_ONCE_INIT;

static void dex_spawn_pool_create(void) {
    /* Base size from the CPU count as before; grows so a spawn never waits
     * behind tasks that never return (servers, forever loops). */
    _dex_spawn_pool = dex_pool_create_elastic(0, DEX_SPAWN_MAX_THREADS);
}

static void dex_spawn_pool_init(void) {
    pthread_once(&_dex_spawn_pool_once, dex_spawn_pool_create);
}

static void dex_spawn_pool_shutdown(void) {
    if (_dex_spawn_pool) {
        dex_pool_destroy(_dex_spawn_pool);
        _dex_spawn_pool = NULL;
    }
}

static void dex_spawn_submit(void (*func)(void*), void* arg) {
    dex_spawn_pool_init();
    dex_pool_submit(_dex_spawn_pool, func, arg);
}
