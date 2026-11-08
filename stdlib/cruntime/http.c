
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <ctype.h>
#include <stdint.h>
#include <errno.h>
#include <sys/socket.h>
#include <sys/uio.h>
#include <sys/resource.h>
#ifdef __linux__
#include <sys/prctl.h>
int accept4(int fd, struct sockaddr* addr, socklen_t* addrlen, int flags);
#if defined(__has_include)
#if __has_include(<linux/io_uring.h>)
#define DEX_HTTP_HAVE_URING 1
#include <linux/io_uring.h>
#include <sys/mman.h>
#include <sys/syscall.h>
#endif
#endif
#endif
#include <netinet/in.h>
#include <arpa/inet.h>
#include <unistd.h>
#include <pthread.h>
#include <signal.h>
#include <time.h>
#include <curl/curl.h>
#ifdef DEX_HAS_LIBDEFLATE
#include <libdeflate.h>
#endif

// A route handler is a function value: code plus whatever state it carries, so a
// handler can be a method bound to the struct holding its dependencies. The
// environment is passed as a hidden first argument, as for every closure.
typedef Dex_HttpResponse (*dex_handler_fn)(void*, Dex_HttpRequest);

#define DEX_CALL_HANDLER(c, req) (((dex_handler_fn)(c)->fn)((c)->env, (req)))

typedef struct {
    const char* method;
    const char* pattern;          // original pattern string (e.g. "/posts/:id")
    int param_count;              // number of :param segments
    int param_indices[16];        // which segment index has a param (max 16 params)
    char param_names[16][64];     // param names without ':'
    int segment_count;            // total segments in pattern
    char segments[32][256];       // pre-split pattern segments
    DexClosure* handler;
} dex_route_entry;

/* Grown on demand rather than fixed. An entry is a little over 9KB — the
 * pre-split segments are most of it — so a static table generous enough for a
 * large service would cost tens of megabytes of BSS in every binary that links
 * this, including the ones serving six routes. Doubling from a small start
 * means a small service pays for what it registers and a large one is not
 * capped at all.
 *
 * Safe to move under realloc because the table is built in full before
 * dex_http_listen begins serving and is never written again: every dex_route
 * call happens during startup, and the worker processes listen forks already
 * inherit a finished table. */
static dex_route_entry* dex_routes = NULL;
static int dex_route_count = 0;

/* How many threads run handlers. 0 leaves the pool to size itself from the CPU
 * count, with a floor of four — which is the right hedge in general, because a
 * handler that blocks on a database or an outbound request would stall a
 * one-thread pool. It is the wrong number when handlers are CPU-bound and
 * serialised behind a lock on a one-core box, where four threads only add
 * context switches to work that cannot overlap. Only the program knows which
 * of those it is, so it is the program that says. */
static int dex_http_thread_count = 0;

static int dex_http_inline = 0;
static int dex_http_uring_requested = 0;

void dex_http_set_inline(void) {
    dex_http_inline = 1;
}

void dex_http_set_uring(void) {
    dex_http_inline = 1;
    dex_http_uring_requested = 1;
}

void dex_http_set_threads(int n) {
    dex_http_thread_count = n;
}

/* Limits on what one connection may cost us. Without them a listener on the
 * open internet is trivially taken down: a client that opens a socket and
 * sends one byte is never reaped, and one that announces a two-gigabyte body
 * is buffered toward it.
 *
 * The header cap is separate from the body cap because they bound different
 * things — a request line and headers that never terminate, versus a body
 * whose length was declared up front and is checked before a byte is read. */
#define DEX_HTTP_MAX_HEADER_BYTES 32768
#define DEX_HTTP_DEFAULT_MAX_BODY (8 * 1024 * 1024)
#define DEX_HTTP_DEFAULT_IDLE_SECS 30
#define DEX_HTTP_DEFAULT_MAX_CONNS 8192

static int dex_http_max_body  = DEX_HTTP_DEFAULT_MAX_BODY;
static int dex_http_idle_secs = DEX_HTTP_DEFAULT_IDLE_SECS;
static int dex_http_max_conns = DEX_HTTP_DEFAULT_MAX_CONNS;
static int dex_http_live_conns = 0;
static int dex_http_conn_cap = 0;
static int dex_http_shed_at = 0;

/* Where keep-alive shedding starts: fifteen sixteenths of the cap, but never
 * more than 4096 connections short of it, so a cap in the hundreds of
 * thousands does not start closing connections tens of thousands early. */
static int dex_http_shed_point(int cap) {
    int room = cap / 16;
    if (room > 4096) room = 4096;
    return cap - room;
}
static int dex_http_accepting = 1;

#define DEX_HTTP_MAX_LISTENERS 4
static int dex_http_extra_ports[DEX_HTTP_MAX_LISTENERS - 1];
static int dex_http_extra_port_count = 0;
static int dex_http_listen_fds[DEX_HTTP_MAX_LISTENERS];
static int dex_http_listen_count = 0;

/* 0 for any argument keeps that default: a program raising the body limit for
 * uploads should not have to restate the idle timeout to do it. */
void dex_http_set_limits(int max_body_bytes, int idle_timeout_secs, int max_conns) {
    if (max_body_bytes > 0)    dex_http_max_body = max_body_bytes;
    if (idle_timeout_secs > 0) dex_http_idle_secs = idle_timeout_secs;
    if (max_conns > 0)         dex_http_max_conns = max_conns;
}

/* Answers when no route matched. NULL leaves the built-in 404 in place, which
 * is the right default for a program that has not said otherwise — but a
 * program that has an error shape of its own needs the body and content type to
 * be its own, and a wildcard route cannot express "no pattern matched" because
 * route matching is per-segment. */
static DexClosure* dex_route_fallback = NULL;
static int dex_route_capacity = 0;

#define DEX_ROUTES_INITIAL 16

void dex_route(const char* method, const char* path, DexClosure* handler) {
    if (dex_route_count == dex_route_capacity) {
        int grown = dex_route_capacity ? dex_route_capacity * 2 : DEX_ROUTES_INITIAL;
        dex_route_entry* table =
            (dex_route_entry*)dex_xrealloc(dex_routes, (size_t)grown * sizeof(dex_route_entry));
        /* Dropping a route silently is worse than the failure itself: the route
         * simply answers 404 with nothing anywhere to say why. */
        if (!table) {
            fprintf(stderr,
                "[http] out of memory growing the route table to %d entries: "
                "dropped %s %s.\n",
                grown, method, path);
            return;
        }
        dex_routes = table;
        dex_route_capacity = grown;
    }
    dex_route_entry* r = &dex_routes[dex_route_count];
    r->method = method;
    r->pattern = path;
    // The table outlives the expression that built the handler, so it takes its
    // own reference to it.
    dex_retain(handler);
    r->handler = handler;
    r->param_count = 0;
    r->segment_count = 0;

    // Parse pattern into segments, identify :param segments
    const char* p = path;
    if (*p == '/') p++; // skip leading slash
    while (*p && r->segment_count < 32) {
        const char* end = strchr(p, '/');
        int len = end ? (int)(end - p) : (int)strlen(p);
        if (len >= 256) len = 255;
        memcpy(r->segments[r->segment_count], p, len);
        r->segments[r->segment_count][len] = '\0';
        if (p[0] == ':' && len > 1 && r->param_count < 16) {
            r->param_indices[r->param_count] = r->segment_count;
            int name_len = len - 1;
            if (name_len >= 64) name_len = 63;
            memcpy(r->param_names[r->param_count], p + 1, name_len);
            r->param_names[r->param_count][name_len] = '\0';
            r->param_count++;
        }
        r->segment_count++;
        if (!end) break;
        p = end + 1;
    }

    dex_route_count++;
}

static void dex_send_response(int fd, const char* status, const char* body, const char* content_type, int keep_alive) {
    int body_len = (int)strlen(body);
    char header[512];
    int hlen = snprintf(header, sizeof(header),
        "HTTP/1.1 %s\r\n"
        "Content-Type: %s\r\n"
        "Content-Length: %d\r\n"
        "Connection: %s\r\n"
        "\r\n",
        status, content_type, body_len, keep_alive ? "keep-alive" : "close");
    // Use writev to send header+body in one syscall
    struct iovec iov[2];
    iov[0].iov_base = header;
    iov[0].iov_len = hlen;
    iov[1].iov_base = (void*)body;
    iov[1].iov_len = body_len;
    writev(fd, iov, 2);
}

static const char* dex_http_status_text(int code) {
    switch (code) {
    case 200: return "200 OK";
    case 201: return "201 Created";
    case 204: return "204 No Content";
    case 301: return "301 Moved Permanently";
    case 302: return "302 Found";
    case 304: return "304 Not Modified";
    case 400: return "400 Bad Request";
    case 401: return "401 Unauthorized";
    case 403: return "403 Forbidden";
    case 404: return "404 Not Found";
    case 405: return "405 Method Not Allowed";
    case 409: return "409 Conflict";
    case 422: return "422 Unprocessable Entity";
    case 429: return "429 Too Many Requests";
    case 500: return "500 Internal Server Error";
    case 502: return "502 Bad Gateway";
    case 503: return "503 Service Unavailable";
    default: {
        static __thread char status_buf[32];
        snprintf(status_buf, sizeof(status_buf), "%d", code);
        return status_buf;
    }
    }
}

/* ── HTTP connection state machine (event-loop driven) ────────────── */

typedef enum {
    HTTP_READING_HEADERS,
    HTTP_READING_BODY,
    HTTP_DISPATCHED,
    HTTP_WRITING,
    HTTP_DONE
} DexHttpState;

typedef struct DexHttpConn {
    int           fd;
    DexHttpState  state;

    /* Read buffer (request accumulation) */
    char*         read_buf;
    int           read_len;
    int           read_cap;

    /* Parsed request fields (populated after headers complete) */
    char          method[16];
    char*         path;
    char*         query;
    int           content_length;
    int           headers_end;     /* offset of body start in read_buf */
    int           header_start;    /* offset of the first header line */
    int           keep_alive;

    /* Write buffer (response) */
    char*         write_buf;
    size_t        write_cap;
    int           write_len;
    int           write_pos;       /* how much has been flushed */

    /* When this connection last made progress, for the idle sweep. */
    time_t        last_active;

    /* Linked list for completed queue */
    struct DexHttpConn* next;

    int           direct;
    unsigned char ring;        /* io_uring: the ring that owns this connection */
    unsigned char plain_recv;  /* io_uring: the read in flight uses read_buf */
    unsigned char accept_gzip; /* the request's Accept-Encoding allows gzip */
    unsigned char later;       /* io_uring: the handler asked to run again (http.later) */
    int           relay_slot;
    struct DexHttpConn* later_next;
    uint64_t      later_since; /* io_uring: ms when http.later first parked it, 0 if never */
} DexHttpConn;

/* Global event-loop state for HTTP server */
static DexEventLoop*  dex_http_loop  = NULL;
static DexThreadPool* dex_http_pool  = NULL;
static DexNotifyPipe  dex_http_notify;

/* Completed connections queue (worker → event loop) */
static DexHttpConn*   dex_http_completed_head = NULL;
static pthread_mutex_t dex_http_completed_mutex = PTHREAD_MUTEX_INITIALIZER;

/* Connection table indexed by fd */
#define DEX_HTTP_MAX_FD 65536
static DexHttpConn*   dex_http_conn_table[DEX_HTTP_MAX_FD];

static DexHttpConn* dex_http_conn_new(int fd) {
    DexHttpConn* conn = (DexHttpConn*)dex_xcalloc(1, sizeof(DexHttpConn));
    if (!conn) return NULL;
    conn->fd = fd;
    conn->state = HTTP_READING_HEADERS;
    conn->read_cap = 2048;
    conn->read_buf = (char*)dex_xmalloc(conn->read_cap);
    if (!conn->read_buf) { free(conn); return NULL; }
    conn->read_len = 0;
    conn->content_length = -1;
    conn->headers_end = -1;
    conn->header_start = -1;
    conn->keep_alive = 1;
    conn->last_active = time(NULL);
    if (fd >= 0 && fd < DEX_HTTP_MAX_FD) {
        dex_http_conn_table[fd] = conn;
    }
    __atomic_add_fetch(&dex_http_live_conns, 1, __ATOMIC_RELAXED);
    return conn;
}

#define DEX_HTTP_POOL_BUF_BYTES 16384
#define DEX_HTTP_POOL_MAX 256
static __thread char* dex_http_buf_pool[DEX_HTTP_POOL_MAX];
static __thread int dex_http_buf_pool_len = 0;

static char* dex_http_buf_take(size_t needed, size_t* cap) {
    if (dex_http_inline && needed <= DEX_HTTP_POOL_BUF_BYTES) {
        *cap = DEX_HTTP_POOL_BUF_BYTES;
        if (dex_http_buf_pool_len > 0) return dex_http_buf_pool[--dex_http_buf_pool_len];
        return (char*)dex_xmalloc(DEX_HTTP_POOL_BUF_BYTES);
    }
    *cap = needed;
    return (char*)dex_xmalloc(needed);
}

static void dex_http_release_write_buf(DexHttpConn* conn) {
    char* b = conn->write_buf;
    conn->write_buf = NULL;
    if (!b) return;
    if (dex_http_inline && conn->write_cap == DEX_HTTP_POOL_BUF_BYTES &&
        dex_http_buf_pool_len < DEX_HTTP_POOL_MAX) {
        dex_http_buf_pool[dex_http_buf_pool_len++] = b;
        return;
    }
    free(b);
}

static void dex_lane_forget(DexHttpConn* conn);

static void dex_http_conn_free(DexHttpConn* conn) {
    if (!conn) return;
    if (conn->relay_slot) dex_lane_forget(conn);
    if (conn->fd >= 0 && conn->fd < DEX_HTTP_MAX_FD && dex_http_conn_table[conn->fd] == conn) {
        dex_http_conn_table[conn->fd] = NULL;
    }
    __atomic_sub_fetch(&dex_http_live_conns, 1, __ATOMIC_RELAXED);
    free(conn->read_buf);
    dex_http_release_write_buf(conn);
    free(conn->path);
    free(conn);
}

static void dex_http_conn_reset(DexHttpConn* conn) {
    /* Reset for keep-alive: reuse fd and buffers */
    conn->state = HTTP_READING_HEADERS;
    conn->read_len = 0;
    conn->content_length = -1;
    conn->headers_end = -1;
    conn->keep_alive = 1;
    conn->header_start = -1;
    conn->last_active = time(NULL);
    conn->method[0] = '\0';
    free(conn->path);
    conn->path = NULL;
    conn->query = NULL;
    dex_http_release_write_buf(conn);
    conn->write_len = 0;
    conn->write_pos = 0;
}

/* Answer a malformed or oversized request and drop the connection.
 *
 * One best-effort write: the socket is non-blocking, so a short write is
 * possible, but a client that sent us something we are refusing does not get
 * a retry loop. Closing without an answer at all would look like a network
 * fault rather than a rejection. */
static void dex_http_fail_and_close(DexHttpConn* conn, const char* status, const char* body) {
    dex_send_response(conn->fd, status, body, "application/json", 0);
    dex_ev_del(dex_http_loop, conn->fd);
    close(conn->fd);
    dex_http_conn_free(conn);
}

/* http.gzip: bodies of at least this many bytes are compressed for a client
 * that accepts gzip. 0 is off, and off is all there is without libdeflate. */
static int dex_http_gzip_min = 0;

void dex_http_set_gzip(int min_bytes) {
#ifdef DEX_HAS_LIBDEFLATE
    dex_http_gzip_min = min_bytes > 0 ? min_bytes : 0;
#else
    (void)min_bytes;
#endif
}

/* Whether an Accept-Encoding value lists coding with a non-zero q. */
static int dex_http_coding_accepted(const char* v, int vlen, const char* coding) {
    size_t clen = strlen(coding);
    const char* end = v + vlen;
    const char* p = v;
    while (p < end) {
        const char* comma = memchr(p, ',', (size_t)(end - p));
        const char* item_end = comma ? comma : end;
        while (p < item_end && (*p == ' ' || *p == '\t')) p++;
        const char* name_end = p;
        while (name_end < item_end && *name_end != ';' && *name_end != ' ' && *name_end != '\t') name_end++;
        if ((size_t)(name_end - p) == clen && strncasecmp(p, coding, clen) == 0) {
            const char* q = name_end;
            while (q < item_end && *q != ';') q++;
            if (q >= item_end) return 1;
            q++;
            while (q < item_end && (*q == ' ' || *q == '\t')) q++;
            if (item_end - q >= 2 && (q[0] == 'q' || q[0] == 'Q') && q[1] == '=') {
                q += 2;
                /* q=0, q=0.0, q=0.000: refused. Anything else: accepted. */
                if (q < item_end && *q == '0') {
                    q++;
                    if (q < item_end && *q == '.') q++;
                    while (q < item_end && *q == '0') q++;
                    if (q >= item_end || *q == ' ' || *q == '\t' || *q == ';') return 0;
                }
            }
            return 1;
        }
        if (!comma) break;
        p = comma + 1;
    }
    return 0;
}

#ifdef DEX_HAS_LIBDEFLATE
/* One compressor and output buffer per thread: compressing never allocates
 * once the buffer has grown to the largest body seen. Level 1 is the fastest
 * and, on JSON, within a few percent of the default level's size. */
static __thread struct libdeflate_compressor* dex_http_gz = NULL;
static __thread char* dex_http_gz_buf = NULL;
static __thread size_t dex_http_gz_cap = 0;

/* Compress body into the thread's buffer. Returns the compressed length, or
 * 0 when compressing failed or would not make it smaller. */
static size_t dex_http_gzip(const char* body, size_t len) {
    if (!dex_http_gz) {
        dex_http_gz = libdeflate_alloc_compressor(1);
        if (!dex_http_gz) return 0;
    }
    size_t bound = libdeflate_gzip_compress_bound(dex_http_gz, len);
    if (bound > dex_http_gz_cap) {
        char* nb = (char*)realloc(dex_http_gz_buf, bound);
        if (!nb) return 0;
        dex_http_gz_buf = nb;
        dex_http_gz_cap = bound;
    }
    size_t n = libdeflate_gzip_compress(dex_http_gz, body, len, dex_http_gz_buf, dex_http_gz_cap);
    return (n > 0 && n < len) ? n : 0;
}
#endif

/* Find one header's value inside the header block. Scoped to header lines
 * rather than searched for across the whole request, which is what strstr over
 * read_buf did: any header *value* containing "Content-Length:" was picked up
 * as the request's own, and so was a path containing "HTTP/1.1". Behind a
 * proxy that difference is request smuggling.
 *
 * `out_dup` is set when the name appears more than once, which is a malformed
 * request rather than something to pick a winner from. */
