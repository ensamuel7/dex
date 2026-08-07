
/* event_loop.c — Platform-abstracted event loop (epoll/kqueue) + worker thread pool
 *
 * Provides:
 *   - DexEventLoop:  unified I/O multiplexing over epoll (Linux) and kqueue (macOS/BSD)
 *   - DexThreadPool: fixed-size worker thread pool with condvar work queue
 *   - Notify pipe helpers for worker→event-loop signaling
 *
 * All symbols are static to avoid collisions when linked into a single translation unit.
 */

#include <fcntl.h>
#include <unistd.h>
#include <stdlib.h>
#include <string.h>
#include <pthread.h>

#ifdef __linux__
#include <sys/epoll.h>
#endif

#ifdef __APPLE__
#include <sys/types.h>
#include <sys/event.h>
#include <sys/time.h>
#endif

/* ── Event flags ─────────────────────────────────────────────────── */

#define DEX_EV_READ  1
#define DEX_EV_WRITE 2
#define DEX_EV_ERROR 4

/* ── Types ───────────────────────────────────────────────────────── */

typedef struct {
    int   fd;
    int   events;   /* bitmask of DEX_EV_* */
    void* user_data;
} DexEvent;

typedef struct {
#ifdef __linux__
    int epfd;
#elif defined(__APPLE__)
    int kqfd;
#endif
    int max_events;
} DexEventLoop;

/* ── Helpers ─────────────────────────────────────────────────────── */

static void dex_set_nonblocking(int fd) {
    int flags = fcntl(fd, F_GETFL, 0);
    if (flags < 0) flags = 0;
    fcntl(fd, F_SETFL, flags | O_NONBLOCK);
}

/* ── Event loop core ─────────────────────────────────────────────── */

static DexEventLoop* dex_ev_create(int max_events) {
    DexEventLoop* loop = (DexEventLoop*)calloc(1, sizeof(DexEventLoop));
    if (!loop) return NULL;
    loop->max_events = max_events;

#ifdef __linux__
    loop->epfd = epoll_create1(0);
    if (loop->epfd < 0) { free(loop); return NULL; }
#elif defined(__APPLE__)
    loop->kqfd = kqueue();
    if (loop->kqfd < 0) { free(loop); return NULL; }
#endif

    return loop;
}

static void dex_ev_destroy(DexEventLoop* loop) {
    if (!loop) return;
#ifdef __linux__
    close(loop->epfd);
#elif defined(__APPLE__)
    close(loop->kqfd);
#endif
    free(loop);
}

static int dex_ev_add(DexEventLoop* loop, int fd, int events, void* user_data) {
#ifdef __linux__
    struct epoll_event ev;
    memset(&ev, 0, sizeof(ev));
    ev.data.ptr = user_data;
    if (events & DEX_EV_READ)  ev.events |= EPOLLIN;
    if (events & DEX_EV_WRITE) ev.events |= EPOLLOUT;
    return epoll_ctl(loop->epfd, EPOLL_CTL_ADD, fd, &ev);
#elif defined(__APPLE__)
    struct kevent changes[2];
    int nchanges = 0;
    if (events & DEX_EV_READ) {
        EV_SET(&changes[nchanges], fd, EVFILT_READ, EV_ADD | EV_ENABLE, 0, 0, user_data);
        nchanges++;
    }
    if (events & DEX_EV_WRITE) {
        EV_SET(&changes[nchanges], fd, EVFILT_WRITE, EV_ADD | EV_ENABLE, 0, 0, user_data);
        nchanges++;
    }
    return kevent(loop->kqfd, changes, nchanges, NULL, 0, NULL);
#else
    return -1;
#endif
}

