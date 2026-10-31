
/* event_loop.c — Platform-abstracted event loop (epoll/kqueue)
 *
 * Provides:
 *   - DexEventLoop:  unified I/O multiplexing over epoll (Linux) and kqueue (macOS/BSD)
 *   - Notify pipe helpers for worker→event-loop signaling
 *
 * The thread pool (DexThreadPool) lives in threadpool.c and is emitted before this file.
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
    DexEventLoop* loop = (DexEventLoop*)dex_xcalloc(1, sizeof(DexEventLoop));
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

#if defined(__APPLE__)
/* Remove both of a descriptor's filters, whichever are registered.
 *
 * Without EV_RECEIPT, kevent() stops at the first change that fails and
 * applies none after it. Deleting a read filter that was never added fails
 * with ENOENT, so the write-filter delete queued behind it was silently
 * dropped: a connection that had been waiting to write kept its write filter
 * when it went back to reading, and from then on was reported twice per wait.
 * The http loop freed a connection on one of those events and then read it on
 * the other. EV_RECEIPT makes kevent apply every change and report each
 * outcome separately; a filter that was not there is not an error here. */
static void dex_kq_delete_filters(int kqfd, int fd) {
    struct kevent changes[2];
    struct kevent receipts[2];
    EV_SET(&changes[0], fd, EVFILT_READ, EV_DELETE | EV_RECEIPT, 0, 0, NULL);
    EV_SET(&changes[1], fd, EVFILT_WRITE, EV_DELETE | EV_RECEIPT, 0, 0, NULL);
    (void)kevent(kqfd, changes, 2, receipts, 2, NULL);
}
#endif

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
    /* kqueue: remove both, then add back what was requested */
    struct kevent changes[2];
    int nchanges = 0;
    dex_kq_delete_filters(loop->kqfd, fd);

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
    dex_kq_delete_filters(loop->kqfd, fd);
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
    if (n <= 0) return n;
    /* kqueue reports each filter separately, so a descriptor watched for both
     * reading and writing can come back twice in one batch. epoll reports a
     * descriptor once with both bits set, and callers are written for that: a
     * handler that closes and frees a connection on one event must not find a
     * second event for it later in the same batch. Fold them together. */
    int m = 0;
    for (int i = 0; i < n; i++) {
        int bits = 0;
        if (kevs[i].filter == EVFILT_READ)  bits |= DEX_EV_READ;
        if (kevs[i].filter == EVFILT_WRITE) bits |= DEX_EV_WRITE;
        if (kevs[i].flags & EV_ERROR)       bits |= DEX_EV_ERROR;
        if (kevs[i].flags & EV_EOF)         bits |= DEX_EV_READ; /* treat EOF as readable */
        int j = 0;
        while (j < m && !(out[j].fd == (int)kevs[i].ident && out[j].user_data == kevs[i].udata)) j++;
        if (j < m) {
            out[j].events |= bits;
            continue;
        }
        out[m].user_data = kevs[i].udata;
        out[m].fd = (int)kevs[i].ident;
        out[m].events = bits;
        m++;
    }
    return m;
#else
    return -1;
#endif
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

/* Set by the WebSocket listener when one is running.
 *
 * The HTTP and WebSocket servers each install a SIGTERM handler, and whichever
 * starts last owns the signal — so the one that gets it has to be able to shut
 * the other down. A hook here rather than a direct call because either module
 * may be compiled in without the other. */
static void (*dex_shutdown_drain_hook)(void) = NULL;

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