static const char* dex_http_find_header(const char* block, int block_len,
                                        const char* name, int* out_len, int* out_dup) {
    int name_len = (int)strlen(name);
    const char* found = NULL;
    int found_len = 0;
    if (out_dup) *out_dup = 0;

    /* Past the request line: a header can only start after the first CRLF. */
    const char* p = block;
    const char* limit = block + block_len;
    const char* eol = (const char*)memchr(p, '\n', (size_t)block_len);
    if (!eol) return NULL;
    p = eol + 1;

    while (p < limit) {
        const char* line_end = (const char*)memchr(p, '\n', (size_t)(limit - p));
        const char* end = line_end ? line_end : limit;
        int line_len = (int)(end - p);
        if (line_len > 0 && p[line_len - 1] == '\r') line_len--;
        if (line_len <= 0) break;

        if (line_len > name_len && p[name_len] == ':' &&
            strncasecmp(p, name, (size_t)name_len) == 0) {
            const char* vp = p + name_len + 1;
            int vlen = line_len - name_len - 1;
            while (vlen > 0 && (*vp == ' ' || *vp == '\t')) { vp++; vlen--; }
            while (vlen > 0 && (vp[vlen - 1] == ' ' || vp[vlen - 1] == '\t')) vlen--;
            if (found && out_dup) *out_dup = 1;
            found = vp;
            found_len = vlen;
        }

        if (!line_end) break;
        p = line_end + 1;
    }

    if (found && out_len) *out_len = found_len;
    return found;
}

/* Try to parse headers from the read buffer. Returns 1 if complete. */
static int dex_http_try_parse_headers(DexHttpConn* conn) {
    /* Look for \r\n\r\n */
    char* end = NULL;
    for (int i = 0; i <= conn->read_len - 4; i++) {
        if (conn->read_buf[i] == '\r' && conn->read_buf[i+1] == '\n' &&
            conn->read_buf[i+2] == '\r' && conn->read_buf[i+3] == '\n') {
            end = conn->read_buf + i;
            break;
        }
    }
    if (!end) return 0;

    conn->headers_end = (int)(end - conn->read_buf) + 4;

    /* Null-terminate for parsing (temporary) */
    char save = conn->read_buf[conn->headers_end];
    conn->read_buf[conn->headers_end] = '\0';

    {
        const char* t = conn->read_buf;
        while (*t && isspace((unsigned char)*t)) t++;
        int m = 0;
        while (m < 15 && t[m] && !isspace((unsigned char)t[m])) { conn->method[m] = t[m]; m++; }
        if (m > 0) conn->method[m] = '\0';
        while (*t && !isspace((unsigned char)*t)) t++;
        while (*t && isspace((unsigned char)*t)) t++;
        size_t n = 0;
        while (t[n] && !isspace((unsigned char)t[n]) && n < 2047) n++;
        free(conn->path);
        conn->path = (char*)dex_xmalloc(n + 1);
        if (!conn->path) { conn->read_buf[conn->headers_end] = save; return -1; }
        memcpy(conn->path, t, n);
        conn->path[n] = '\0';
        conn->query = NULL;
        char* qmark = strchr(conn->path, '?');
        if (qmark) {
            *qmark = '\0';
            conn->query = qmark + 1;
        }
    }

    /* Where the header lines start: just past the request line. */
    {
        const char* eol = strstr(conn->read_buf, "\r\n");
        conn->header_start = eol ? (int)(eol - conn->read_buf) + 2 : -1;
    }

    /* Keep-alive comes from the request line's version, read where the version
     * actually is, then from a Connection header scoped to the headers. */
    {
        const char* rl_end = (const char*)memchr(conn->read_buf, '\n', (size_t)conn->headers_end);
        int rl_len = rl_end ? (int)(rl_end - conn->read_buf) : conn->headers_end;
        conn->keep_alive = 0;
        for (int i = 0; i + 8 <= rl_len; i++) {
            if (memcmp(conn->read_buf + i, "HTTP/1.1", 8) == 0) { conn->keep_alive = 1; break; }
        }
    }
    {
        int vlen = 0;
        const char* v = dex_http_find_header(conn->read_buf, conn->headers_end,
                                             "Connection", &vlen, NULL);
        if (v && vlen == 5 && strncasecmp(v, "close", 5) == 0) conn->keep_alive = 0;
    }
    conn->accept_gzip = 0;
    if (dex_http_gzip_min > 0) {
        int vlen = 0;
        const char* v = dex_http_find_header(conn->read_buf, conn->headers_end,
                                             "Accept-Encoding", &vlen, NULL);
        conn->accept_gzip = v && dex_http_coding_accepted(v, vlen, "gzip");
    }

    /* A request that frames its body with chunked encoding is refused rather
     * than guessed at. Treating it as Content-Length: 0 left the chunk data in
     * the buffer to be read as the next pipelined request, which is the other
     * half of request smuggling. */
    {
        int vlen = 0;
        if (dex_http_find_header(conn->read_buf, conn->headers_end,
                                 "Transfer-Encoding", &vlen, NULL)) {
            conn->read_buf[conn->headers_end] = save;
            return -1;
        }
    }

    {
        int vlen = 0, dup = 0;
        const char* v = dex_http_find_header(conn->read_buf, conn->headers_end,
                                             "Content-Length", &vlen, &dup);
        if (!v) {
            conn->content_length = 0;
        } else if (dup || vlen <= 0 || vlen > 9) {
            /* Two of them disagree, or it is too long to be a length we accept.
             * Nine digits caps it below INT_MAX with no overflow to reason
             * about; the body limit below is what actually decides. */
            conn->read_buf[conn->headers_end] = save;
            return -1;
        } else {
            int n = 0;
            for (int i = 0; i < vlen; i++) {
                if (v[i] < '0' || v[i] > '9') {
                    conn->read_buf[conn->headers_end] = save;
                    return -1;
                }
                n = n * 10 + (v[i] - '0');
            }
            conn->content_length = n;
        }
    }

    conn->read_buf[conn->headers_end] = save;

    if (conn->content_length > dex_http_max_body) return -2;
    return 1;
}

/* Fill a map with this request's headers, keys lower-cased.
 *
 * HTTP header names are case-insensitive, and a caller should not have to guess
 * whether the client sent "Authorization" or "authorization". Lower-casing on
 * the way in means there is one spelling to look up.
 *
 * Values are trimmed of leading spaces and of the trailing CR. A header line
 * with no colon is skipped rather than guessed at. */
static void dex_http_parse_header_lines(const char* p, const char* limit, DexMap_str_str* headers) {
    while (p < limit) {
        const char* eol = p;
        while (eol < limit && *eol != '\n') eol++;
        int line_len = (int)(eol - p);
        if (line_len > 0 && p[line_len - 1] == '\r') line_len--;
        if (line_len <= 0) break;   /* blank line ends the header block */

        const char* colon = (const char*)memchr(p, ':', (size_t)line_len);
        if (colon) {
            int name_len = (int)(colon - p);
            const char* vp = colon + 1;
            int value_len = line_len - name_len - 1;
            while (value_len > 0 && (*vp == ' ' || *vp == '\t')) { vp++; value_len--; }

            if (name_len > 0 && name_len < 256) {
                char name[256];
                for (int i = 0; i < name_len; i++) {
                    char c = p[i];
                    name[i] = (c >= 'A' && c <= 'Z') ? (char)(c - 'A' + 'a') : c;
                }
                name[name_len] = '\0';

                DexString* key = dex_string_new(name, (size_t)name_len);
                DexString* val = dex_string_new(vp, (size_t)(value_len > 0 ? value_len : 0));
                dex_map_str_str_set_raw(headers, key, val);
                dex_release(key);
                dex_release(val);
            }
        }
        p = (eol < limit) ? eol + 1 : limit;
    }
}

/* The header lines of one request, copied out of the connection's read buffer
 * so the map they fill owns them outright. The read buffer is reset for the
 * next request on the connection as soon as this response is queued, and a
 * handler may keep req.headers for longer than that. */
typedef struct {
    DexMapLazy base;
    size_t len;
    char bytes[];
} DexHttpLazyHeaders;

static void dex_http_headers_fill(void* map, DexMapLazy* self) {
    DexHttpLazyHeaders* h = (DexHttpLazyHeaders*)self;
    dex_http_parse_header_lines(h->bytes, h->bytes + h->len, (DexMap_str_str*)map);
}

/* Give req.headers this request's headers. They are parsed the first time the
 * handler (or anything it hands the map to) uses the map, not here, so a
 * handler that never reads a header never pays for the strings. The entries,
 * their order and every lookup are the same as parsing them up front. */
static void dex_http_fill_headers(DexHttpConn* conn, DexMap_str_str* headers) {
    if (conn->header_start < 0 || conn->headers_end <= conn->header_start) return;
    size_t len = (size_t)(conn->headers_end - conn->header_start);
    DexHttpLazyHeaders* h = (DexHttpLazyHeaders*)dex_xmalloc(sizeof(DexHttpLazyHeaders) + len);
    h->base.fill = dex_http_headers_fill;
    h->len = len;
    memcpy(h->bytes, conn->read_buf + conn->header_start, len);
    headers->lazy = &h->base;
}

/* Build the write buffer for a response */
/* `extra` is a newline-separated block of "Key: Value" pairs, as built by
 * http.header(). Each line is re-emitted CRLF-terminated, so a key may appear
 * more than once — which is exactly what Set-Cookie needs and what a map could
 * not have expressed. NULL or empty means none. */
static void dex_http_build_response_full(DexHttpConn* conn, const char* status,
                                     const char* body, int body_len, const char* content_type,
                                     const char* extra) {
    size_t extra_len = (extra && *extra) ? strlen(extra) : 0;
    size_t status_len = strlen(status);
    size_t ct_len = strcspn(content_type, "\r\n");
    if (body_len < 0) body_len = 0;

    /* A client that accepts gzip gets a large enough success body compressed. */
    const char* coding = "";
    size_t coding_len = 0;
#ifdef DEX_HAS_LIBDEFLATE
    if (dex_http_gzip_min > 0 && conn->accept_gzip && body_len >= dex_http_gzip_min && status[0] == '2') {
        size_t zn = dex_http_gzip(body, (size_t)body_len);
        if (zn > 0) {
            body = dex_http_gz_buf;
            body_len = (int)zn;
            coding = "Content-Encoding: gzip\r\nVary: Accept-Encoding\r\n";
            coding_len = strlen(coding);
        }
    }
#endif

    if (conn->keep_alive && dex_http_shed_at > 0 &&
        __atomic_load_n(&dex_http_live_conns, __ATOMIC_RELAXED) >= dex_http_shed_at) {
        conn->keep_alive = 0;
    }
    const char* conn_line = conn->keep_alive ? "\r\nConnection: keep-alive\r\n" : "\r\nConnection: close\r\n";
    size_t conn_line_len = strlen(conn_line);

    char digits[16];
    int ndigits = 0;
    {
        unsigned v = (unsigned)body_len;
        char rev[16];
        do { rev[ndigits++] = (char)('0' + v % 10); v /= 10; } while (v);
        for (int i = 0; i < ndigits; i++) digits[i] = rev[ndigits - 1 - i];
    }

    size_t needed = 9 + status_len + 16 + ct_len + 18 + (size_t)ndigits + conn_line_len
                  + coding_len + extra_len * 2 + 2 + (size_t)body_len + 1;
    dex_http_release_write_buf(conn);
    conn->write_buf = dex_http_buf_take(needed, &conn->write_cap);
    if (!conn->write_buf) { conn->write_len = 0; conn->write_pos = 0; return; }

    char* w = conn->write_buf;
    memcpy(w, "HTTP/1.1 ", 9); w += 9;
    memcpy(w, status, status_len); w += status_len;
    memcpy(w, "\r\nContent-Type: ", 16); w += 16;
    memcpy(w, content_type, ct_len); w += ct_len;
    memcpy(w, "\r\nContent-Length: ", 18); w += 18;
    memcpy(w, digits, (size_t)ndigits); w += ndigits;
    memcpy(w, conn_line, conn_line_len); w += conn_line_len;
    if (coding_len) { memcpy(w, coding, coding_len); w += coding_len; }
    size_t hlen = (size_t)(w - conn->write_buf);

    if (extra_len > 0) {
        const char* p = extra;
        while (*p) {
            const char* eol = strchr(p, '\n');
            size_t len = eol ? (size_t)(eol - p) : strlen(p);
            if (len > 0 && p[len - 1] == '\r') len--;
            /* A blank or colon-less line is not a header; skip it rather than
             * emit something that would corrupt the response framing. */
            if (len > 0 && memchr(p, ':', len) && hlen + len + 4 < needed) {
                memcpy(conn->write_buf + hlen, p, len);
                hlen += len;
                conn->write_buf[hlen++] = '\r';
                conn->write_buf[hlen++] = '\n';
            }
            if (!eol) break;
            p = eol + 1;
        }
    }

    conn->write_buf[hlen++] = '\r';
    conn->write_buf[hlen++] = '\n';
    memcpy(conn->write_buf + hlen, body, (size_t)body_len);
    conn->write_len = (int)hlen + body_len;
    conn->write_pos = 0;
}

static inline void dex_http_build_response_len(DexHttpConn* conn, const char* status,
                                     const char* body, int body_len, const char* content_type) {
    dex_http_build_response_full(conn, status, body, body_len, content_type, NULL);
}

static inline void dex_http_build_response(DexHttpConn* conn, const char* status,
                                     const char* body, const char* content_type) {
    dex_http_build_response_len(conn, status, body, (int)strlen(body), content_type);
}

/* Build the wire response from what a handler returned, taking over the
 * references it handed back. Three callers: the two route-matching paths and
 * the not-found fallback. */
static inline void dex_http_apply_response(DexHttpConn* conn, Dex_HttpResponse resp) {
    if (conn->later) {
        /* The handler runs again on the next pass; what it returned now is not the answer. */
        if (resp.contentType) dex_release(resp.contentType);
        if (resp.headers) dex_release(resp.headers);
        dex_release(resp.body);
        return;
    }
    const char* status = dex_http_status_text(resp.statusCode);
    const char* ct = "application/json";
    if (resp.contentType && resp.contentType->data[0] != '\0') {
        ct = resp.contentType->data;
    }
    const char* extra = resp.headers ? resp.headers->data : NULL;
    dex_http_build_response_full(conn, status, resp.body->data, (int)resp.body->len, ct, extra);
    if (resp.contentType) dex_release(resp.contentType);
    if (resp.headers) dex_release(resp.headers);
    dex_release(resp.body);
}

/* Register the fallback. The handler arrives owned — genRouteHandler mints the
 * reference for this call, the same convention ws.handleMessage follows — so
 * retaining it here would be one reference more than there is an owner for, and
 * the closure a replaced registration dropped would never reach zero.
 * Replacing releases the reference the previous registration held, so
 * registering twice does not strand the first and whatever it captured. */
void dex_route_not_found(DexClosure* handler) {
    DexClosure* prev = dex_route_fallback;
    dex_route_fallback = handler;
    if (prev) dex_release(prev);
}

/* Worker function: run handler, build response, enqueue for event loop */
static __thread DexHttpConn* dex_http_running = NULL;

static void dex_http_run_handler_body(DexHttpConn* conn);

static void dex_http_run_handler(DexHttpConn* conn) {
    dex_http_running = conn;
    dex_http_run_handler_body(conn);
    dex_http_running = NULL;
}

static void dex_http_run_handler_body(DexHttpConn* conn) {

    /* Extract body from read buffer */
    char* body_data = "";
    int body_len = 0;
    if (conn->content_length > 0 && conn->headers_end >= 0) {
        body_data = conn->read_buf + conn->headers_end;
        body_len = conn->read_len - conn->headers_end;
        if (body_len > conn->content_length) body_len = conn->content_length;
        body_data[body_len] = '\0';
    }

    /* Build HttpRequest struct — use known lengths to avoid strlen */
    const char* cpath = conn->path ? conn->path : "";
    size_t cpath_len = strlen(cpath);
    Dex_HttpRequest req;
    req.method = dex_string_new(conn->method, strlen(conn->method));
    req.path = dex_string_new(cpath, cpath_len);
    req.body = (body_len > 0)
        ? dex_string_new(body_data, (size_t)body_len)
        : dex_string_new("", 0);
    req.query = conn->query ? dex_string_new(conn->query, strlen(conn->query)) : dex_string_new("", 0);
    req.params = dex_map_str_str_new();
    req.headers = dex_map_str_str_new();
    dex_http_fill_headers(conn, req.headers);

    /* Split incoming path into segments for matching */
    /* Truncated to the buffer exactly as strncpy did, without strncpy
     * zero-filling all 2 KB of it on every request. */
    char path_copy[2048];
    size_t path_copy_len = cpath_len < sizeof(path_copy) - 1 ? cpath_len : sizeof(path_copy) - 1;
    memcpy(path_copy, cpath, path_copy_len);
    path_copy[path_copy_len] = '\0';

    char* req_segments[32];
    int req_segment_count = 0;
    {
        char* p = path_copy;
        if (*p == '/') p++;
        while (*p && req_segment_count < 32) {
            req_segments[req_segment_count] = p;
            char* slash = strchr(p, '/');
            if (slash) {
                *slash = '\0';
                p = slash + 1;
            } else {
                p = p + strlen(p);
            }
            req_segment_count++;
        }
    }

    /* Match route */
    int matched = 0;
    for (int i = 0; i < dex_route_count; i++) {
        if (strcmp(conn->method, dex_routes[i].method) != 0) continue;

        dex_route_entry* r = &dex_routes[i];

        /* Fast path: no params, use exact string match */
        if (r->param_count == 0) {
            if (strcmp(cpath, r->pattern) == 0) {
                dex_http_apply_response(conn, DEX_CALL_HANDLER(r->handler, req));
                matched = 1;
                break;
            }
            continue;
        }

        /* Parameterized route: segment-by-segment matching */
        if (r->segment_count != req_segment_count) continue;

        int seg_match = 1;
        for (int s = 0; s < r->segment_count; s++) {
            if (r->segments[s][0] == ':') {
                /* Param segment — any non-empty value matches */
                if (req_segments[s][0] == '\0') { seg_match = 0; break; }
            } else {
                if (strcmp(r->segments[s], req_segments[s]) != 0) { seg_match = 0; break; }
            }
        }

        if (seg_match) {
            /* Populate params map with extracted values */
            for (int pi = 0; pi < r->param_count; pi++) {
                int seg_idx = r->param_indices[pi];
                DexString* key = dex_string_from_lit(r->param_names[pi]);
                DexString* val = dex_string_from_lit(req_segments[seg_idx]);
                dex_map_str_str_set(req.params, key, val);
                dex_release(key);
                dex_release(val);
            }

            dex_http_apply_response(conn, DEX_CALL_HANDLER(r->handler, req));
            matched = 1;
            break;
        }
    }

    /* No route matched. A registered fallback answers instead of the built-in
     * 404, and reading the request is the whole point of it — so this runs
     * before the request strings are released, not beside the default below. */
    if (!matched && dex_route_fallback) {
        dex_http_apply_response(conn, DEX_CALL_HANDLER(dex_route_fallback, req));
        matched = 1;
    }

    /* Release request strings and params map */
    dex_release(req.method);
    dex_release(req.path);
    dex_release(req.body);
    dex_release(req.query);
    dex_release(req.params);
    dex_release(req.headers);

    if (!matched) {
        dex_http_build_response(conn, "404 Not Found",
            "{\"error\": \"Not Found\"}", "application/json");
    }

    /* A request parked by http.later is still being read, as far as anyone
     * looking at its state can tell: it has no response yet. */
    if (conn->later) return;

    /* Atomic because on a pool this runs on a worker while the event loop's
     * idle sweep reads every connection's state, this one included. */
    __atomic_store_n(&conn->state, HTTP_WRITING, __ATOMIC_RELAXED);
}