static int dex_ev_mod(DexEventLoop* loop, int fd, int events, void* user_data) {
#ifdef __linux__
    struct epoll_event ev;
    memset(&ev, 0, sizeof(ev));
    ev.data.ptr = user_data;
    if (events & DEX_EV_READ)  ev.events |= EPOLLIN;
    if (events & DEX_EV_WRITE) ev.events |= EPOLLOUT;
    return epoll_ctl(loop->epfd, EPOLL_CTL_MOD, fd, &ev);
#elif defined(__APPLE__)
    /* kqueue: disable both, then re-enable requested */
    struct kevent changes[4];
    int nchanges = 0;
    EV_SET(&changes[nchanges], fd, EVFILT_READ, EV_DELETE, 0, 0, NULL);
    nchanges++;
    EV_SET(&changes[nchanges], fd, EVFILT_WRITE, EV_DELETE, 0, 0, NULL);
    nchanges++;
    /* Ignore errors from deleting filters that were not registered */
    kevent(loop->kqfd, changes, nchanges, NULL, 0, NULL);

    nchanges = 0;
    if (events & DEX_EV_READ) {
        EV_SET(&changes[nchanges], fd, EVFILT_READ, EV_ADD | EV_ENABLE, 0, 0, user_data);
        nchanges++;
    }
    if (events & DEX_EV_WRITE) {
        EV_SET(&changes[nchanges], fd, EVFILT_WRITE, EV_ADD | EV_ENABLE, 0, 0, user_data);
        nchanges++;
    }
    if (nchanges > 0) {
        return kevent(loop->kqfd, changes, nchanges, NULL, 0, NULL);
    }
    return 0;
#else
    return -1;
#endif
}

static int dex_ev_del(DexEventLoop* loop, int fd) {
#ifdef __linux__
    return epoll_ctl(loop->epfd, EPOLL_CTL_DEL, fd, NULL);
#elif defined(__APPLE__)
    struct kevent changes[2];
    EV_SET(&changes[0], fd, EVFILT_READ, EV_DELETE, 0, 0, NULL);
    EV_SET(&changes[1], fd, EVFILT_WRITE, EV_DELETE, 0, 0, NULL);
    /* Ignore errors — filter may not be registered */
    kevent(loop->kqfd, changes, 2, NULL, 0, NULL);
    return 0;
#else
    return -1;
#endif
}

static int dex_ev_wait(DexEventLoop* loop, DexEvent* out, int max, int timeout_ms) {
#ifdef __linux__
    struct epoll_event* events = (struct epoll_event*)alloca(sizeof(struct epoll_event) * max);
    int n = epoll_wait(loop->epfd, events, max, timeout_ms);
    for (int i = 0; i < n; i++) {
        out[i].user_data = events[i].data.ptr;
        out[i].events = 0;
        if (events[i].events & EPOLLIN)  out[i].events |= DEX_EV_READ;
        if (events[i].events & EPOLLOUT) out[i].events |= DEX_EV_WRITE;
        if (events[i].events & (EPOLLERR | EPOLLHUP)) out[i].events |= DEX_EV_ERROR;
        out[i].fd = -1; /* fd recovered from user_data in caller */
    }
    return n;
#elif defined(__APPLE__)
    struct kevent* kevs = (struct kevent*)alloca(sizeof(struct kevent) * max);
    struct timespec ts;
    struct timespec* tsp = NULL;
    if (timeout_ms >= 0) {
        ts.tv_sec = timeout_ms / 1000;
        ts.tv_nsec = (timeout_ms % 1000) * 1000000L;
        tsp = &ts;
    }
    int n = kevent(loop->kqfd, NULL, 0, kevs, max, tsp);
    for (int i = 0; i < n; i++) {
        out[i].user_data = kevs[i].udata;
        out[i].fd = (int)kevs[i].ident;
        out[i].events = 0;
        if (kevs[i].filter == EVFILT_READ)  out[i].events |= DEX_EV_READ;
        if (kevs[i].filter == EVFILT_WRITE) out[i].events |= DEX_EV_WRITE;
        if (kevs[i].flags & EV_ERROR)       out[i].events |= DEX_EV_ERROR;
        if (kevs[i].flags & EV_EOF)         out[i].events |= DEX_EV_READ; /* treat EOF as readable */
    }
    return n;
#else
    return -1;
#endif
}

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

/* ── Notify pipe (worker → event loop signaling) ─────────────────── */

typedef struct {
    int read_fd;
    int write_fd;
} DexNotifyPipe;

static int dex_notify_pipe_init(DexNotifyPipe* np) {
    int fds[2];
    if (pipe(fds) < 0) return -1;
    np->read_fd = fds[0];
    np->write_fd = fds[1];
    dex_set_nonblocking(np->read_fd);
    dex_set_nonblocking(np->write_fd);
    return 0;
}

static void dex_notify_pipe_signal(DexNotifyPipe* np) {
    char c = 1;
    (void)write(np->write_fd, &c, 1);
}

static void dex_notify_pipe_drain(DexNotifyPipe* np) {
    char buf[64];
    while (read(np->read_fd, buf, sizeof(buf)) > 0) {}
}

static void dex_notify_pipe_close(DexNotifyPipe* np) {
    close(np->read_fd);
    close(np->write_fd);
}
