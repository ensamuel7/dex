/* threadpool.c — Fixed-size worker thread pool for spawn and event loop
 *
 * Provides:
 *   - DexThreadPool: fixed-size worker thread pool with condvar work queue
 *   - Global spawn pool: lazily initialized, used by all spawn expressions
 *
 * All symbols are static to avoid collisions when linked into a single translation unit.
 */

#include <stdlib.h>
#include <string.h>
#include <pthread.h>
#include <unistd.h>

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
} DexThreadPool;

static void* dex_pool_worker(void* arg) {
    DexThreadPool* pool = (DexThreadPool*)arg;
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

static DexThreadPool* dex_pool_create(int num_threads) {
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

    DexThreadPool* pool = (DexThreadPool*)calloc(1, sizeof(DexThreadPool));
    pool->num_threads = num_threads;
    pool->threads = (pthread_t*)calloc(num_threads, sizeof(pthread_t));
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

static void dex_pool_submit(DexThreadPool* pool, void (*func)(void*), void* arg) {
    DexWorkItem* item = (DexWorkItem*)malloc(sizeof(DexWorkItem));
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
    pthread_cond_signal(&pool->cond);
    pthread_mutex_unlock(&pool->mutex);
}

static void dex_pool_destroy(DexThreadPool* pool) {
    if (!pool) return;
    pthread_mutex_lock(&pool->mutex);
    pool->shutdown = 1;
    pthread_cond_broadcast(&pool->cond);
    pthread_mutex_unlock(&pool->mutex);

    for (int i = 0; i < pool->num_threads; i++) {
        pthread_join(pool->threads[i], NULL);
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
    free(pool);
}

/* ── Global spawn pool ───────────────────────────────────────────── */

static DexThreadPool* _dex_spawn_pool = NULL;

static void dex_spawn_pool_init(void) {
    if (!_dex_spawn_pool) {
        _dex_spawn_pool = dex_pool_create(0); /* 0 = auto-detect CPU count */
    }
}

static void dex_spawn_pool_shutdown(void) {
    if (_dex_spawn_pool) {
        dex_pool_destroy(_dex_spawn_pool);
        _dex_spawn_pool = NULL;
    }
}

static void dex_spawn_submit(void (*func)(void*), void* arg) {
    dex_pool_submit(_dex_spawn_pool, func, arg);
}