/* http.afterBatch: a hook run after a batch of handlers and before any of
 * their responses is sent, so handlers can share one transaction and have it
 * committed before anyone hears it succeeded. On io_uring a batch is every
 * request one pass of the loop served; on epoll and the thread pool it is a
 * single request, which keeps the guarantee though not the batching. The hook
 * returns false when the batch failed, and every request in it is then
 * answered 500 rather than with what its handler said. */
typedef _Bool (*dex_after_batch_fn)(void* env);
static DexClosure* dex_http_after_batch = NULL;

/* The hook arrives owned, the convention ws.handleMessage follows, and
 * replacing it releases the previous one. */
void dex_http_set_after_batch(DexClosure* hook) {
    DexClosure* prev = dex_http_after_batch;
    dex_http_after_batch = hook;
    if (prev) dex_release(prev);
}

static _Bool dex_http_call_after_batch(void) {
    DexClosure* h = dex_http_after_batch;
    if (!h) return 1;
    return ((dex_after_batch_fn)h->fn)(h->env);
}

/* What a request in a failed batch is told. Its own response, including any
 * extra headers such as Set-Cookie, is discarded. */
static void dex_http_answer_batch_failed(DexHttpConn* conn) {
    dex_http_build_response(conn, "500 Internal Server Error",
                            "{\"error\":\"internal server error\"}", "application/json");
}

/* http.hold and http.reply: a handler takes a ticket for its request, and
 * the batch hook answers it, so one query can serve every request of a batch.
 * A ticket names its batch as well as its request, and is honoured only while
 * that batch's hook runs, so a stale one is ignored rather than answering
 * whichever request now holds its slot. A request the hook does not answer
 * gets what its handler returned. */
#define DEX_HTTP_HOLD_MAX 0xFFFF
static int dex_http_batch_window_ms = 0;
static unsigned dex_http_gather_n = 0;
static unsigned dex_http_gather_us = 0;

void dex_http_set_gather(int count, int micros) {
    if (count < 0) count = 0;
    if (count > 4096) count = 4096;
    if (micros < 0) micros = 0;
    if (micros > 100000) micros = 100000;
    dex_http_gather_n = (count > 1 && micros > 0) ? (unsigned)count : 0;
    dex_http_gather_us = (unsigned)micros;
}

void dex_http_set_batch_window(int ms) {
    if (ms < 0) ms = 0;
    if (ms > 1000) ms = 1000;
    dex_http_batch_window_ms = ms;
}
static __thread DexHttpConn** dex_http_held = NULL;
static __thread int dex_http_held_len = 0;
static __thread int dex_http_held_cap = 0;
static unsigned dex_http_hold_batches = 0;
static __thread unsigned dex_http_hold_batch = 0;
static __thread _Bool dex_http_in_hook = 0;

int dex_http_hold(void) {
    DexHttpConn* c = dex_http_running;
    if (!c || !dex_http_after_batch || dex_http_held_len >= DEX_HTTP_HOLD_MAX) return -1;
    if (dex_http_held_len == dex_http_held_cap) {
        int cap = dex_http_held_cap ? dex_http_held_cap * 2 : 256;
        DexHttpConn** grown = (DexHttpConn**)dex_xrealloc(dex_http_held, (size_t)cap * sizeof(DexHttpConn*));
        if (!grown) return -1;
        dex_http_held = grown;
        dex_http_held_cap = cap;
    }
    if (!dex_http_hold_batch) {
        dex_http_hold_batch = __atomic_add_fetch(&dex_http_hold_batches, 1, __ATOMIC_RELAXED) % 0x7FFF + 1;
    }
    dex_http_running = NULL;
    int slot = dex_http_held_len++;
    dex_http_held[slot] = c;
    return (int)((dex_http_hold_batch << 16) | (unsigned)slot);
}

static _Bool dex_lane_store_reply(int ticket, int status, DexString* body, const char* content_type);

void dex_http_reply(int ticket, int status, DexString* body, const char* content_type) {
    if (dex_lane_store_reply(ticket, status, body, content_type)) return;
    if (!dex_http_in_hook || ticket < 0 || !dex_http_hold_batch) return;
    if (((unsigned)ticket >> 16) != dex_http_hold_batch) return;
    int slot = ticket & DEX_HTTP_HOLD_MAX;
    if (slot >= dex_http_held_len || !dex_http_held[slot]) return;
    DexHttpConn* c = dex_http_held[slot];
    dex_http_held[slot] = NULL;
    const char* ct = (content_type && *content_type) ? content_type : "application/json";
    dex_http_build_response_full(c, dex_http_status_text(status), body->data, (int)body->len, ct, NULL);
}

static _Bool dex_http_run_batch_hook(void) {
    dex_http_in_hook = 1;
    _Bool ok = dex_http_call_after_batch();
    dex_http_in_hook = 0;
    dex_http_held_len = 0;
    dex_http_hold_batch = 0;
    return ok;
}

static int dex_http_worker_index;

#define DEX_LANE_MAX 16
#define DEX_LANE_BATCH_MAX 4096
#define DEX_LANE_FRAME_MAX (64u << 20)
#define DEX_LANE_SLOT_BITS 20

typedef struct {
    DexHttpConn* conn;
    uint32_t     gen;
    int          lane;
    int          next_free;
} DexLaneSlot;

typedef struct {
    int    fd;
    int    dead;
    int    recv_inflight;
    int    send_inflight;
    char*  out;
    size_t out_len, out_cap;
    char*  sending;
    size_t send_len, send_pos, send_cap;
    char*  in;
    size_t in_len, in_cap;
} DexLaneLink;

static int dex_lane_count = 0;
static pid_t dex_lane_owner = 0;
static DexLaneSlot* dex_lane_slots = NULL;
static int dex_lane_slots_len = 0;
static int dex_lane_slots_cap = 0;
static int dex_lane_free_head = -1;
static DexLaneLink dex_lane_links[DEX_LANE_MAX];
static int dex_lane_uring_live = 0;

static int dex_lane_serving = 0;
static int dex_lane_batch_len = 0;
static int* dex_lane_reply_status = NULL;
static DexString** dex_lane_reply_body = NULL;
static char** dex_lane_reply_ct = NULL;

static void dex_lane_forget(DexHttpConn* conn) {
    int slot = conn->relay_slot - 1;
    conn->relay_slot = 0;
    if (slot < 0 || slot >= dex_lane_slots_len || dex_lane_slots[slot].conn != conn) return;
    dex_lane_slots[slot].conn = NULL;
    dex_lane_slots[slot].gen = (dex_lane_slots[slot].gen + 1) & 0xFFF;
    dex_lane_slots[slot].next_free = dex_lane_free_head;
    dex_lane_free_head = slot;
}

static int dex_lane_slot_take(DexHttpConn* conn, int lane) {
    int slot = dex_lane_free_head;
    if (slot >= 0) {
        dex_lane_free_head = dex_lane_slots[slot].next_free;
    } else {
        if (dex_lane_slots_len >= (1 << DEX_LANE_SLOT_BITS)) return -1;
        if (dex_lane_slots_len == dex_lane_slots_cap) {
            int cap = dex_lane_slots_cap ? dex_lane_slots_cap * 2 : 1024;
            DexLaneSlot* grown = (DexLaneSlot*)dex_xrealloc(dex_lane_slots, (size_t)cap * sizeof(DexLaneSlot));
            if (!grown) return -1;
            dex_lane_slots = grown;
            dex_lane_slots_cap = cap;
        }
        slot = dex_lane_slots_len++;
        dex_lane_slots[slot].gen = 0;
    }
    dex_lane_slots[slot].conn = conn;
    dex_lane_slots[slot].lane = lane;
    dex_lane_slots[slot].next_free = -1;
    conn->relay_slot = slot + 1;
    return slot;
}

static _Bool dex_lane_store_reply(int ticket, int status, DexString* body, const char* content_type) {
    if (!dex_lane_serving) return 0;
    if (ticket < 0 || ticket >= dex_lane_batch_len || dex_lane_reply_status[ticket]) return 1;
    dex_lane_reply_status[ticket] = status > 0 ? status : 500;
    dex_retain(body);
    dex_lane_reply_body[ticket] = body;
    dex_lane_reply_ct[ticket] = (content_type && *content_type) ? dex_xstrdup(content_type) : NULL;
    return 1;
}

static int dex_lane_grow(char** buf, size_t* cap, size_t need) {
    if (need <= *cap) return 0;
    size_t cap2 = *cap ? *cap : 65536;
    while (cap2 < need) {
        if (cap2 > ((size_t)1 << 40)) return -1;
        cap2 *= 2;
    }
    char* grown = (char*)dex_xrealloc(*buf, cap2);
    if (!grown) return -1;
    *buf = grown;
    *cap = cap2;
    return 0;
}

static void dex_lane_put32(char* p, uint32_t v) { memcpy(p, &v, 4); }
static uint32_t dex_lane_get32(const char* p) { uint32_t v; memcpy(&v, p, 4); return v; }

#ifdef __linux__
#include <sys/un.h>
#include <sys/epoll.h>
#include <poll.h>
#include <stddef.h>

static socklen_t dex_lane_addr(struct sockaddr_un* sa, pid_t owner, int lane) {
    memset(sa, 0, sizeof(*sa));
    sa->sun_family = AF_UNIX;
    int n = snprintf(sa->sun_path + 1, sizeof(sa->sun_path) - 1, "dex-lane-%d-%d", (int)owner, lane);
    return (socklen_t)(offsetof(struct sockaddr_un, sun_path) + 1 + (size_t)n);
}

typedef struct {
    int    fd;
    char*  in;
    size_t in_len, in_cap;
    char*  out;
    size_t out_len, out_cap;
} DexLaneClient;

typedef struct {
    int        client;
    uint32_t   id;
    DexString* payload;
} DexLaneItem;

typedef void (*dex_lane_serve_fn)(void* env, int lane, DexArrayString* payloads);

static void dex_lane_send_all(DexLaneClient* c) {
    size_t pos = 0;
    while (c->fd >= 0 && pos < c->out_len) {
        ssize_t w = send(c->fd, c->out + pos, c->out_len - pos, MSG_NOSIGNAL);
        if (w > 0) { pos += (size_t)w; continue; }
        if (w < 0 && errno == EINTR) continue;
        if (w < 0 && (errno == EAGAIN || errno == EWOULDBLOCK)) {
            struct pollfd p = { c->fd, POLLOUT, 0 };
            poll(&p, 1, 1000);
            continue;
        }
        close(c->fd);
        c->fd = -1;
    }
    c->out_len = 0;
}

static void dex_lane_answer(DexLaneClient* c, uint32_t id, int status, DexString* body, const char* ct) {
    if (c->fd < 0) return;
    const char* data = body ? body->data : "{\"error\":\"internal server error\"}";
    size_t blen = body ? body->len : strlen(data);
    size_t ctlen = ct ? strlen(ct) : 0;
    if (ctlen > 255) ctlen = 255;
    size_t flen = 4 + 2 + 1 + ctlen + blen;
    if (flen > DEX_LANE_FRAME_MAX || dex_lane_grow(&c->out, &c->out_cap, c->out_len + 4 + flen) < 0) {
        data = "{\"error\":\"internal server error\"}";
        blen = strlen(data);
        ctlen = 0;
        status = 500;
        flen = 4 + 2 + 1 + blen;
        if (dex_lane_grow(&c->out, &c->out_cap, c->out_len + 4 + flen) < 0) return;
    }
    char* w = c->out + c->out_len;
    uint16_t st = (uint16_t)status;
    dex_lane_put32(w, (uint32_t)flen);
    dex_lane_put32(w + 4, id);
    memcpy(w + 8, &st, 2);
    w[10] = (char)(unsigned char)ctlen;
    if (ctlen) memcpy(w + 11, ct, ctlen);
    memcpy(w + 11 + ctlen, data, blen);
    c->out_len += 4 + flen;
}

__attribute__((noreturn)) static void dex_lane_run(int lane, int listen_fd, DexClosure* serve) {
    int ep = epoll_create1(EPOLL_CLOEXEC);
    if (ep < 0) _exit(1);
    struct epoll_event ev = { .events = EPOLLIN, .data.u64 = (uint64_t)-1 };
    epoll_ctl(ep, EPOLL_CTL_ADD, listen_fd, &ev);
    DexLaneClient* clients = NULL;
    int nclients = 0, capclients = 0;
    DexLaneItem* queue = NULL;
    int qlen = 0, qcap = 0;
    uint64_t opened = 0;
    struct epoll_event evs[64];
    for (;;) {
        int timeout = -1;
        if (qlen > 0) {
            timeout = 0;
            if (dex_http_batch_window_ms > 0 && qlen < DEX_LANE_BATCH_MAX) {
                struct timespec ts;
                clock_gettime(CLOCK_MONOTONIC, &ts);
                uint64_t now = (uint64_t)ts.tv_sec * 1000 + (uint64_t)ts.tv_nsec / 1000000;
                uint64_t age = now - opened;
                if (age < (uint64_t)dex_http_batch_window_ms) timeout = (int)((uint64_t)dex_http_batch_window_ms - age);
            }
        }
        int n = epoll_wait(ep, evs, 64, timeout);
        if (n < 0 && errno != EINTR) _exit(1);
        for (int e = 0; e < n; e++) {
            if (evs[e].data.u64 == (uint64_t)-1) {
                for (;;) {
                    int fd = accept4(listen_fd, NULL, NULL, SOCK_CLOEXEC | SOCK_NONBLOCK);
                    if (fd < 0) break;
                    if (nclients == capclients) {
                        int cap = capclients ? capclients * 2 : 64;
                        DexLaneClient* grown = (DexLaneClient*)dex_xrealloc(clients, (size_t)cap * sizeof(DexLaneClient));
                        if (!grown) { close(fd); break; }
                        clients = grown;
                        capclients = cap;
                    }
                    memset(&clients[nclients], 0, sizeof(DexLaneClient));
                    clients[nclients].fd = fd;
                    struct epoll_event cev = { .events = EPOLLIN, .data.u64 = (uint64_t)nclients };
                    epoll_ctl(ep, EPOLL_CTL_ADD, fd, &cev);
                    nclients++;
                }
                continue;
            }
            int ci = (int)evs[e].data.u64;
            DexLaneClient* c = &clients[ci];
            for (;;) {
                if (c->fd < 0) break;
                if (dex_lane_grow(&c->in, &c->in_cap, c->in_len + 65536) < 0) { close(c->fd); c->fd = -1; break; }
                ssize_t r = recv(c->fd, c->in + c->in_len, c->in_cap - c->in_len, MSG_DONTWAIT);
                if (r < 0 && errno == EINTR) continue;
                if (r < 0 && (errno == EAGAIN || errno == EWOULDBLOCK)) break;
                if (r <= 0) { close(c->fd); c->fd = -1; break; }
                c->in_len += (size_t)r;
            }
            size_t pos = 0;
            while (c->in_len - pos >= 8) {
                uint32_t flen = dex_lane_get32(c->in + pos);
                if (flen < 4 || flen > DEX_LANE_FRAME_MAX) { if (c->fd >= 0) close(c->fd); c->fd = -1; pos = c->in_len; break; }
                if (c->in_len - pos < 4 + (size_t)flen) break;
                if (qlen == qcap) {
                    int cap = qcap ? qcap * 2 : 1024;
                    DexLaneItem* grown = (DexLaneItem*)dex_xrealloc(queue, (size_t)cap * sizeof(DexLaneItem));
                    if (!grown) _exit(1);
                    queue = grown;
                    qcap = cap;
                }
                if (qlen == 0) {
                    struct timespec ts;
                    clock_gettime(CLOCK_MONOTONIC, &ts);
                    opened = (uint64_t)ts.tv_sec * 1000 + (uint64_t)ts.tv_nsec / 1000000;
                }
                queue[qlen].client = ci;
                queue[qlen].id = dex_lane_get32(c->in + pos + 4);
                queue[qlen].payload = dex_string_new(c->in + pos + 8, flen - 4);
                qlen++;
                pos += 4 + (size_t)flen;
            }
            if (pos > 0) {
                memmove(c->in, c->in + pos, c->in_len - pos);
                c->in_len -= pos;
            }
        }
        if (qlen == 0) continue;
        if (dex_http_batch_window_ms > 0 && qlen < DEX_LANE_BATCH_MAX) {
            struct timespec ts;
            clock_gettime(CLOCK_MONOTONIC, &ts);
            uint64_t now = (uint64_t)ts.tv_sec * 1000 + (uint64_t)ts.tv_nsec / 1000000;
            if (now - opened < (uint64_t)dex_http_batch_window_ms) continue;
        }
        int take = qlen < DEX_LANE_BATCH_MAX ? qlen : DEX_LANE_BATCH_MAX;
        DexArrayString* payloads = dex_array_string_new();
        for (int i = 0; i < take; i++) {
            dex_array_string_push(payloads, queue[i].payload);
            dex_release(queue[i].payload);
            queue[i].payload = NULL;
        }
        dex_lane_reply_status = (int*)dex_xcalloc((size_t)take, sizeof(int));
        dex_lane_reply_body = (DexString**)dex_xcalloc((size_t)take, sizeof(DexString*));
        dex_lane_reply_ct = (char**)dex_xcalloc((size_t)take, sizeof(char*));
        if (!dex_lane_reply_status || !dex_lane_reply_body || !dex_lane_reply_ct) _exit(1);
        dex_lane_batch_len = take;
        dex_lane_serving = 1;
        ((dex_lane_serve_fn)serve->fn)(serve->env, lane, payloads);
        dex_lane_serving = 0;
        dex_lane_batch_len = 0;
        dex_release(payloads);
        for (int i = 0; i < take; i++) {
            DexLaneClient* c = &clients[queue[i].client];
            int st = dex_lane_reply_status[i];
            dex_lane_answer(c, queue[i].id, st ? st : 500, st ? dex_lane_reply_body[i] : NULL, dex_lane_reply_ct[i]);
            if (dex_lane_reply_body[i]) dex_release(dex_lane_reply_body[i]);
            free(dex_lane_reply_ct[i]);
        }
        free(dex_lane_reply_status);
        free(dex_lane_reply_body);
        free(dex_lane_reply_ct);
        dex_lane_reply_status = NULL;
        dex_lane_reply_body = NULL;
        dex_lane_reply_ct = NULL;
        for (int i = 0; i < nclients; i++) {
            if (clients[i].out_len) dex_lane_send_all(&clients[i]);
        }
        memmove(queue, queue + take, (size_t)(qlen - take) * sizeof(DexLaneItem));
        qlen -= take;
        if (qlen > 0) {
            struct timespec ts;
            clock_gettime(CLOCK_MONOTONIC, &ts);
            opened = (uint64_t)ts.tv_sec * 1000 + (uint64_t)ts.tv_nsec / 1000000;
        }
    }
}

void dex_http_lanes(int count, DexClosure* serve) {
    if (dex_lane_count > 0 || count < 1 || count > DEX_LANE_MAX || dex_http_worker_index >= 0) {
        if (serve) dex_release(serve);
        return;
    }
    signal(SIGPIPE, SIG_IGN);
    pid_t owner = getpid();
    fflush(stdout);
    fflush(stderr);
    int started = 0;
    for (int i = 0; i < count; i++) {
        int fd = socket(AF_UNIX, SOCK_STREAM | SOCK_CLOEXEC | SOCK_NONBLOCK, 0);
        if (fd < 0) break;
        struct sockaddr_un sa;
        socklen_t len = dex_lane_addr(&sa, owner, i);
        if (bind(fd, (struct sockaddr*)&sa, len) < 0 || listen(fd, 1024) < 0) { close(fd); break; }
        pid_t pid = fork();
        if (pid < 0) { close(fd); break; }
        if (pid == 0) {
            prctl(PR_SET_PDEATHSIG, SIGTERM);
            if (getppid() != owner) _exit(0);
            dex_lane_run(i, fd, serve);
        }
        close(fd);
        started++;
    }
    dex_lane_owner = owner;
    dex_lane_count = started;
    for (int i = 0; i < DEX_LANE_MAX; i++) dex_lane_links[i].fd = -1;
    if (serve) dex_release(serve);
}

static int dex_lane_connect(int lane) {
    int fd = socket(AF_UNIX, SOCK_STREAM | SOCK_CLOEXEC, 0);
    if (fd < 0) return -1;
    struct sockaddr_un sa;
    socklen_t len = dex_lane_addr(&sa, dex_lane_owner, lane);
    for (int tries = 0; tries < 50; tries++) {
        if (connect(fd, (struct sockaddr*)&sa, len) == 0) return fd;
        if (errno != ECONNREFUSED && errno != ENOENT && errno != EAGAIN) break;
        usleep(20000);
    }
    close(fd);
    return -1;
}
#else
void dex_http_lanes(int count, DexClosure* serve) {
    (void)count;
    if (serve) dex_release(serve);
}
#endif

/* The one-request batch of the epoll and pool paths. */
static void dex_http_after_one(DexHttpConn* conn) {
    if (!dex_http_after_batch) return;
    if (!dex_http_run_batch_hook()) dex_http_answer_batch_failed(conn);
}

static void dex_http_worker_func(void* arg) {
    DexHttpConn* conn = (DexHttpConn*)arg;
    dex_http_run_handler(conn);
    dex_http_after_one(conn);
    pthread_mutex_lock(&dex_http_completed_mutex);
    conn->next = dex_http_completed_head;
    dex_http_completed_head = conn;
    pthread_mutex_unlock(&dex_http_completed_mutex);
    dex_notify_pipe_signal(&dex_http_notify);
}

static void dex_http_flush_inline(DexHttpConn* conn) {
    while (conn->write_pos < conn->write_len) {
        int w = (int)write(conn->fd, conn->write_buf + conn->write_pos, conn->write_len - conn->write_pos);
        if (w < 0 && (errno == EAGAIN || errno == EWOULDBLOCK)) {
            dex_ev_mod(dex_http_loop, conn->fd, DEX_EV_WRITE, conn);
            return;
        }
        if (w <= 0) {
            dex_ev_del(dex_http_loop, conn->fd);
            close(conn->fd);
            dex_http_conn_free(conn);
            return;
        }
        conn->write_pos += w;
    }
    if (conn->keep_alive) {
        dex_http_conn_reset(conn);
    } else {
        dex_ev_del(dex_http_loop, conn->fd);
        close(conn->fd);
        dex_http_conn_free(conn);
    }
}


#ifdef DEX_HTTP_HAVE_URING

#include <sys/epoll.h>
#include <sys/eventfd.h>
#include <sys/utsname.h>

#define DEX_URING_SQ_ENTRIES 4096
#define DEX_URING_CQ_ENTRIES 65536
#define DEX_UD_ACCEPT  1
#define DEX_UD_TIMEOUT 2
#define DEX_UD_IGNORE  3
#define DEX_UD_ACCEPT_DIRECT 4
#define DEX_UD_WINDOW 5
#define DEX_UD_LANE_SEND 6
#define DEX_UD_LANE_RECV 7
#define DEX_OP_RECV 1
#define DEX_OP_SEND 2

/* Rings run on the one loop thread. Each has its own table of direct
 * descriptors, and the kernel caps a table at the open-file limit, so one ring
 * holds at most about 65,000 connections however high the program's own limit
 * is set. Asking http.limits() for more than one ring can hold adds rings; the
 * default limit never needs a second one. */
#define DEX_URING_MAX_RINGS 8

/* Received bytes land in buffers the ring owns and are copied out at once into
 * a per-request buffer, so a connection waiting for its next request holds no
 * read buffer at all. With a few hundred thousand idle keep-alive clients that
 * is the difference between a buffer each and a few megabytes in total. */
/* IORING_REGISTER_PBUF_RING is an enum, so it cannot be tested for directly.
 * Buffer rings (5.19) predate IORING_SETUP_DEFER_TASKRUN (6.1), which is a
 * macro: headers that have it have them. */
#if defined(IORING_SETUP_DEFER_TASKRUN) && defined(IORING_CQE_F_BUFFER)
#define DEX_URING_HAVE_PBUF 1
#endif
#define DEX_URING_BUFS 4096
#define DEX_URING_BUF_BYTES 2048
#define DEX_URING_RBUF_BYTES 2048
#define DEX_URING_RBUF_POOL 1024

typedef struct {
    int fd;
    unsigned* sq_head;
    unsigned* sq_tail;
    unsigned* sq_mask;
    unsigned* sq_array;
    unsigned* cq_head;
    unsigned* cq_tail;
    unsigned* cq_mask;
    struct io_uring_sqe* sqes;
    struct io_uring_cqe* cqes;
    unsigned sq_entries;
    unsigned local_tail;
    void* sq_map;
    size_t sq_map_size;
    void* cq_map;
    size_t cq_map_size;
    size_t sqes_size;
    unsigned* sq_flags;
    int taskrun_flag;      /* the kernel sets IORING_SQ_TASKRUN in sq_flags when work waits */
    int ext_arg;           /* io_uring_enter takes a timeout (IORING_FEAT_EXT_ARG) */
} DexUring;

typedef struct {
    DexUring u;
    int index;
    int accepts_pending;
    int listener_pending[DEX_HTTP_MAX_LISTENERS];
    int direct_pending;
    int direct_cap;            /* size of direct and direct_idle */
    int direct_slots;          /* how many of them accepts may fill */
    int direct_live;
    DexHttpConn** direct;      /* by direct slot */
    uint32_t* direct_idle;     /* by direct slot: see dex_uring_idle */
#ifdef DEX_URING_HAVE_PBUF
    struct io_uring_buf_ring* br;   /* NULL when the kernel has no buffer rings */
#else
    void* br;
#endif
    char* bufs;
    unsigned br_tail;
    int efd;
} DexUringRing;

static DexUringRing dex_urings[DEX_URING_MAX_RINGS];
static int dex_uring_count = 0;
static int dex_uring_epfd = -1;
static int dex_uring_real_live = 0;
static int dex_uring_real_cap = 0;

/* The second each connection became idle-eligible (reading, waiting on the
 * client), or 0 while the loop is working on it. The sweep reads these arrays
 * and touches a connection only when it has actually timed out, instead of
 * reading every connection's state once a second. Real descriptors are
 * indexed by fd, direct ones by slot in their ring's array. */
static uint32_t dex_uring_real_idle[DEX_HTTP_MAX_FD];

static DexHttpConn* dex_uring_pass_head = NULL;
static int dex_uring_pass_count = 0;

/* http.batchWindow: the batch stays open until its first request is this old,
 * or this full, and a timeout on the ring wakes the loop when it is due. */
#define DEX_URING_BATCH_MAX 4096
static uint64_t dex_uring_batch_opened = 0;
static int dex_uring_window_armed = 0;
static struct __kernel_timespec dex_uring_window_ts;

static char* dex_uring_rbuf_pool[DEX_URING_RBUF_POOL];
static int dex_uring_rbuf_pool_len = 0;

static inline DexUringRing* dex_uring_ring_of(DexHttpConn* conn) {
    return &dex_urings[conn->ring];
}

static inline void dex_uring_idle(DexHttpConn* conn, uint32_t since) {
    if (conn->direct) {
        DexUringRing* r = dex_uring_ring_of(conn);
        if (conn->fd >= 0 && conn->fd < r->direct_cap) r->direct_idle[conn->fd] = since;
    } else if (conn->fd >= 0 && conn->fd < DEX_HTTP_MAX_FD) {
        dex_uring_real_idle[conn->fd] = since;
    }
}

static inline uint32_t dex_uring_now(void) {
    return (uint32_t)time(NULL);
}

static void dex_uring_unmap(DexUring* u) {
    if (u->sqes) munmap(u->sqes, u->sqes_size);
    if (u->cq_map && u->cq_map != u->sq_map) munmap(u->cq_map, u->cq_map_size);
    if (u->sq_map) munmap(u->sq_map, u->sq_map_size);
    if (u->fd >= 0) close(u->fd);
    memset(u, 0, sizeof(*u));
    u->fd = -1;
}

/* Deferred task work keeps completions on the loop thread until it next enters
 * the ring, which is the cheapest arrangement and the one a lone ring uses.
 * With several rings the thread sleeps in epoll instead, and it is the ring's
 * eventfd that has to wake it. Kernels from 6.2 signal that eventfd when
 * deferred work arrives; on anything older the rings are set up without
 * deferral, where completions post (and signal) on their own. */
static int dex_uring_kernel_at_least(int major, int minor) {
    struct utsname u;
    if (uname(&u) != 0) return 0;
    int a = 0, b = 0;
    if (sscanf(u.release, "%d.%d", &a, &b) != 2) return 0;
    return a > major || (a == major && b >= minor);
}

static int dex_uring_setup(DexUring* r, int allow_defer) {
    struct io_uring_params p;
    unsigned base = IORING_SETUP_CQSIZE;
    unsigned tries[3] = { base, base, base };
#if defined(IORING_SETUP_SINGLE_ISSUER) && defined(IORING_SETUP_DEFER_TASKRUN)
    if (allow_defer) {
        tries[1] = base | IORING_SETUP_SINGLE_ISSUER | IORING_SETUP_DEFER_TASKRUN;
        tries[0] = tries[1];
#ifdef IORING_SETUP_TASKRUN_FLAG
        tries[0] |= IORING_SETUP_TASKRUN_FLAG;
#endif
    }
#endif
    (void)allow_defer;
    int fd = -1;
    for (int i = 0; i < 3 && fd < 0; i++) {
        memset(&p, 0, sizeof(p));
        p.flags = tries[i];
        p.cq_entries = DEX_URING_CQ_ENTRIES;
        fd = (int)syscall(__NR_io_uring_setup, DEX_URING_SQ_ENTRIES, &p);
    }
    if (fd < 0) return -1;

    size_t sq_size = p.sq_off.array + p.sq_entries * sizeof(unsigned);
    size_t cq_size = p.cq_off.cqes + p.cq_entries * sizeof(struct io_uring_cqe);
    int single = (p.features & IORING_FEAT_SINGLE_MMAP) != 0;
    if (single && cq_size > sq_size) sq_size = cq_size;

    void* sq = mmap(NULL, sq_size, PROT_READ | PROT_WRITE, MAP_SHARED | MAP_POPULATE, fd, IORING_OFF_SQ_RING);
    if (sq == MAP_FAILED) { close(fd); return -1; }
    void* cq = sq;
    if (!single) {
        cq = mmap(NULL, cq_size, PROT_READ | PROT_WRITE, MAP_SHARED | MAP_POPULATE, fd, IORING_OFF_CQ_RING);
        if (cq == MAP_FAILED) { munmap(sq, sq_size); close(fd); return -1; }
    }
    size_t sqes_size = p.sq_entries * sizeof(struct io_uring_sqe);
    void* sqes = mmap(NULL, sqes_size, PROT_READ | PROT_WRITE, MAP_SHARED | MAP_POPULATE, fd, IORING_OFF_SQES);
    if (sqes == MAP_FAILED) {
        if (!single) munmap(cq, cq_size);
        munmap(sq, sq_size);
        close(fd);
        return -1;
    }

    r->fd = fd;
    r->sq_head = (unsigned*)((char*)sq + p.sq_off.head);
    r->sq_tail = (unsigned*)((char*)sq + p.sq_off.tail);
    r->sq_mask = (unsigned*)((char*)sq + p.sq_off.ring_mask);
    r->sq_array = (unsigned*)((char*)sq + p.sq_off.array);
    r->cq_head = (unsigned*)((char*)cq + p.cq_off.head);
    r->cq_tail = (unsigned*)((char*)cq + p.cq_off.tail);
    r->cq_mask = (unsigned*)((char*)cq + p.cq_off.ring_mask);
    r->cqes = (struct io_uring_cqe*)((char*)cq + p.cq_off.cqes);
    r->sqes = (struct io_uring_sqe*)sqes;
    r->sq_entries = p.sq_entries;
    r->local_tail = *r->sq_tail;
    r->sq_map = sq;
    r->sq_map_size = sq_size;
    r->cq_map = cq;
    r->cq_map_size = cq_size;
    r->sqes_size = sqes_size;
    r->sq_flags = (unsigned*)((char*)sq + p.sq_off.flags);
    r->taskrun_flag = 0;
    r->ext_arg = 0;
#ifdef IORING_FEAT_EXT_ARG
    r->ext_arg = (p.features & IORING_FEAT_EXT_ARG) != 0;
#endif
#if defined(IORING_SETUP_TASKRUN_FLAG) && defined(IORING_SETUP_DEFER_TASKRUN)
    r->taskrun_flag = (p.flags & IORING_SETUP_TASKRUN_FLAG) && (p.flags & IORING_SETUP_DEFER_TASKRUN);
#endif
    return 0;
}

/* Whether entering the ring would do anything: SQEs to submit, or (when the
 * kernel says so through IORING_SQ_TASKRUN) deferred completions to post. A
 * ring without the flag always counts as busy. */
static int dex_uring_wants_enter(DexUring* r) {
    if (!r->taskrun_flag) return 1;
    if (r->local_tail != __atomic_load_n(r->sq_head, __ATOMIC_ACQUIRE)) return 1;
#ifdef IORING_SQ_TASKRUN
    return (__atomic_load_n(r->sq_flags, __ATOMIC_ACQUIRE) & IORING_SQ_TASKRUN) != 0;
#else
    return 1;
#endif
}

/* Submit what is queued. wait_nr > 0 blocks for that many completions; with
 * getevents and wait_nr 0 it only runs deferred work, so completions that have
 * arrived are posted, and returns at once. */
static int dex_uring_enter(DexUring* r, unsigned wait_nr, int getevents) {
    __atomic_store_n(r->sq_tail, r->local_tail, __ATOMIC_RELEASE);
    unsigned to_submit = r->local_tail - __atomic_load_n(r->sq_head, __ATOMIC_ACQUIRE);
    unsigned flags = (wait_nr || getevents) ? IORING_ENTER_GETEVENTS : 0;
    for (;;) {
        int ret = (int)syscall(__NR_io_uring_enter, r->fd, to_submit, wait_nr, flags, NULL, 0);
        if (ret >= 0 || errno != EINTR) return ret;
    }
}

/* Submit what is queued, then sleep until wait_nr completions are waiting or
 * micros have passed, whichever is first. Returns -1 when the kernel can't
 * take a timeout, and the caller waits the plain way. */
static int dex_uring_enter_timed(DexUring* r, unsigned wait_nr, unsigned micros) {
#ifdef IORING_ENTER_EXT_ARG
    if (!r->ext_arg) return -1;
    __atomic_store_n(r->sq_tail, r->local_tail, __ATOMIC_RELEASE);
    unsigned to_submit = r->local_tail - __atomic_load_n(r->sq_head, __ATOMIC_ACQUIRE);
    struct __kernel_timespec ts = { .tv_sec = micros / 1000000, .tv_nsec = (long long)(micros % 1000000) * 1000 };
    struct io_uring_getevents_arg arg;
    memset(&arg, 0, sizeof(arg));
    arg.ts = (unsigned long long)(uintptr_t)&ts;
    for (;;) {
        int ret = (int)syscall(__NR_io_uring_enter, r->fd, to_submit, wait_nr,
                               IORING_ENTER_GETEVENTS | IORING_ENTER_EXT_ARG, &arg, sizeof(arg));
        if (ret >= 0 || errno == ETIME) return 0;
        if (errno != EINTR) return ret;
        to_submit = 0;
    }
#else
    (void)r; (void)wait_nr; (void)micros;
    return -1;
#endif
}

static struct io_uring_sqe* dex_uring_sqe(DexUring* r) {
    if (r->local_tail - __atomic_load_n(r->sq_head, __ATOMIC_ACQUIRE) >= r->sq_entries) {
        dex_uring_enter(r, 0, 0);
        if (r->local_tail - __atomic_load_n(r->sq_head, __ATOMIC_ACQUIRE) >= r->sq_entries) return NULL;
    }
    unsigned idx = r->local_tail & *r->sq_mask;
    struct io_uring_sqe* sqe = &r->sqes[idx];
    memset(sqe, 0, sizeof(*sqe));
    r->sq_array[idx] = idx;
    r->local_tail++;
    return sqe;
}

#define DEX_URING_ACCEPTS 64

static unsigned dex_uring_direct_limit(void) {
    struct rlimit rl;
    unsigned slots = DEX_HTTP_MAX_FD;
    if (getrlimit(RLIMIT_NOFILE, &rl) == 0 && rl.rlim_cur != RLIM_INFINITY && rl.rlim_cur < slots) slots = (unsigned)rl.rlim_cur;
    return slots;
}

static void dex_uring_register_direct(DexUringRing* r, unsigned slots) {
#if defined(IORING_RSRC_REGISTER_SPARSE) && defined(IORING_FILE_INDEX_ALLOC)
    struct io_uring_rsrc_register rr;
    memset(&rr, 0, sizeof(rr));
    rr.nr = slots;
    rr.flags = IORING_RSRC_REGISTER_SPARSE;
    if (syscall(__NR_io_uring_register, r->u.fd, IORING_REGISTER_FILES2, &rr, sizeof(rr)) != 0) return;
    r->direct = (DexHttpConn**)dex_xcalloc(slots, sizeof(DexHttpConn*));
    r->direct_idle = (uint32_t*)dex_xcalloc(slots, sizeof(uint32_t));
    r->direct_cap = (int)slots;
    r->direct_slots = (int)slots;
#else
    (void)r; (void)slots;
#endif
}

#ifdef DEX_URING_HAVE_PBUF
static inline void dex_uring_buf_put(DexUringRing* r, unsigned bid) {
    struct io_uring_buf* b = &r->br->bufs[r->br_tail & (DEX_URING_BUFS - 1)];
    b->addr = (unsigned long)(r->bufs + (size_t)bid * DEX_URING_BUF_BYTES);
    b->len = DEX_URING_BUF_BYTES;
    b->bid = (unsigned short)bid;
    r->br_tail++;
}

static inline void dex_uring_buf_publish(DexUringRing* r) {
    __atomic_store_n(&r->br->tail, (unsigned short)r->br_tail, __ATOMIC_RELEASE);
}
#endif

/* Register this ring's receive buffers. On a kernel without buffer rings (or
 * if the memory is not there) the ring keeps r->br NULL and every connection
 * reads into a buffer of its own, which is how it always worked. */
static void dex_uring_setup_bufs(DexUringRing* r) {
    r->br = NULL;
#ifdef DEX_URING_HAVE_PBUF
    size_t ring_bytes = DEX_URING_BUFS * sizeof(struct io_uring_buf);
    size_t buf_bytes = (size_t)DEX_URING_BUFS * DEX_URING_BUF_BYTES;
    void* br = mmap(NULL, ring_bytes, PROT_READ | PROT_WRITE, MAP_PRIVATE | MAP_ANONYMOUS, -1, 0);
    if (br == MAP_FAILED) return;
    void* bufs = mmap(NULL, buf_bytes, PROT_READ | PROT_WRITE, MAP_PRIVATE | MAP_ANONYMOUS, -1, 0);
    if (bufs == MAP_FAILED) { munmap(br, ring_bytes); return; }
    struct io_uring_buf_reg reg;
    memset(&reg, 0, sizeof(reg));
    reg.ring_addr = (unsigned long)br;
    reg.ring_entries = DEX_URING_BUFS;
    reg.bgid = 0;
    if (syscall(__NR_io_uring_register, r->u.fd, IORING_REGISTER_PBUF_RING, &reg, 1) != 0) {
        munmap(bufs, buf_bytes);
        munmap(br, ring_bytes);
        return;
    }
    r->br = (struct io_uring_buf_ring*)br;
    r->bufs = (char*)bufs;
    r->br_tail = 0;
    for (unsigned i = 0; i < DEX_URING_BUFS; i++) dex_uring_buf_put(r, i);
    dex_uring_buf_publish(r);
#endif
}

/* A connection's own read buffer, taken when bytes arrive and handed back when
 * the request is answered. Buffers that grew past the standard size are freed
 * rather than kept. */
static char* dex_uring_rbuf_take(void) {
    if (dex_uring_rbuf_pool_len > 0) return dex_uring_rbuf_pool[--dex_uring_rbuf_pool_len];
    return (char*)dex_xmalloc(DEX_URING_RBUF_BYTES);
}

static void dex_uring_rbuf_release(DexHttpConn* conn) {
    char* b = conn->read_buf;
    conn->read_buf = NULL;
    if (b) {
        if (conn->read_cap == DEX_URING_RBUF_BYTES && dex_uring_rbuf_pool_len < DEX_URING_RBUF_POOL) {
            dex_uring_rbuf_pool[dex_uring_rbuf_pool_len++] = b;
        } else {
            free(b);
        }
    }
    conn->read_cap = 0;
}

/* How many bytes this request may still take in, and room for that many plus a
 * terminator. The ceiling is the header cap while headers are read and the
 * end of the body once Content-Length is known, as dex_uring_need_more applies
 * it. Returns -1 when memory runs out. */
static int dex_uring_reserve(DexHttpConn* conn, int want) {
    int ceiling = DEX_HTTP_MAX_HEADER_BYTES;
    if (conn->state == HTTP_READING_BODY) ceiling = conn->headers_end + conn->content_length;
    int room = ceiling - conn->read_len;
    if (room <= 0) return 0;
    if (want < room) room = want;
    if (!conn->read_buf) {
        conn->read_buf = dex_uring_rbuf_take();
        if (!conn->read_buf) return -1;
        conn->read_cap = DEX_URING_RBUF_BYTES;
    }
    int need = conn->read_len + room + 1;
    if (need > conn->read_cap) {
        int new_cap = conn->read_cap * 2;
        if (new_cap < need) new_cap = need;
        if (new_cap > ceiling + 1) new_cap = ceiling + 1;
        char* nb = (char*)dex_xrealloc(conn->read_buf, (size_t)new_cap);
        if (!nb) return -1;
        conn->read_buf = nb;
        conn->read_cap = new_cap;
    }
    return room;
}

static void dex_uring_target(struct io_uring_sqe* sqe, DexHttpConn* conn) {
    sqe->fd = conn->fd;
    if (conn->direct) sqe->flags |= IOSQE_FIXED_FILE;
}
static struct __kernel_timespec dex_uring_tick = { 1, 0 };

static int dex_uring_queue_accept(DexUringRing* r, int server_fd, int direct, int listener) {
    struct io_uring_sqe* sqe = dex_uring_sqe(&r->u);
    if (!sqe) return -1;
    sqe->opcode = IORING_OP_ACCEPT;
    sqe->fd = server_fd;
    r->listener_pending[listener]++;
    r->accepts_pending++;
#ifdef IORING_FILE_INDEX_ALLOC
    if (direct) {
        sqe->file_index = IORING_FILE_INDEX_ALLOC;
        sqe->user_data = DEX_UD_ACCEPT_DIRECT + 16 * (unsigned long)listener;
        r->direct_pending++;
        return 0;
    }
#endif
    sqe->accept_flags = SOCK_CLOEXEC;
    sqe->user_data = DEX_UD_ACCEPT + 16 * (unsigned long)listener;
    return 0;
}

/* Every ring accepts into its own direct table while it has room. Only the
 * first ring falls back to ordinary descriptors, which all rings share. Each
 * listening port gets an equal share of a ring's accepts in flight. */
static void dex_uring_arm_accepts(DexUringRing* r) {
    int quota = DEX_URING_ACCEPTS / dex_http_listen_count;
    for (int k = 0; k < dex_http_listen_count; k++) {
        while (r->listener_pending[k] < quota) {
            int direct = 0;
            if (r->direct_live + r->direct_pending < r->direct_slots) {
                direct = 1;
            } else {
                if (r->index != 0) return;
                int real_pending = r->accepts_pending - r->direct_pending;
                if (dex_uring_real_live + real_pending >= dex_uring_real_cap) return;
            }
            if (dex_uring_queue_accept(r, dex_http_listen_fds[k], direct, k) != 0) return;
        }
    }
}

static uint64_t dex_uring_now_ms(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (uint64_t)ts.tv_sec * 1000 + (uint64_t)ts.tv_nsec / 1000000;
}

static void dex_uring_queue_window(uint64_t ms) {
    struct io_uring_sqe* sqe = dex_uring_sqe(&dex_urings[0].u);
    if (!sqe) return;
    dex_uring_window_ts.tv_sec = (long long)(ms / 1000);
    dex_uring_window_ts.tv_nsec = (long long)(ms % 1000) * 1000000;
    sqe->opcode = IORING_OP_TIMEOUT;
    sqe->addr = (unsigned long)&dex_uring_window_ts;
    sqe->len = 1;
    sqe->user_data = DEX_UD_WINDOW;
    dex_uring_window_armed = 1;
}

static void dex_uring_queue_timeout(void) {
    struct io_uring_sqe* sqe = dex_uring_sqe(&dex_urings[0].u);
    if (!sqe) return;
    sqe->opcode = IORING_OP_TIMEOUT;
    sqe->addr = (unsigned long)&dex_uring_tick;
    sqe->len = 1;
    sqe->user_data = DEX_UD_TIMEOUT;
}

static void dex_uring_close(DexHttpConn* conn) {
    DexUringRing* r = dex_uring_ring_of(conn);
    struct io_uring_sqe* sqe = dex_uring_sqe(&r->u);
    dex_uring_idle(conn, 0);
    if (conn->direct) {
        if (sqe) {
            sqe->opcode = IORING_OP_CLOSE;
            sqe->file_index = (unsigned)conn->fd + 1;
            sqe->user_data = DEX_UD_IGNORE;
        }
        if (conn->fd >= 0 && conn->fd < r->direct_cap) r->direct[conn->fd] = NULL;
        r->direct_live--;
    } else {
        if (sqe) {
            sqe->opcode = IORING_OP_CLOSE;
            sqe->fd = conn->fd;
            sqe->user_data = DEX_UD_IGNORE;
        } else {
            close(conn->fd);
        }
        dex_uring_real_live--;
    }
    dex_http_conn_free(conn);
}

static void dex_uring_shutdown(DexHttpConn* conn) {
#ifdef IORING_RSRC_REGISTER_SPARSE
    struct io_uring_sqe* sqe = dex_uring_sqe(&dex_uring_ring_of(conn)->u);
    if (sqe) {
        sqe->opcode = IORING_OP_SHUTDOWN;
        dex_uring_target(sqe, conn);
        sqe->len = SHUT_RDWR;
        sqe->user_data = DEX_UD_IGNORE;
        return;
    }
#endif
    if (!conn->direct) shutdown(conn->fd, SHUT_RDWR);
}

/* Queue the next read. With a buffer ring the kernel picks the buffer when
 * data arrives; without one, or for the one read after the ring ran dry
 * (plain_recv), it reads into the connection's own buffer. */
static void dex_uring_queue_recv_ex(DexHttpConn* conn, int poll_first) {
    DexUringRing* r = dex_uring_ring_of(conn);
    if (!r->br || conn->plain_recv) {
        if (r->br) {
            /* The ring was out of buffers: one read into memory of our own. */
            if (dex_uring_reserve(conn, DEX_URING_BUF_BYTES) <= 0) { dex_uring_close(conn); return; }
        } else if (!conn->read_buf) {
            conn->read_buf = dex_uring_rbuf_take();
            if (!conn->read_buf) { dex_uring_close(conn); return; }
            conn->read_cap = DEX_URING_RBUF_BYTES;
        }
    }
    struct io_uring_sqe* sqe = dex_uring_sqe(&r->u);
    if (!sqe) { dex_uring_close(conn); return; }
    sqe->opcode = IORING_OP_RECV;
    dex_uring_target(sqe, conn);
    if (r->br && !conn->plain_recv) {
        sqe->flags |= IOSQE_BUFFER_SELECT;
        sqe->buf_group = 0;
        sqe->len = DEX_URING_BUF_BYTES;
    } else {
        sqe->addr = (unsigned long)(conn->read_buf + conn->read_len);
        sqe->len = (unsigned)(conn->read_cap - conn->read_len - 1);
    }
#ifdef IORING_RECVSEND_POLL_FIRST
    /* Right after a response the socket is almost always empty: wait for
     * data instead of trying a read that would fail first. */
    if (poll_first) sqe->ioprio |= IORING_RECVSEND_POLL_FIRST;
#else
    (void)poll_first;
#endif
    sqe->user_data = (unsigned long)conn | DEX_OP_RECV;
}

static void dex_uring_queue_recv(DexHttpConn* conn) { dex_uring_queue_recv_ex(conn, 0); }

static void dex_uring_queue_send(DexHttpConn* conn) {
    struct io_uring_sqe* sqe = dex_uring_sqe(&dex_uring_ring_of(conn)->u);
    if (!sqe) { dex_uring_close(conn); return; }
    sqe->opcode = IORING_OP_SEND;
    dex_uring_target(sqe, conn);
    sqe->addr = (unsigned long)(conn->write_buf + conn->write_pos);
    sqe->len = (unsigned)(conn->write_len - conn->write_pos);
    sqe->msg_flags = MSG_NOSIGNAL;
    sqe->user_data = (unsigned long)conn | DEX_OP_SEND;
}

static void dex_uring_fail(DexHttpConn* conn, const char* status, const char* body) {
    conn->keep_alive = 0;
    conn->state = HTTP_WRITING;
    dex_uring_idle(conn, 0);
    dex_http_release_write_buf(conn);
    conn->write_pos = 0;
    dex_http_build_response(conn, status, body, "application/json");
    if (!conn->write_buf || conn->write_len <= 0) { dex_uring_close(conn); return; }
    dex_uring_queue_send(conn);
}

static void dex_uring_need_more(DexHttpConn* conn) {
    int ceiling = DEX_HTTP_MAX_HEADER_BYTES;
    if (conn->state == HTTP_READING_BODY) ceiling = conn->headers_end + conn->content_length;
    if (conn->read_len >= ceiling) {
        if (conn->state == HTTP_READING_HEADERS) {
            dex_uring_fail(conn, "431 Request Header Fields Too Large", "{\"error\": \"Request Header Fields Too Large\"}");
        } else {
            dex_uring_fail(conn, "400 Bad Request", "{\"error\": \"Bad Request\"}");
        }
        return;
    }
    if (dex_uring_ring_of(conn)->br) {
        /* Room is made as bytes arrive (dex_uring_reserve). */
        dex_uring_queue_recv(conn);
        return;
    }
    if (conn->read_len >= conn->read_cap - 1) {
        int new_cap = conn->read_cap * 2;
        if (new_cap > ceiling + 1) new_cap = ceiling + 1;
        char* new_buf = (char*)dex_xrealloc(conn->read_buf, (size_t)new_cap);
        if (!new_buf) { dex_uring_close(conn); return; }
        conn->read_buf = new_buf;
        conn->read_cap = new_cap;
    }
    if (conn->read_cap - conn->read_len - 1 <= 0) {
        dex_uring_fail(conn, "400 Bad Request", "{\"error\": \"Bad Request\"}");
        return;
    }
    dex_uring_queue_recv(conn);
}

static void dex_uring_handled(DexHttpConn* conn);
static void dex_uring_park(DexHttpConn* conn);

/* res bytes have been appended to conn->read_buf (or res <= 0: the read
 * failed or the client closed). */
static void dex_uring_on_recv(DexHttpConn* conn, int res) {
    if (res <= 0 || conn->state == HTTP_DONE) { dex_uring_close(conn); return; }
    conn->read_len += res;
    conn->read_buf[conn->read_len] = '\0';
    conn->last_active = time(NULL);
    dex_uring_idle(conn, (uint32_t)conn->last_active);

    if (conn->state == HTTP_READING_HEADERS) {
        int parsed = dex_http_try_parse_headers(conn);
        if (parsed == -1) { dex_uring_fail(conn, "400 Bad Request", "{\"error\": \"Bad Request\"}"); return; }
        if (parsed == -2) { dex_uring_fail(conn, "413 Payload Too Large", "{\"error\": \"Payload Too Large\"}"); return; }
        if (parsed == 1) conn->state = HTTP_READING_BODY;
    }
    if (conn->state == HTTP_READING_BODY && conn->read_len - conn->headers_end >= conn->content_length) {
        dex_http_run_handler(conn);
        if (conn->later) { dex_uring_park(conn); return; }
        dex_uring_handled(conn);
        return;
    }
    dex_uring_need_more(conn);
}

/* A handler has answered conn's request. */
static void dex_uring_handled(DexHttpConn* conn) {
    conn->later_since = 0;
    dex_uring_idle(conn, 0);
    if (conn->relay_slot) {
        dex_http_release_write_buf(conn);
        conn->write_len = 0;
        conn->write_pos = 0;
        return;
    }
    if (!conn->write_buf || conn->write_len <= 0) { dex_uring_close(conn); return; }
    if (dex_http_after_batch) {
        /* Held until the batch hook has run: see dex_uring_finish_pass. */
        conn->next = dex_uring_pass_head;
        dex_uring_pass_head = conn;
        dex_uring_pass_count++;
        return;
    }
    dex_uring_queue_send(conn);
}

/* http.later: requests whose handlers asked to run again, in arrival order.
 * Each pass of the loop runs them once more, after its new requests and
 * before the batch hook, until they answer or have waited
 * DEX_HTTP_LATER_MAX_MS, after which http.later refuses and the handler
 * answers as it would without it. */
#define DEX_HTTP_LATER_MAX_MS 1000
#define DEX_HTTP_LATER_WAIT_US 200
static DexHttpConn* dex_uring_later_head = NULL;
static DexHttpConn* dex_uring_later_tail = NULL;

static void dex_uring_park(DexHttpConn* conn) {
    if (!conn->later_since) {
        uint64_t now = dex_uring_now_ms();
        conn->later_since = now ? now : 1;
    }
    dex_uring_idle(conn, 0);
    conn->later_next = NULL;
    if (dex_uring_later_tail) dex_uring_later_tail->later_next = conn;
    else dex_uring_later_head = conn;
    dex_uring_later_tail = conn;
}

static void dex_uring_run_later(void) {
    DexHttpConn* c = dex_uring_later_head;
    dex_uring_later_head = dex_uring_later_tail = NULL;
    while (c) {
        DexHttpConn* next = c->later_next;
        c->later_next = NULL;
        c->later = 0;
        dex_http_run_handler(c);
        if (c->later) dex_uring_park(c);
        else dex_uring_handled(c);
        c = next;
    }
}

/* A receive completed. With a buffer ring the bytes are in one of the ring's
 * buffers, which goes back to the ring as soon as they are copied out. */
static void dex_uring_on_recv_cqe(DexUringRing* r, DexHttpConn* conn, int res, unsigned cflags) {
#ifdef DEX_URING_HAVE_PBUF
    if (r->br && (cflags & IORING_CQE_F_BUFFER)) {
        unsigned bid = cflags >> IORING_CQE_BUFFER_SHIFT;
        const char* src = r->bufs + (size_t)bid * DEX_URING_BUF_BYTES;
        if (res <= 0 || conn->state == HTTP_DONE) {
            dex_uring_buf_put(r, bid);
            dex_uring_buf_publish(r);
            dex_uring_on_recv(conn, res <= 0 ? res : 0);
            return;
        }
        int room = dex_uring_reserve(conn, res);
        if (room > 0) memcpy(conn->read_buf + conn->read_len, src, (size_t)room);
        dex_uring_buf_put(r, bid);
        dex_uring_buf_publish(r);
        if (room < 0) { dex_uring_close(conn); return; }
        if (room == 0) { dex_uring_need_more(conn); return; }   /* at the ceiling: 431 or 400 */
        dex_uring_on_recv(conn, room);
        return;
    }
#endif
    (void)cflags;
    if (r->br && !conn->plain_recv) {
        if (res == -ENOBUFS && conn->state != HTTP_DONE) {
            conn->plain_recv = 1;
            dex_uring_queue_recv(conn);
            return;
        }
        /* EOF or an error: no buffer was used. */
        dex_uring_on_recv(conn, res > 0 ? 0 : res);
        return;
    }
    conn->plain_recv = 0;
    dex_uring_on_recv(conn, res);
}

static void dex_uring_on_send(DexHttpConn* conn, int res) {
    if (res <= 0) { dex_uring_close(conn); return; }
    conn->write_pos += res;
    if (conn->write_pos < conn->write_len) { dex_uring_queue_send(conn); return; }
    if (conn->keep_alive) {
        dex_http_conn_reset(conn);
        if (dex_uring_ring_of(conn)->br) dex_uring_rbuf_release(conn);
        dex_uring_idle(conn, (uint32_t)conn->last_active);
        dex_uring_queue_recv_ex(conn, 1);
    } else {
        dex_uring_close(conn);
    }
}

/* Every response a pass of the loop produced waits here until the program's
 * batch hook (http.afterBatch) has run, so a transaction the handlers shared
 * is committed before any of them is answered. If the hook reports failure,
 * every request in the batch is answered 500 instead. */
static void dex_uring_finish_pass(void) {
    DexHttpConn* c = dex_uring_pass_head;
    if (!c) return;
    if (dex_http_batch_window_ms > 0 && dex_uring_pass_count < DEX_URING_BATCH_MAX) {
        uint64_t now = dex_uring_now_ms();
        if (!dex_uring_batch_opened) dex_uring_batch_opened = now ? now : 1;
        uint64_t age = now - dex_uring_batch_opened;
        if (age < (uint64_t)dex_http_batch_window_ms) {
            if (!dex_uring_window_armed) dex_uring_queue_window((uint64_t)dex_http_batch_window_ms - age);
            return;
        }
    }
    dex_uring_batch_opened = 0;
    dex_uring_pass_count = 0;
    dex_uring_pass_head = NULL;
    _Bool ok = dex_http_run_batch_hook();
    while (c) {
        DexHttpConn* next = c->next;
        c->next = NULL;
        if (!ok) dex_http_answer_batch_failed(c);
        if (!c->write_buf || c->write_len <= 0) dex_uring_close(c);
        else dex_uring_queue_send(c);
        c = next;
    }
}

static void dex_lane_arm_recv(int lane) {
    DexLaneLink* L = &dex_lane_links[lane];
    if (L->dead || L->fd < 0 || L->recv_inflight) return;
    if (L->in_cap - L->in_len < 65536 && dex_lane_grow(&L->in, &L->in_cap, L->in_len + 65536) < 0) return;
    struct io_uring_sqe* sqe = dex_uring_sqe(&dex_urings[0].u);
    if (!sqe) return;
    sqe->opcode = IORING_OP_RECV;
    sqe->fd = L->fd;
    sqe->addr = (unsigned long)(L->in + L->in_len);
    sqe->len = (unsigned)(L->in_cap - L->in_len);
    sqe->user_data = DEX_UD_LANE_RECV + 16 * (unsigned long)lane;
    L->recv_inflight = 1;
}

static void dex_lane_send_next(int lane) {
    DexLaneLink* L = &dex_lane_links[lane];
    if (L->dead || L->fd < 0 || L->send_inflight) return;
    if (L->send_pos >= L->send_len) {
        if (!L->out_len) return;
        char* b = L->sending; size_t cap = L->send_cap;
        L->sending = L->out; L->send_cap = L->out_cap; L->send_len = L->out_len; L->send_pos = 0;
        L->out = b; L->out_cap = cap; L->out_len = 0;
    }
    struct io_uring_sqe* sqe = dex_uring_sqe(&dex_urings[0].u);
    if (!sqe) return;
    sqe->opcode = IORING_OP_SEND;
    sqe->fd = L->fd;
    sqe->addr = (unsigned long)(L->sending + L->send_pos);
    sqe->len = (unsigned)(L->send_len - L->send_pos);
    sqe->msg_flags = MSG_NOSIGNAL;
    sqe->user_data = DEX_UD_LANE_SEND + 16 * (unsigned long)lane;
    L->send_inflight = 1;
}

static void dex_lane_answer_conn(DexHttpConn* conn, int status, const char* ct, const char* body, int len) {
    dex_lane_forget(conn);
    conn->write_pos = 0;
    dex_http_build_response_full(conn, dex_http_status_text(status), body, len, ct, NULL);
    if (!conn->write_buf || conn->write_len <= 0) { dex_uring_close(conn); return; }
    dex_uring_queue_send(conn);
}

static void dex_lane_fail(int lane) {
    DexLaneLink* L = &dex_lane_links[lane];
    L->dead = 1;
    L->out_len = 0;
    L->send_len = L->send_pos = 0;
    for (int i = 0; i < dex_lane_slots_len; i++) {
        DexHttpConn* conn = dex_lane_slots[i].conn;
        if (!conn || dex_lane_slots[i].lane != lane) continue;
        dex_lane_answer_conn(conn, 503, "application/json", "{\"error\":\"service unavailable\"}", 31);
    }
}

static void dex_lane_on_send(int lane, int res) {
    if (lane < 0 || lane >= dex_lane_count) return;
    DexLaneLink* L = &dex_lane_links[lane];
    L->send_inflight = 0;
    if (L->dead) return;
    if (res <= 0) { dex_lane_fail(lane); return; }
    L->send_pos += (size_t)res;
    dex_lane_send_next(lane);
}

static void dex_lane_on_recv(int lane, int res) {
    if (lane < 0 || lane >= dex_lane_count) return;
    DexLaneLink* L = &dex_lane_links[lane];
    L->recv_inflight = 0;
    if (L->dead) return;
    if (res <= 0) { dex_lane_fail(lane); return; }
    L->in_len += (size_t)res;
    size_t pos = 0;
    while (L->in_len - pos >= 8) {
        uint32_t flen = dex_lane_get32(L->in + pos);
        if (flen < 7 || flen > DEX_LANE_FRAME_MAX) { dex_lane_fail(lane); return; }
        if (L->in_len - pos < 4 + (size_t)flen) {
            if (dex_lane_grow(&L->in, &L->in_cap, pos + 4 + (size_t)flen) < 0) { dex_lane_fail(lane); return; }
            break;
        }
        const char* f = L->in + pos + 4;
        uint32_t id = dex_lane_get32(f);
        uint16_t status;
        memcpy(&status, f + 4, 2);
        size_t ctlen = (unsigned char)f[6];
        if (7 + ctlen > flen) { dex_lane_fail(lane); return; }
        int slot = (int)(id & ((1u << DEX_LANE_SLOT_BITS) - 1));
        uint32_t gen = id >> DEX_LANE_SLOT_BITS;
        pos += 4 + (size_t)flen;
        if (slot >= dex_lane_slots_len) continue;
        DexLaneSlot* s = &dex_lane_slots[slot];
        if (!s->conn || s->gen != gen || s->lane != lane) continue;
        char ct[256];
        if (ctlen) memcpy(ct, f + 7, ctlen);
        ct[ctlen] = '\0';
        dex_lane_answer_conn(s->conn, status, ctlen ? ct : "application/json", f + 7 + ctlen, (int)(flen - 7 - ctlen));
    }
    if (pos > 0) {
        memmove(L->in, L->in + pos, L->in_len - pos);
        L->in_len -= pos;
    }
    dex_lane_arm_recv(lane);
}

static void dex_lane_flush(void) {
    for (int i = 0; i < dex_lane_count; i++) {
        DexLaneLink* L = &dex_lane_links[i];
        if (L->fd < 0 || L->dead) continue;
        dex_lane_arm_recv(i);
        dex_lane_send_next(i);
    }
}

static _Bool dex_http_later_uring(void) {
    DexHttpConn* c = dex_http_running;
    if (!c || !dex_lane_uring_live || c->relay_slot || c->later) return 0;
    if (c->later_since && dex_uring_now_ms() - c->later_since >= DEX_HTTP_LATER_MAX_MS) return 0;
    c->later = 1;
    return 1;
}

static _Bool dex_lane_relay_uring(int lane, DexString* payload) {
    DexHttpConn* c = dex_http_running;
    if (!c || !dex_lane_uring_live || c->relay_slot) return 0;
    DexLaneLink* L = &dex_lane_links[lane];
    if (L->dead) return 0;
    if (L->fd < 0) {
        L->fd = dex_lane_connect(lane);
        if (L->fd < 0) { L->dead = 1; return 0; }
    }
    size_t plen = payload ? payload->len : 0;
    if (plen + 4 > DEX_LANE_FRAME_MAX) return 0;
    if (dex_lane_grow(&L->out, &L->out_cap, L->out_len + 8 + plen) < 0) return 0;
    int slot = dex_lane_slot_take(c, lane);
    if (slot < 0) return 0;
    uint32_t id = (dex_lane_slots[slot].gen << DEX_LANE_SLOT_BITS) | (uint32_t)slot;
    char* w = L->out + L->out_len;
    dex_lane_put32(w, (uint32_t)(plen + 4));
    dex_lane_put32(w + 4, id);
    if (plen) memcpy(w + 8, payload->data, plen);
    L->out_len += 8 + plen;
    dex_http_running = NULL;
    return 1;
}

static void dex_uring_sweep_idle(uint32_t* idle, DexHttpConn** table, int n, uint32_t now) {
    for (int i = 0; i < n; i++) {
        uint32_t since = idle[i];
        if (!since || now - since < (uint32_t)dex_http_idle_secs) continue;
        idle[i] = 0;
        DexHttpConn* c = table[i];
        if (!c) continue;
        if (c->state != HTTP_READING_HEADERS && c->state != HTTP_READING_BODY) continue;
        c->state = HTTP_DONE;
        dex_uring_shutdown(c);
    }
}

static void dex_uring_sweep(void) {
    uint32_t now = dex_uring_now();
    dex_uring_sweep_idle(dex_uring_real_idle, dex_http_conn_table, DEX_HTTP_MAX_FD, now);
    for (int i = 0; i < dex_uring_count; i++) {
        DexUringRing* r = &dex_urings[i];
        if (r->direct_cap > 0) dex_uring_sweep_idle(r->direct_idle, r->direct, r->direct_cap, now);
    }
}

/* Handle every completion that is waiting on one ring. Returns how many. */
static unsigned dex_uring_reap(DexUringRing* r) {
    unsigned reaped = 0;
    unsigned head = *r->u.cq_head;
    unsigned tail = __atomic_load_n(r->u.cq_tail, __ATOMIC_ACQUIRE);
    while (head != tail) {
        struct io_uring_cqe* cqe = &r->u.cqes[head & *r->u.cq_mask];
        unsigned long ud = (unsigned long)cqe->user_data;
        int res = cqe->res;
        unsigned cflags = cqe->flags;
        head++;
        reaped++;
        if (ud < 256 && (ud & 0xF) == DEX_UD_ACCEPT) {
            r->accepts_pending--;
            r->listener_pending[ud >> 4]--;
            if (res >= 0) {
                DexHttpConn* conn = (res < DEX_HTTP_MAX_FD) ? dex_http_conn_new(res) : NULL;
                if (!conn) { close(res); continue; }
                conn->ring = (unsigned char)r->index;
                if (r->br) dex_uring_rbuf_release(conn);
                dex_uring_real_live++;
                dex_uring_idle(conn, (uint32_t)conn->last_active);
                dex_uring_queue_recv(conn);
            }
            continue;
        }
        if (ud < 256 && (ud & 0xF) == DEX_UD_ACCEPT_DIRECT) {
            r->accepts_pending--;
            r->listener_pending[ud >> 4]--;
            r->direct_pending--;
            if (res >= 0 && res < r->direct_cap) {
                DexHttpConn* conn = dex_http_conn_new(-1);
                if (!conn) {
                    struct io_uring_sqe* sqe = dex_uring_sqe(&r->u);
                    if (sqe) { sqe->opcode = IORING_OP_CLOSE; sqe->file_index = (unsigned)res + 1; sqe->user_data = DEX_UD_IGNORE; }
                    continue;
                }
                conn->fd = res;
                conn->direct = 1;
                conn->ring = (unsigned char)r->index;
                if (r->br) dex_uring_rbuf_release(conn);
                r->direct[res] = conn;
                r->direct_live++;
                dex_uring_idle(conn, (uint32_t)conn->last_active);
                dex_uring_queue_recv(conn);
            } else if (res == -ENFILE) {
                r->direct_slots = r->direct_live;
            }
            continue;
        }
        if (ud == DEX_UD_TIMEOUT) {
            dex_uring_sweep();
            dex_uring_queue_timeout();
            continue;
        }
        if (ud == DEX_UD_IGNORE) continue;
        if (ud == DEX_UD_WINDOW) { dex_uring_window_armed = 0; continue; }
        if (ud < 256 && (ud & 0xF) == DEX_UD_LANE_SEND) { dex_lane_on_send((int)(ud >> 4), res); continue; }
        if (ud < 256 && (ud & 0xF) == DEX_UD_LANE_RECV) { dex_lane_on_recv((int)(ud >> 4), res); continue; }
        DexHttpConn* conn = (DexHttpConn*)(ud & ~0xFUL);
        if ((ud & 0xF) == DEX_OP_RECV) dex_uring_on_recv_cqe(r, conn, res, cflags);
        else if ((ud & 0xF) == DEX_OP_SEND) dex_uring_on_send(conn, res);
    }
    __atomic_store_n(r->u.cq_head, head, __ATOMIC_RELEASE);
    return reaped;
}

static int dex_uring_has_cqes(DexUringRing* r) {
    return *r->u.cq_head != __atomic_load_n(r->u.cq_tail, __ATOMIC_ACQUIRE);
}

/* Undo rings 1..n-1, leaving the first. */
static void dex_uring_drop_extra_rings(void) {
    for (int i = 1; i < dex_uring_count; i++) {
        DexUringRing* r = &dex_urings[i];
        if (r->efd >= 0) close(r->efd);
        dex_uring_unmap(&r->u);
        free(r->direct);
        free(r->direct_idle);
#ifdef DEX_URING_HAVE_PBUF
        if (r->br) {
            munmap(r->br, DEX_URING_BUFS * sizeof(struct io_uring_buf));
            munmap(r->bufs, (size_t)DEX_URING_BUFS * DEX_URING_BUF_BYTES);
        }
#endif
        memset(r, 0, sizeof(*r));
    }
    dex_uring_count = 1;
    if (dex_uring_epfd >= 0) { close(dex_uring_epfd); dex_uring_epfd = -1; }
}

/* Several rings on one thread need one place to sleep: each ring signals an
 * eventfd when it has completions, and the thread waits on all of them in
 * epoll. Returns -1 if any part of that is unavailable, and the caller goes
 * back to one ring. */
static int dex_uring_link_rings(void) {
    dex_uring_epfd = epoll_create1(EPOLL_CLOEXEC);
    if (dex_uring_epfd < 0) return -1;
    for (int i = 0; i < dex_uring_count; i++) {
        DexUringRing* r = &dex_urings[i];
        r->efd = eventfd(0, EFD_NONBLOCK | EFD_CLOEXEC);
        if (r->efd < 0) return -1;
        if (syscall(__NR_io_uring_register, r->u.fd, IORING_REGISTER_EVENTFD, &r->efd, 1) != 0) return -1;
        struct epoll_event ev;
        memset(&ev, 0, sizeof(ev));
        ev.events = EPOLLIN | EPOLLET;
        ev.data.u32 = (unsigned)i;
        if (epoll_ctl(dex_uring_epfd, EPOLL_CTL_ADD, r->efd, &ev) != 0) return -1;
    }
    return 0;
}

static int dex_http_uring_run(int server_fd, int port) {
    (void)server_fd;
    unsigned slots = dex_uring_direct_limit();
    int real_cap = dex_http_conn_cap;

    /* How many rings the connection limit asks for. */
    int want = 1;
    if (dex_http_max_conns > real_cap + (int)slots && slots > 0) {
        want = (dex_http_max_conns - real_cap + (int)slots - 1) / (int)slots;
        if (want > DEX_URING_MAX_RINGS) want = DEX_URING_MAX_RINGS;
        if (want < 1) want = 1;
    }
    int defer = want == 1 || dex_uring_kernel_at_least(6, 2);

    memset(dex_urings, 0, sizeof(dex_urings));
    for (int i = 0; i < DEX_URING_MAX_RINGS; i++) { dex_urings[i].u.fd = -1; dex_urings[i].efd = -1; }
    if (dex_uring_setup(&dex_urings[0].u, defer) != 0) {
        fprintf(stderr, "[http] io_uring unavailable (%s); using epoll\n", strerror(errno));
        return -1;
    }
    dex_uring_count = 1;
    for (int i = 1; i < want; i++) {
        if (dex_uring_setup(&dex_urings[i].u, defer) != 0) break;
        dex_uring_count++;
    }
    for (int i = 0; i < dex_uring_count; i++) {
        dex_urings[i].index = i;
        dex_uring_register_direct(&dex_urings[i], slots);
        dex_uring_setup_bufs(&dex_urings[i]);
    }
    /* An extra ring is only worth having if it holds connections; if any of
     * them could not register a table, go back to the one ring. */
    for (int i = 1; i < dex_uring_count; i++) {
        if (dex_urings[i].direct_cap == 0) { dex_uring_drop_extra_rings(); break; }
    }
    if (dex_uring_count > 1 && dex_uring_link_rings() != 0) {
        fprintf(stderr, "[http] could not link io_uring rings (%s); using one\n", strerror(errno));
        dex_uring_drop_extra_rings();
    }

    for (int k = 0; k < dex_http_listen_count; k++) {
        int flags = fcntl(dex_http_listen_fds[k], F_GETFL, 0);
        if (flags >= 0) fcntl(dex_http_listen_fds[k], F_SETFL, flags & ~O_NONBLOCK);
    }

    dex_uring_real_cap = real_cap;
    int direct_total = 0;
    for (int i = 0; i < dex_uring_count; i++) direct_total += dex_urings[i].direct_slots;
    dex_http_conn_cap = dex_uring_real_cap + direct_total;
    dex_http_shed_at = dex_http_shed_point(dex_http_conn_cap);

    printf("Dex server listening on port %d", port);
    for (int i = 0; i < dex_http_extra_port_count; i++) printf(", %d", dex_http_extra_ports[i]);
    if (dex_uring_count > 1) {
        printf(" (io_uring, %d direct slots across %d rings)\n", direct_total, dex_uring_count);
    } else {
        printf(" (io_uring, %d direct slots)\n", direct_total);
    }
    fflush(stdout);

    dex_uring_queue_timeout();
    dex_lane_uring_live = 1;
    struct epoll_event evs[DEX_URING_MAX_RINGS];
    unsigned last_reaped = 0;
    for (;;) {
        for (int i = 0; i < dex_uring_count; i++) dex_uring_arm_accepts(&dex_urings[i]);
        /* Busy (the last pass had work): with http.gather set, sleep until a
         * batch has arrived on the first ring or the time is up, so one pass,
         * one commit and one round of submissions serve many requests rather
         * than waking for each. The other rings carry about as much, so they
         * are entered without waiting afterwards. */
        int gathered = 0;
        if (dex_http_gather_n && last_reaped >= 4) {
            for (int i = 1; i < dex_uring_count; i++) {
                if (dex_urings[i].u.local_tail != __atomic_load_n(dex_urings[i].u.sq_head, __ATOMIC_ACQUIRE)) dex_uring_enter(&dex_urings[i].u, 0, 0);
            }
            unsigned want = dex_http_gather_n / (unsigned)dex_uring_count;
            if (want < 1) want = 1;
            if (dex_uring_enter_timed(&dex_urings[0].u, want, dex_http_gather_us) == 0) {
                gathered = 1;
                for (int i = 1; i < dex_uring_count; i++) {
                    if (dex_uring_wants_enter(&dex_urings[i].u)) dex_uring_enter(&dex_urings[i].u, 0, 1);
                }
            }
        }
        if (gathered) {
        } else if (dex_uring_later_head) {
            /* Parked requests run again next pass: wait briefly, not until
             * the next event, which may be a second away when idle. */
            for (int i = 1; i < dex_uring_count; i++) dex_uring_enter(&dex_urings[i].u, 0, 1);
            if (dex_uring_enter_timed(&dex_urings[0].u, 1, DEX_HTTP_LATER_WAIT_US) != 0) {
                struct timespec ts = { 0, DEX_HTTP_LATER_WAIT_US * 1000 };
                nanosleep(&ts, NULL);
                dex_uring_enter(&dex_urings[0].u, 0, 1);
            }
        } else if (dex_uring_count == 1) {
            if (dex_uring_enter(&dex_urings[0].u, 1, 1) < 0 && errno != EBUSY && errno != EAGAIN) {
                perror("io_uring_enter");
                return 0;
            }
        } else {
            int ready = 0;
            for (int i = 0; i < dex_uring_count; i++) {
                if (!dex_uring_wants_enter(&dex_urings[i].u)) {
                    if (dex_uring_has_cqes(&dex_urings[i])) ready = 1;
                    continue;
                }
                if (dex_uring_enter(&dex_urings[i].u, 0, 1) < 0 && errno != EBUSY && errno != EAGAIN) {
                    perror("io_uring_enter");
                    return 0;
                }
                if (dex_uring_has_cqes(&dex_urings[i])) ready = 1;
            }
            if (!ready) {
                /* Nothing to do on any ring: sleep until one signals. The
                 * eventfds are edge-triggered and never drained; each signal
                 * is one edge, and an edge that arrives while the loop is busy
                 * makes this return at once next time, never a lost wakeup. */
                int n = epoll_wait(dex_uring_epfd, evs, DEX_URING_MAX_RINGS, 1000);
                if (n < 0 && errno != EINTR) { perror("epoll_wait"); return 0; }
                unsigned signalled = 0;
                for (int k = 0; k < n; k++) signalled |= 1u << evs[k].data.u32;
                for (int i = 0; i < dex_uring_count; i++) {
                    if ((signalled & (1u << i)) || dex_uring_wants_enter(&dex_urings[i].u)) dex_uring_enter(&dex_urings[i].u, 0, 1);
                }
            }
        }
        last_reaped = 0;
        for (int i = 0; i < dex_uring_count; i++) last_reaped += dex_uring_reap(&dex_urings[i]);
        if (dex_uring_later_head) dex_uring_run_later();
        dex_uring_finish_pass();
        dex_lane_flush();
    }
}

#endif

_Bool dex_http_later(void) {
#if defined(DEX_HTTP_HAVE_URING) && defined(__linux__)
    return dex_http_later_uring();
#else
    return 0;
#endif
}

_Bool dex_http_relay(int lane, DexString* payload) {
#if defined(DEX_HTTP_HAVE_URING) && defined(__linux__)
    if (lane < 0 || lane >= dex_lane_count) return 0;
    return dex_lane_relay_uring(lane, payload);
#else
    (void)lane; (void)payload;
    return 0;
#endif
}

static volatile int dex_server_fd = -1;

#define DEX_MAX_WORKERS 1024
#define DEX_AUTO_WORKERS 16
static pid_t dex_worker_pids[DEX_MAX_WORKERS];
static int   dex_worker_count = 0;

/* Set only by the forked multi-worker model — see the note in dex_listen(). */
static int dex_http_share_port = 0;


void dex_http_extra_port(int port) {
    if (port <= 0 || port > 65535) return;
    for (int i = 0; i < dex_http_extra_port_count; i++) {
        if (dex_http_extra_ports[i] == port) return;
    }
    if (dex_http_extra_port_count >= DEX_HTTP_MAX_LISTENERS - 1) {
        fprintf(stderr, "[http] at most %d extra ports\n", DEX_HTTP_MAX_LISTENERS - 1);
        return;
    }
    dex_http_extra_ports[dex_http_extra_port_count++] = port;
}

static void* dex_http_listener_tag(int k) {
    return k == 0 ? NULL : (void*)(intptr_t)(-16 - k);
}

static int dex_http_listener_index(void* tag) {
    if (tag == NULL) return 0;
    intptr_t v = (intptr_t)tag;
    if (v <= -17 && v > -16 - DEX_HTTP_MAX_LISTENERS) return (int)(-16 - v);
    return -1;
}

static int dex_http_bind_listener(const char* host, int port) {
    int fd = socket(AF_INET, SOCK_STREAM, 0);
    if (fd < 0) { perror("socket"); return -1; }
    int opt = 1;
    setsockopt(fd, SOL_SOCKET, SO_REUSEADDR, &opt, sizeof(opt));
#ifdef SO_REUSEPORT
    /* Only the forked worker model needs several processes on one port. Setting
     * this unconditionally means a second single-worker instance binds silently
     * alongside the first — including alongside one that is wedged or exiting —
     * and the kernel then load-balances between them, so a share of requests
     * vanish into a process that will never answer. Without it, the second
     * instance fails loudly with EADDRINUSE, which is the truth. */
    if (dex_http_share_port) {
        setsockopt(fd, SOL_SOCKET, SO_REUSEPORT, &opt, sizeof(opt));
    }
#endif
    struct sockaddr_in addr;
    memset(&addr, 0, sizeof(addr));
    addr.sin_family = AF_INET;
    addr.sin_port = htons(port);
    if (!host || !*host || strcmp(host, "0.0.0.0") == 0) {
        addr.sin_addr.s_addr = INADDR_ANY;
    } else if (inet_pton(AF_INET, host, &addr.sin_addr) != 1) {
        fprintf(stderr, "[http] not an IPv4 address to bind: %s\n", host);
        close(fd);
        return -1;
    }
    if (bind(fd, (struct sockaddr*)&addr, sizeof(addr)) < 0) {
        perror("bind"); close(fd); return -1;
    }
    if (listen(fd, SOMAXCONN) < 0) {
        perror("listen"); close(fd); return -1;
    }
    dex_set_nonblocking(fd);
    return fd;
}

static void dex_http_pause_accepting(void) {
    for (int k = 0; k < dex_http_listen_count; k++) dex_ev_del(dex_http_loop, dex_http_listen_fds[k]);
    dex_http_accepting = 0;
}

static void dex_http_resume_accepting(void) {
    for (int k = 0; k < dex_http_listen_count; k++) {
        dex_ev_add(dex_http_loop, dex_http_listen_fds[k], DEX_EV_READ, dex_http_listener_tag(k));
    }
    dex_http_accepting = 1;
}

static void dex_shutdown_handler(int sig) {
    (void)sig;
    for (int i = 0; i < dex_worker_count; i++) {
        if (dex_worker_pids[i] > 0) kill(dex_worker_pids[i], SIGTERM);
    }
    for (int k = 0; k < dex_http_listen_count; k++) close(dex_http_listen_fds[k]);
    if (dex_server_fd >= 0) close(dex_server_fd);
    /* This handler may have displaced the WebSocket listener's — they install
     * one each and the last to start wins. Exiting here would drop every
     * user without a close frame and without its disconnect handler running,
     * so the drain runs first and ends the process when it is done. An HTTP
     * request in flight is abandoned either way; they are short, and a user's
     * connection is not. */
    if (dex_shutdown_drain_hook) {
        dex_shutdown_drain_hook();
        return;
    }
    DEX_PF_FLUSH();
    _exit(0);
}

/* The event loop. host is the interface to bind: NULL or "" is every one, and
 * anything else must be a dotted-quad, which is refused rather than widened —
 * binding wider than asked is how a service meant for localhost ends up
 * answering the internet. */
static void dex_listen_on(const char* host, int port) {
    signal(SIGPIPE, SIG_IGN);
    signal(SIGINT, dex_shutdown_handler);
    signal(SIGTERM, dex_shutdown_handler);

    int server_fd = dex_http_bind_listener(host, port);
    if (server_fd < 0) return;
    dex_server_fd = server_fd;
    dex_http_listen_fds[0] = server_fd;
    dex_http_listen_count = 1;
    for (int i = 0; i < dex_http_extra_port_count; i++) {
        int fd = dex_http_bind_listener(host, dex_http_extra_ports[i]);
        if (fd < 0) {
            for (int k = 0; k < dex_http_listen_count; k++) close(dex_http_listen_fds[k]);
            dex_http_listen_count = 0;
            return;
        }
        dex_http_listen_fds[dex_http_listen_count++] = fd;
    }

    /* Initialize event loop, thread pool, and notify pipe */
    dex_http_loop = dex_ev_create(1024);
    if (!dex_http_inline) dex_http_pool = dex_pool_create(dex_http_thread_count);
    dex_http_conn_cap = dex_http_max_conns;
    if (dex_http_conn_cap > DEX_HTTP_MAX_FD - 64) dex_http_conn_cap = DEX_HTTP_MAX_FD - 64;
    struct rlimit rl;
    if (getrlimit(RLIMIT_NOFILE, &rl) == 0 && rl.rlim_cur != RLIM_INFINITY &&
        (rlim_t)dex_http_conn_cap + 64 > rl.rlim_cur) {
        dex_http_conn_cap = rl.rlim_cur > 128 ? (int)(rl.rlim_cur - 64) : 64;
    }
    dex_http_shed_at = dex_http_shed_point(dex_http_conn_cap);
    dex_notify_pipe_init(&dex_http_notify);
    memset(dex_http_conn_table, 0, sizeof(dex_http_conn_table));

#ifdef DEX_HTTP_HAVE_URING
    if (dex_http_uring_requested && dex_http_uring_run(server_fd, port) == 0) return;
#endif
    dex_http_resume_accepting();

    /* Register notify pipe for worker completions */
    dex_ev_add(dex_http_loop, dex_http_notify.read_fd, DEX_EV_READ, (void*)(intptr_t)-2);

    printf("Dex server listening on port %d", port);
    for (int i = 0; i < dex_http_extra_port_count; i++) printf(", %d", dex_http_extra_ports[i]);
    printf("\n");
    fflush(stdout);

    DexEvent events[256];

    /* A one-second wait rather than an indefinite one, so the idle sweep below
     * runs even on a listener nobody is talking to. That is the whole defence
     * against a client that opens a socket, sends a byte, and waits: without
     * it nothing ever reaps the connection. */
    time_t last_sweep = time(NULL);

    for (;;) {
        if (!dex_http_accepting && dex_http_live_conns < dex_http_conn_cap) {
            dex_http_resume_accepting();
        }
        /* The sweep runs before the wait, never between the wait and the
         * events it returned: a connection freed there could still have an
         * event in the batch, which would then read freed memory. */
        time_t now = time(NULL);
        if (now - last_sweep >= 1) {
            last_sweep = now;
            for (int fd = 0; fd < DEX_HTTP_MAX_FD; fd++) {
                DexHttpConn* c = dex_http_conn_table[fd];
                if (!c) continue;
                /* A dispatched connection is owned by a worker thread and a
                 * writing one is mid-response; neither is idle, and freeing
                 * either from here would pull it out from under its owner.
                 * A worker may be setting state as this reads it, hence the
                 * atomic load; nothing else is read until the state says the
                 * loop owns the connection. */
                DexHttpState st = __atomic_load_n(&c->state, __ATOMIC_RELAXED);
                if (st != HTTP_READING_HEADERS && st != HTTP_READING_BODY) continue;
                if (now - c->last_active < dex_http_idle_secs) continue;
                dex_ev_del(dex_http_loop, c->fd);
                close(c->fd);
                dex_http_conn_free(c);
            }
        }

        int n = dex_ev_wait(dex_http_loop, events, 256, 1000);

        for (int i = 0; i < n; i++) {
            DexEvent* ev = &events[i];

            int listener = dex_http_listener_index(ev->user_data);
            if (listener >= 0) {
                int lfd = dex_http_listen_fds[listener];
                for (;;) {
                    if (dex_http_live_conns >= dex_http_conn_cap) {
                        dex_http_pause_accepting();
                        break;
                    }
                    struct sockaddr_in client_addr;
                    socklen_t client_len = sizeof(client_addr);
#ifdef __linux__
                    int client_fd = accept4(lfd, (struct sockaddr*)&client_addr, &client_len, SOCK_NONBLOCK | SOCK_CLOEXEC);
                    if (client_fd < 0) break;
#else
                    int client_fd = accept(lfd, (struct sockaddr*)&client_addr, &client_len);
                    if (client_fd < 0) break;
                    dex_set_nonblocking(client_fd);
#endif
                    DexHttpConn* conn = dex_http_conn_new(client_fd);
                    if (!conn) { close(client_fd); continue; }
                    dex_ev_add(dex_http_loop, client_fd, DEX_EV_READ, conn);
                }
                continue;
            }

            /* Notify pipe: drain and process completed connections */
            if (ev->user_data == (void*)(intptr_t)-2) {
                dex_notify_pipe_drain(&dex_http_notify);

                /* Dequeue all completed connections */
                pthread_mutex_lock(&dex_http_completed_mutex);
                DexHttpConn* completed = dex_http_completed_head;
                dex_http_completed_head = NULL;
                pthread_mutex_unlock(&dex_http_completed_mutex);

                while (completed) {
                    DexHttpConn* next = completed->next;
                    completed->next = NULL;
                    /* Register fd for write */
                    dex_ev_add(dex_http_loop, completed->fd, DEX_EV_WRITE, completed);
                    completed = next;
                }
                continue;
            }

            /* Client connection */
            DexHttpConn* conn = (DexHttpConn*)ev->user_data;

            if (ev->events & DEX_EV_ERROR) {
                dex_ev_del(dex_http_loop, conn->fd);
                close(conn->fd);
                dex_http_conn_free(conn);
                continue;
            }

            /* Readable: accumulate request data */
            if ((ev->events & DEX_EV_READ) && (conn->state == HTTP_READING_HEADERS || conn->state == HTTP_READING_BODY)) {
                /* What this connection is still allowed to accumulate. While
                 * the headers are being read that is the header cap; once
                 * Content-Length is known it is the headers plus that body.
                 * Both are far below INT_MAX, so the doubling below cannot
                 * overflow the way an unbounded read_cap * 2 could. */
                int ceiling = DEX_HTTP_MAX_HEADER_BYTES;
                if (conn->state == HTTP_READING_BODY) {
                    ceiling = conn->headers_end + conn->content_length;
                }

                if (conn->read_len >= ceiling) {
                    if (conn->state == HTTP_READING_HEADERS) {
                        dex_http_fail_and_close(conn, "431 Request Header Fields Too Large",
                                                "{\"error\": \"Request Header Fields Too Large\"}");
                    } else {
                        dex_http_fail_and_close(conn, "400 Bad Request",
                                                "{\"error\": \"Bad Request\"}");
                    }
                    continue;
                }

                if (conn->read_len >= conn->read_cap - 1) {
                    int new_cap = conn->read_cap * 2;
                    if (new_cap > ceiling + 1) new_cap = ceiling + 1;
                    char* new_buf = (char*)dex_xrealloc(conn->read_buf, (size_t)new_cap);
                    if (!new_buf) {
                        dex_ev_del(dex_http_loop, conn->fd);
                        close(conn->fd);
                        dex_http_conn_free(conn);
                        continue;
                    }
                    conn->read_buf = new_buf;
                    conn->read_cap = new_cap;
                }

                int want = conn->read_cap - conn->read_len - 1;
                if (want <= 0) {
                    dex_http_fail_and_close(conn, "400 Bad Request",
                                            "{\"error\": \"Bad Request\"}");
                    continue;
                }
                int r = (int)read(conn->fd, conn->read_buf + conn->read_len, (size_t)want);
                if (r <= 0) {
                    /* Connection closed or error */
                    dex_ev_del(dex_http_loop, conn->fd);
                    close(conn->fd);
                    dex_http_conn_free(conn);
                    continue;
                }
                conn->read_len += r;
                conn->read_buf[conn->read_len] = '\0';
                conn->last_active = time(NULL);

                if (conn->state == HTTP_READING_HEADERS) {
                    int parsed = dex_http_try_parse_headers(conn);
                    if (parsed == -1) {
                        dex_http_fail_and_close(conn, "400 Bad Request",
                                                "{\"error\": \"Bad Request\"}");
                        continue;
                    }
                    if (parsed == -2) {
                        dex_http_fail_and_close(conn, "413 Payload Too Large",
                                                "{\"error\": \"Payload Too Large\"}");
                        continue;
                    }
                    if (parsed == 1) {
                        conn->state = HTTP_READING_BODY;
                    }
                }

                if (conn->state == HTTP_READING_BODY) {
                    int body_received = conn->read_len - conn->headers_end;
                    if (body_received >= conn->content_length) {
                        if (dex_http_inline) {
                            dex_http_run_handler(conn);
                            dex_http_after_one(conn);
                            dex_http_flush_inline(conn);
                            continue;
                        }
                        conn->state = HTTP_DISPATCHED;
                        dex_ev_del(dex_http_loop, conn->fd);
                        dex_pool_submit(dex_http_pool, dex_http_worker_func, conn);
                    }
                }
            }

            /* Writable: flush response */
            if ((ev->events & DEX_EV_WRITE) && conn->state == HTTP_WRITING) {
                int remaining = conn->write_len - conn->write_pos;
                if (remaining > 0) {
                    int w = (int)write(conn->fd, conn->write_buf + conn->write_pos, remaining);
                    if (w <= 0) {
                        dex_ev_del(dex_http_loop, conn->fd);
                        close(conn->fd);
                        dex_http_conn_free(conn);
                        continue;
                    }
                    conn->write_pos += w;
                }
                if (conn->write_pos >= conn->write_len) {
                    /* Response fully sent */
                    if (conn->keep_alive) {
                        dex_http_conn_reset(conn);
                        dex_ev_mod(dex_http_loop, conn->fd, DEX_EV_READ, conn);
                    } else {
                        dex_ev_del(dex_http_loop, conn->fd);
                        close(conn->fd);
                        dex_http_conn_free(conn);
                    }
                }
            }
        }
    }
}

/* ── Multi-process worker model via SO_REUSEPORT ─────────────────── */

static int dex_http_worker_index = -1;

int dex_http_fork(int count) {
    if (dex_http_worker_index >= 0) return dex_http_worker_index;
    if (count <= 0) count = (int)sysconf(_SC_NPROCESSORS_ONLN);
    if (count < 1) count = 1;
    if (count > DEX_MAX_WORKERS) count = DEX_MAX_WORKERS;
    dex_http_share_port = 1;
    dex_http_worker_index = 0;
    if (count == 1) return 0;
    signal(SIGPIPE, SIG_IGN);
    pid_t parent = getpid();
    fflush(stdout);
    fflush(stderr);
    for (int i = 1; i < count; i++) {
        pid_t pid = fork();
        if (pid < 0) {
            perror("fork");
            break;
        }
        if (pid == 0) {
            dex_worker_count = 0;
            dex_http_worker_index = i;
#ifdef __linux__
            prctl(PR_SET_PDEATHSIG, SIGTERM);
            if (getppid() != parent) _exit(0);
#endif
            return i;
        }
        dex_worker_pids[dex_worker_count++] = pid;
    }
    return 0;
}

static void dex_multi_shutdown_handler(int sig) {
    (void)sig;
    for (int i = 0; i < dex_worker_count; i++) {
        if (dex_worker_pids[i] > 0) {
            kill(dex_worker_pids[i], SIGTERM);
        }
    }
    if (dex_server_fd >= 0) close(dex_server_fd);
    DEX_PF_FLUSH();
    _exit(0);
}

static void dex_listen_multi_on(const char* host, int port, int num_workers) {
    if (num_workers <= 0) {
        num_workers = (int)sysconf(_SC_NPROCESSORS_ONLN);
        if (num_workers < 1) num_workers = 1;
        if (num_workers > DEX_AUTO_WORKERS) num_workers = DEX_AUTO_WORKERS;
    }
    if (num_workers == 1) {
        dex_listen_on(host, port);
        return;
    }
    if (num_workers > DEX_MAX_WORKERS) num_workers = DEX_MAX_WORKERS;

    signal(SIGPIPE, SIG_IGN);

    /* Every worker binds the same port, which is what SO_REUSEPORT is for. */
    dex_http_share_port = 1;

    /* Fork worker processes */
    dex_worker_count = num_workers - 1;
    for (int i = 0; i < num_workers - 1; i++) {
        pid_t pid = fork();
        if (pid < 0) {
            perror("fork");
            continue;
        }
        if (pid == 0) {
            /* Child: run event loop (SO_REUSEPORT allows multiple binds) */
            dex_worker_count = 0;
            dex_listen_on(host, port);
            _exit(0);
        }
        dex_worker_pids[i] = pid;
    }

    /* Parent: install graceful shutdown handler */
    signal(SIGINT, dex_multi_shutdown_handler);
    signal(SIGTERM, dex_multi_shutdown_handler);

    /* Parent becomes the last worker */
    dex_listen_on(host, port);
}

/* The three spellings the language exposes. http.listen(port[, workers]) binds
 * every interface, which is what a service with nothing in front of it wants;
 * http.listenOn(host, port[, workers]) names one, for a service behind a proxy
 * that should not also be reachable directly. */
void dex_listen(int port) {
    dex_listen_on(NULL, port);
}

void dex_listen_multi(int port, int num_workers) {
    dex_listen_multi_on(NULL, port, num_workers);
}

void dex_listen_host(const char* host, int port, int num_workers) {
    dex_listen_multi_on(host, port, num_workers);
}


// --- HTTP Client (libcurl) ---
// Dex_HttpResponse typedef is emitted by codegen from module Types.

typedef struct {
    char* data;
    size_t len;
} dex_http_buf;

static size_t dex_http_write_cb(void* contents, size_t size, size_t nmemb, void* userp) {
    size_t total = size * nmemb;
    dex_http_buf* buf = (dex_http_buf*)userp;
    char* ptr = dex_xrealloc(buf->data, buf->len + total + 1);
    if (!ptr) return 0;
    buf->data = ptr;
    memcpy(buf->data + buf->len, contents, total);
    buf->len += total;
    buf->data[buf->len] = '\0';
    return total;
}

// Response headers, kept as the newline-separated "Key: Value" pairs that
// http.header() builds and http.responseWith() consumes, so both directions of
// the client speak one format. The status line is not a header and is not kept;
// the status code is already its own field. Following a redirect produces one
// block per hop and only the last survives, so resp.headers always describes
// the response resp.statusCode came from.
static size_t dex_http_header_cb(char* buffer, size_t size, size_t nitems, void* userp) {
    size_t total = size * nitems;
    dex_http_buf* buf = (dex_http_buf*)userp;

    if (total >= 5 && strncmp(buffer, "HTTP/", 5) == 0) {
        buf->len = 0;
        if (buf->data) buf->data[0] = '\0';
        return total;
    }

    size_t n = total;
    while (n > 0 && (buffer[n - 1] == '\n' || buffer[n - 1] == '\r')) n--;
    if (n == 0) return total; // the blank line that closes the block

    char* ptr = dex_xrealloc(buf->data, buf->len + n + 2);
    if (!ptr) return 0;
    buf->data = ptr;
    memcpy(buf->data + buf->len, buffer, n);
    buf->len += n;
    buf->data[buf->len++] = '\n';
    buf->data[buf->len] = '\0';
    return total;
}

// Every string field of a response is a real DexString, including on the paths
// that fail before a request leaves. A NULL field reaches .dx code as a NULL
// DexString* and faults on the first read of it.
static Dex_HttpResponse dex_http_resp_failed(const char* reason) {
    Dex_HttpResponse resp;
    resp.statusCode = 0;
    resp.body = dex_string_from_lit(reason);
    resp.contentType = dex_string_from_lit("");
    resp.headers = dex_string_from_lit("");
    return resp;
}

#define DEX_HTTP_TIMEOUT_MS 30000
#define DEX_HTTP_CONNECT_TIMEOUT_MS 10000

// A request to a host the caller does not control needs a deadline, and
// curl_easy_perform has none of its own: without one, a target that accepts a
// connection and then says nothing holds the calling thread until the process
// restarts. NOSIGNAL is part of that — libcurl otherwise resolves with SIGALRM,
// which is not safe in a process with threads, and this one has them.
//
// Redirects are not followed unless asked for, which hands the 3xx and its
// Location to the caller. That is the right default for anything grading a
// redirect chain, and the safe one for anything that has to re-check where it
// is being sent before it goes.
static void dex_http_apply_opts(CURL* curl, const Dex_HttpOptions* o) {
    long timeout = DEX_HTTP_TIMEOUT_MS;
    long connect = DEX_HTTP_CONNECT_TIMEOUT_MS;
    long follow = 0;
    int insecure = 0;

    if (o) {
        if (o->timeoutMs > 0) timeout = (long)o->timeoutMs;
        if (o->connectTimeoutMs > 0) connect = (long)o->connectTimeoutMs;
        if (o->maxRedirects > 0) follow = (long)o->maxRedirects;
        insecure = o->insecureTls ? 1 : 0;
    }

    curl_easy_setopt(curl, CURLOPT_TIMEOUT_MS, timeout);
    curl_easy_setopt(curl, CURLOPT_CONNECTTIMEOUT_MS, connect);
    curl_easy_setopt(curl, CURLOPT_NOSIGNAL, 1L);

    if (follow > 0) {
        curl_easy_setopt(curl, CURLOPT_FOLLOWLOCATION, 1L);
        curl_easy_setopt(curl, CURLOPT_MAXREDIRS, follow);
    }
    if (insecure) {
        curl_easy_setopt(curl, CURLOPT_SSL_VERIFYPEER, 0L);
        curl_easy_setopt(curl, CURLOPT_SSL_VERIFYHOST, 0L);
    }
}

// --- Header builder ---
const char* dex_http_header(const char* headers, const char* key, const char* value) {
    size_t hlen = strlen(headers);
    size_t klen = strlen(key);
    size_t vlen = strlen(value);
    // "Key: Value\n"
    size_t newlen = hlen + klen + 2 + vlen + 1 + 1;
    char* result = (char*)dex_xmalloc(newlen);
    if (!result) return "";
    snprintf(result, newlen, "%s%s: %s\n", headers, key, value);
    return result;
}

// --- JSON header auto-detection ---
static struct curl_slist* dex_http_parse_json_headers(const char* json_str) {
    struct curl_slist* list = NULL;
    const char* p = json_str;
    while (*p) {
        // Find next quoted key
        const char* kq = strchr(p, '"');
        if (!kq) break;
        kq++;
        const char* kend = strchr(kq, '"');
        if (!kend) break;
        size_t klen = (size_t)(kend - kq);
        // Find colon then quoted value
        const char* colon = strchr(kend + 1, ':');
        if (!colon) break;
        const char* vq = strchr(colon + 1, '"');
        if (!vq) break;
        vq++;
        const char* vend = strchr(vq, '"');
        if (!vend) break;
        size_t vlen = (size_t)(vend - vq);
        // Build "Key: Value" header
        size_t hlen = klen + 2 + vlen + 1;
        char* h = (char*)dex_xmalloc(hlen);
        if (!h) { p = vend + 1; continue; }
        memcpy(h, kq, klen);
        h[klen] = ':';
        h[klen + 1] = ' ';
        memcpy(h + klen + 2, vq, vlen);
        h[hlen - 1] = '\0';
        list = curl_slist_append(list, h);
        free(h);
        p = vend + 1;
    }
    return list;
}

static Dex_HttpResponse dex_http_request_impl(const char* method, const char* url, const char* body, const char* headers, const Dex_HttpOptions* opts) {
    CURL* curl = curl_easy_init();
    if (!curl) return dex_http_resp_failed("could not initialise the http client");

    dex_http_buf buf = {NULL, 0};
    dex_http_buf hbuf = {NULL, 0};
    buf.data = dex_xmalloc(1);
    hbuf.data = dex_xmalloc(1);
    if (!buf.data || !hbuf.data) {
        free(buf.data);
        free(hbuf.data);
        curl_easy_cleanup(curl);
        return dex_http_resp_failed("out of memory");
    }
    buf.data[0] = '\0';
    hbuf.data[0] = '\0';

    curl_easy_setopt(curl, CURLOPT_URL, url);
    curl_easy_setopt(curl, CURLOPT_WRITEFUNCTION, dex_http_write_cb);
    curl_easy_setopt(curl, CURLOPT_WRITEDATA, &buf);
    curl_easy_setopt(curl, CURLOPT_HEADERFUNCTION, dex_http_header_cb);
    curl_easy_setopt(curl, CURLOPT_HEADERDATA, &hbuf);
    curl_easy_setopt(curl, CURLOPT_CUSTOMREQUEST, method);
    dex_http_apply_opts(curl, opts);

    if (body && body[0] != '\0') {
        curl_easy_setopt(curl, CURLOPT_POSTFIELDS, body);
    }

    struct curl_slist* hdr_list = NULL;
    if (headers && headers[0] != '\0') {
        // Auto-detect: JSON object vs newline-separated headers
        // Skip leading whitespace for detection
        const char* detect = headers;
        while (*detect == ' ' || *detect == '\t') detect++;
        if (*detect == '{') {
            // JSON format: parse key-value pairs
            hdr_list = dex_http_parse_json_headers(headers);
        } else {
            // Newline-separated "Key: Value\n" format
            const char* p = headers;
            while (*p) {
                const char* end = strchr(p, '\n');
                if (!end) end = p + strlen(p);
                size_t hlen = (size_t)(end - p);
                char* h = (char*)dex_xmalloc(hlen + 1);
                if (!h) { p = (*end) ? end + 1 : end; continue; }
                memcpy(h, p, hlen);
                h[hlen] = '\0';
                // Trim trailing \r
                if (hlen > 0 && h[hlen-1] == '\r') h[hlen-1] = '\0';
                if (h[0] != '\0') hdr_list = curl_slist_append(hdr_list, h);
                free(h);
                p = (*end) ? end + 1 : end;
            }
        }
    }
    if (hdr_list) curl_easy_setopt(curl, CURLOPT_HTTPHEADER, hdr_list);

    Dex_HttpResponse resp;
    CURLcode res = curl_easy_perform(curl);
    if (res == CURLE_OK) {
        long code;
        curl_easy_getinfo(curl, CURLINFO_RESPONSE_CODE, &code);
        resp.statusCode = (int)code;
        // from_cstr takes the buffer and frees it, so neither is freed here.
        resp.body = dex_string_from_cstr(buf.data);
        resp.headers = dex_string_from_cstr(hbuf.data);
        char* ct = NULL;
        curl_easy_getinfo(curl, CURLINFO_CONTENT_TYPE, &ct);
        resp.contentType = dex_string_from_lit(ct ? ct : "");
    } else {
        // A refused connection, a timeout and a certificate we would not accept
        // all arrive here. The reason goes in the body because a caller that
        // only prints the body still learns what happened.
        free(buf.data);
        free(hbuf.data);
        resp = dex_http_resp_failed(curl_easy_strerror(res));
    }

    if (hdr_list) curl_slist_free_all(hdr_list);
    curl_easy_cleanup(curl);
    return resp;
}

Dex_HttpResponse dex_http_get(const char* url) {
    return dex_http_request_impl("GET", url, NULL, NULL, NULL);
}

Dex_HttpResponse dex_http_get_h(const char* url, const char* headers) {
    return dex_http_request_impl("GET", url, NULL, headers, NULL);
}

Dex_HttpResponse dex_http_post(const char* url, const char* body) {
    return dex_http_request_impl("POST", url, body, "Content-Type: application/json", NULL);
}

Dex_HttpResponse dex_http_post_h(const char* url, const char* body, const char* headers) {
    return dex_http_request_impl("POST", url, body, headers, NULL);
}

Dex_HttpResponse dex_http_put(const char* url, const char* body) {
    return dex_http_request_impl("PUT", url, body, "Content-Type: application/json", NULL);
}

Dex_HttpResponse dex_http_put_h(const char* url, const char* body, const char* headers) {
    return dex_http_request_impl("PUT", url, body, headers, NULL);
}

Dex_HttpResponse dex_http_patch(const char* url, const char* body) {
    return dex_http_request_impl("PATCH", url, body, "Content-Type: application/json", NULL);
}

Dex_HttpResponse dex_http_patch_h(const char* url, const char* body, const char* headers) {
    return dex_http_request_impl("PATCH", url, body, headers, NULL);
}

Dex_HttpResponse dex_http_delete(const char* url) {
    return dex_http_request_impl("DELETE", url, NULL, NULL, NULL);
}

Dex_HttpResponse dex_http_delete_h(const char* url, const char* headers) {
    return dex_http_request_impl("DELETE", url, NULL, headers, NULL);
}

Dex_HttpResponse dex_http_request(const char* method, const char* url, const char* body, const char* headers) {
    return dex_http_request_impl(method, url, body, headers, NULL);
}

Dex_HttpResponse dex_http_request_opts(const char* method, const char* url, const char* body, const char* headers, Dex_HttpOptions opts) {
    return dex_http_request_impl(method, url, body, headers, &opts);
}

// --- Form data (multipart) ---
// Form string encoding: "F\tkey\tvalue\n" for fields, "P\tkey\tpath\n" for files

const char* dex_http_form_new(void) {
    return dex_xstrdup("");
}

const char* dex_http_form_field(const char* form, const char* key, const char* value) {
    size_t flen = strlen(form);
    size_t klen = strlen(key);
    size_t vlen = strlen(value);
    // "F\tkey\tvalue\n"
    size_t newlen = flen + 2 + klen + 1 + vlen + 1 + 1;
    char* result = (char*)dex_xmalloc(newlen);
    if (!result) return "";
    snprintf(result, newlen, "%sF\t%s\t%s\n", form, key, value);
    return result;
}

const char* dex_http_form_file(const char* form, const char* key, const char* path) {
    size_t flen = strlen(form);
    size_t klen = strlen(key);
    size_t plen = strlen(path);
    // "P\tkey\tpath\n"
    size_t newlen = flen + 2 + klen + 1 + plen + 1 + 1;
    char* result = (char*)dex_xmalloc(newlen);
    if (!result) return "";
    snprintf(result, newlen, "%sP\t%s\t%s\n", form, key, path);
    return result;
}

static Dex_HttpResponse dex_http_post_form_impl(const char* url, const char* form, const char* headers) {
    CURL* curl = curl_easy_init();
    if (!curl) return dex_http_resp_failed("could not initialise the http client");

    dex_http_buf buf = {NULL, 0};
    dex_http_buf hbuf = {NULL, 0};
    buf.data = dex_xmalloc(1);
    hbuf.data = dex_xmalloc(1);
    if (!buf.data || !hbuf.data) {
        free(buf.data);
        free(hbuf.data);
        curl_easy_cleanup(curl);
        return dex_http_resp_failed("out of memory");
    }
    buf.data[0] = '\0';
    hbuf.data[0] = '\0';

    curl_easy_setopt(curl, CURLOPT_URL, url);
    curl_easy_setopt(curl, CURLOPT_WRITEFUNCTION, dex_http_write_cb);
    curl_easy_setopt(curl, CURLOPT_WRITEDATA, &buf);
    curl_easy_setopt(curl, CURLOPT_HEADERFUNCTION, dex_http_header_cb);
    curl_easy_setopt(curl, CURLOPT_HEADERDATA, &hbuf);
    dex_http_apply_opts(curl, NULL);

    struct curl_slist* hdr_list = NULL;
    if (headers && headers[0] != '\0') {
        const char* detect = headers;
        while (*detect == ' ' || *detect == '\t') detect++;
        if (*detect == '{') {
            hdr_list = dex_http_parse_json_headers(headers);
        } else {
            const char* hp = headers;
            while (*hp) {
                const char* end = strchr(hp, '\n');
                if (!end) end = hp + strlen(hp);
                size_t hlen = (size_t)(end - hp);
                char* h = (char*)dex_xmalloc(hlen + 1);
                if (!h) { hp = (*end) ? end + 1 : end; continue; }
                memcpy(h, hp, hlen);
                h[hlen] = '\0';
                if (hlen > 0 && h[hlen-1] == '\r') h[hlen-1] = '\0';
                if (h[0] != '\0') hdr_list = curl_slist_append(hdr_list, h);
                free(h);
                hp = (*end) ? end + 1 : end;
            }
        }
    }
    if (hdr_list) curl_easy_setopt(curl, CURLOPT_HTTPHEADER, hdr_list);

    curl_mime* mime = curl_mime_init(curl);

    // Parse form string
    const char* p = form;
    while (*p) {
        const char* line_end = strchr(p, '\n');
        if (!line_end) line_end = p + strlen(p);

        if ((line_end - p) > 2 && (p[0] == 'F' || p[0] == 'P') && p[1] == '\t') {
            char type = p[0];
            const char* key_start = p + 2;
            const char* tab2 = memchr(key_start, '\t', (size_t)(line_end - key_start));
            if (tab2) {
                size_t klen = (size_t)(tab2 - key_start);
                size_t vlen = (size_t)(line_end - tab2 - 1);
                char* key = (char*)dex_xmalloc(klen + 1);
                if (!key) goto next_form_line;
                char* val = (char*)dex_xmalloc(vlen + 1);
                if (!val) { free(key); goto next_form_line; }
                memcpy(key, key_start, klen); key[klen] = '\0';
                memcpy(val, tab2 + 1, vlen); val[vlen] = '\0';

                curl_mimepart* part = curl_mime_addpart(mime);
                curl_mime_name(part, key);
                if (type == 'F') {
                    curl_mime_data(part, val, CURL_ZERO_TERMINATED);
                } else {
                    curl_mime_filedata(part, val);
                }
                free(key);
                free(val);
            }
        }
        next_form_line:
        p = (*line_end) ? line_end + 1 : line_end;
    }

    curl_easy_setopt(curl, CURLOPT_MIMEPOST, mime);

    Dex_HttpResponse resp;
    CURLcode res = curl_easy_perform(curl);
    if (res == CURLE_OK) {
        long code;
        curl_easy_getinfo(curl, CURLINFO_RESPONSE_CODE, &code);
        resp.statusCode = (int)code;
        resp.body = dex_string_from_cstr(buf.data);
        resp.headers = dex_string_from_cstr(hbuf.data);
        char* ct = NULL;
        curl_easy_getinfo(curl, CURLINFO_CONTENT_TYPE, &ct);
        resp.contentType = dex_string_from_lit(ct ? ct : "");
    } else {
        free(buf.data);
        free(hbuf.data);
        resp = dex_http_resp_failed(curl_easy_strerror(res));
    }

    if (hdr_list) curl_slist_free_all(hdr_list);
    curl_mime_free(mime);
    curl_easy_cleanup(curl);
    return resp;
}

Dex_HttpResponse dex_http_post_form(const char* url, const char* form) {
    return dex_http_post_form_impl(url, form, NULL);
}

Dex_HttpResponse dex_http_post_form_h(const char* url, const char* form, const char* headers) {
    return dex_http_post_form_impl(url, form, headers);
}
