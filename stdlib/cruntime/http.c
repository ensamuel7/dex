
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <errno.h>
#include <sys/socket.h>
#include <sys/uio.h>
#include <netinet/in.h>
#include <arpa/inet.h>
#include <unistd.h>
#include <pthread.h>
#include <signal.h>
#include <time.h>
#include <curl/curl.h>

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
            (dex_route_entry*)realloc(dex_routes, (size_t)grown * sizeof(dex_route_entry));
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
    char          path[2048];
    char          query[2048];
    int           content_length;
    int           headers_end;     /* offset of body start in read_buf */
    int           header_start;    /* offset of the first header line */
    int           keep_alive;

    /* Write buffer (response) */
    char*         write_buf;
    int           write_len;
    int           write_pos;       /* how much has been flushed */

    /* When this connection last made progress, for the idle sweep. */
    time_t        last_active;

    /* Linked list for completed queue */
    struct DexHttpConn* next;
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
    DexHttpConn* conn = (DexHttpConn*)calloc(1, sizeof(DexHttpConn));
    if (!conn) return NULL;
    conn->fd = fd;
    conn->state = HTTP_READING_HEADERS;
    conn->read_cap = 8192;
    conn->read_buf = (char*)malloc(conn->read_cap);
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
    dex_http_live_conns++;
    return conn;
}

static void dex_http_conn_free(DexHttpConn* conn) {
    if (!conn) return;
    if (conn->fd >= 0 && conn->fd < DEX_HTTP_MAX_FD) {
        dex_http_conn_table[conn->fd] = NULL;
    }
    dex_http_live_conns--;
    free(conn->read_buf);
    free(conn->write_buf);
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
    conn->path[0] = '\0';
    conn->query[0] = '\0';
    free(conn->write_buf);
    conn->write_buf = NULL;
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

    /* Parse method and path directly into conn buffers */
    sscanf(conn->read_buf, "%15s %2047s", conn->method, conn->path);

    /* Split path and query at '?' in-place */
    char* qmark = strchr(conn->path, '?');
    if (qmark) {
        strncpy(conn->query, qmark + 1, sizeof(conn->query) - 1);
        conn->query[sizeof(conn->query) - 1] = '\0';
        *qmark = '\0';
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
static void dex_http_fill_headers(DexHttpConn* conn, DexMap_str_str* headers) {
    if (conn->header_start < 0 || conn->headers_end <= conn->header_start) return;

    const char* p = conn->read_buf + conn->header_start;
    const char* limit = conn->read_buf + conn->headers_end;

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
                dex_map_str_str_set(headers, key, val);
                dex_release(key);
                dex_release(val);
            }
        }
        p = (eol < limit) ? eol + 1 : limit;
    }
}

/* Build the write buffer for a response */
/* `extra` is a newline-separated block of "Key: Value" pairs, as built by
 * http.header(). Each line is re-emitted CRLF-terminated, so a key may appear
 * more than once — which is exactly what Set-Cookie needs and what a map could
 * not have expressed. NULL or empty means none. */
static void dex_http_build_response_full(DexHttpConn* conn, const char* status,
                                     const char* body, int body_len, const char* content_type,
                                     const char* extra) {
    int extra_len = (extra && *extra) ? (int)strlen(extra) : 0;
    int needed = 512 + body_len + extra_len * 2 + 8;
    conn->write_buf = (char*)malloc(needed);
    if (!conn->write_buf) { conn->write_len = 0; conn->write_pos = 0; return; }

    int hlen = snprintf(conn->write_buf, needed,
        "HTTP/1.1 %s\r\n"
        "Content-Type: %s\r\n"
        "Content-Length: %d\r\n"
        "Connection: %s\r\n",
        status, content_type, body_len, conn->keep_alive ? "keep-alive" : "close");

    if (extra_len > 0) {
        const char* p = extra;
        while (*p) {
            const char* eol = strchr(p, '\n');
            int len = eol ? (int)(eol - p) : (int)strlen(p);
            if (len > 0 && p[len - 1] == '\r') len--;
            /* A blank or colon-less line is not a header; skip it rather than
             * emit something that would corrupt the response framing. */
            if (len > 0 && memchr(p, ':', (size_t)len) && hlen + len + 4 < needed) {
                memcpy(conn->write_buf + hlen, p, (size_t)len);
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
    memcpy(conn->write_buf + hlen, body, body_len);
    conn->write_len = hlen + body_len;
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
static void dex_http_worker_func(void* arg) {
    DexHttpConn* conn = (DexHttpConn*)arg;

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
    Dex_HttpRequest req;
    req.method = dex_string_new(conn->method, strlen(conn->method));
    req.path = dex_string_new(conn->path, strlen(conn->path));
    req.body = (body_len > 0)
        ? dex_string_new(body_data, (size_t)body_len)
        : dex_string_new("", 0);
    req.query = dex_string_new(conn->query, strlen(conn->query));
    req.params = dex_map_str_str_new();
    req.headers = dex_map_str_str_new();
    dex_http_fill_headers(conn, req.headers);

    /* Split incoming path into segments for matching */
    char path_copy[2048];
    strncpy(path_copy, conn->path, sizeof(path_copy) - 1);
    path_copy[sizeof(path_copy) - 1] = '\0';

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
            if (strcmp(conn->path, r->pattern) == 0) {
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

    conn->state = HTTP_WRITING;

    /* Enqueue into completed list and signal event loop */
    pthread_mutex_lock(&dex_http_completed_mutex);
    conn->next = dex_http_completed_head;
    dex_http_completed_head = conn;
    pthread_mutex_unlock(&dex_http_completed_mutex);
    dex_notify_pipe_signal(&dex_http_notify);
}

static volatile int dex_server_fd = -1;

/* Set only by the forked multi-worker model — see the note in dex_listen(). */
static int dex_http_share_port = 0;

static void dex_shutdown_handler(int sig) {
    (void)sig;
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

    int server_fd = socket(AF_INET, SOCK_STREAM, 0);
    if (server_fd < 0) { perror("socket"); return; }
    dex_server_fd = server_fd;

    int opt = 1;
    setsockopt(server_fd, SOL_SOCKET, SO_REUSEADDR, &opt, sizeof(opt));
#ifdef SO_REUSEPORT
    /* Only the forked worker model needs several processes on one port. Setting
     * this unconditionally means a second single-worker instance binds silently
     * alongside the first — including alongside one that is wedged or exiting —
     * and the kernel then load-balances between them, so a share of requests
     * vanish into a process that will never answer. Without it, the second
     * instance fails loudly with EADDRINUSE, which is the truth. */
    if (dex_http_share_port) {
        setsockopt(server_fd, SOL_SOCKET, SO_REUSEPORT, &opt, sizeof(opt));
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
        close(server_fd);
        return;
    }

    if (bind(server_fd, (struct sockaddr*)&addr, sizeof(addr)) < 0) {
        perror("bind"); close(server_fd); return;
    }

    if (listen(server_fd, 1024) < 0) {
        perror("listen"); close(server_fd); return;
    }

    dex_set_nonblocking(server_fd);

    /* Initialize event loop, thread pool, and notify pipe */
    dex_http_loop = dex_ev_create(1024);
    dex_http_pool = dex_pool_create(dex_http_thread_count);
    dex_notify_pipe_init(&dex_http_notify);
    memset(dex_http_conn_table, 0, sizeof(dex_http_conn_table));

    /* Register server fd for accept events */
    dex_ev_add(dex_http_loop, server_fd, DEX_EV_READ, NULL);

    /* Register notify pipe for worker completions */
    dex_ev_add(dex_http_loop, dex_http_notify.read_fd, DEX_EV_READ, (void*)(intptr_t)-2);

    printf("Dex server listening on port %d\n", port);
    fflush(stdout);

    DexEvent events[256];

    /* A one-second wait rather than an indefinite one, so the idle sweep below
     * runs even on a listener nobody is talking to. That is the whole defence
     * against a client that opens a socket, sends a byte, and waits: without
     * it nothing ever reaps the connection. */
    time_t last_sweep = time(NULL);

    for (;;) {
        int n = dex_ev_wait(dex_http_loop, events, 256, 1000);

        time_t now = time(NULL);
        if (now - last_sweep >= 1) {
            last_sweep = now;
            for (int fd = 0; fd < DEX_HTTP_MAX_FD; fd++) {
                DexHttpConn* c = dex_http_conn_table[fd];
                if (!c) continue;
                /* A dispatched connection is owned by a worker thread and a
                 * writing one is mid-response; neither is idle, and freeing
                 * either from here would pull it out from under its owner. */
                if (c->state != HTTP_READING_HEADERS && c->state != HTTP_READING_BODY) continue;
                if (now - c->last_active < dex_http_idle_secs) continue;
                dex_ev_del(dex_http_loop, c->fd);
                close(c->fd);
                dex_http_conn_free(c);
            }
        }

        for (int i = 0; i < n; i++) {
            DexEvent* ev = &events[i];

            /* Server fd: accept new connections */
            if (ev->user_data == NULL) {
                for (;;) {
                    struct sockaddr_in client_addr;
                    socklen_t client_len = sizeof(client_addr);
                    int client_fd = accept(server_fd, (struct sockaddr*)&client_addr, &client_len);
                    if (client_fd < 0) break;
                    /* Refusing beyond the cap keeps the listener answering:
                     * accepting without limit means a flood of half-open
                     * sockets exhausts fds and memory for everyone. */
                    if (dex_http_live_conns >= dex_http_max_conns) {
                        close(client_fd);
                        continue;
                    }
                    dex_set_nonblocking(client_fd);
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
                    char* new_buf = (char*)realloc(conn->read_buf, (size_t)new_cap);
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
                        /* Request complete — dispatch to worker pool */
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
                    dex_ev_del(dex_http_loop, conn->fd);
                    if (conn->keep_alive) {
                        dex_http_conn_reset(conn);
                        dex_ev_add(dex_http_loop, conn->fd, DEX_EV_READ, conn);
                    } else {
                        close(conn->fd);
                        dex_http_conn_free(conn);
                    }
                }
            }
        }
    }
}

/* ── Multi-process worker model via SO_REUSEPORT ─────────────────── */

#define DEX_MAX_WORKERS 16
static pid_t dex_worker_pids[DEX_MAX_WORKERS];
static int   dex_worker_count = 0;

static void dex_multi_shutdown_handler(int sig) {
    (void)sig;
    for (int i = 0; i < dex_worker_count; i++) {
        if (dex_worker_pids[i] > 0) {
            kill(dex_worker_pids[i], SIGTERM);
        }
    }
    if (dex_server_fd >= 0) close(dex_server_fd);
    _exit(0);
}

static void dex_listen_multi_on(const char* host, int port, int num_workers) {
    if (num_workers <= 0) {
        num_workers = (int)sysconf(_SC_NPROCESSORS_ONLN);
        if (num_workers < 1) num_workers = 1;
        if (num_workers > DEX_MAX_WORKERS) num_workers = DEX_MAX_WORKERS;
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
    char* ptr = realloc(buf->data, buf->len + total + 1);
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

    char* ptr = realloc(buf->data, buf->len + n + 2);
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
    char* result = (char*)malloc(newlen);
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
        char* h = (char*)malloc(hlen);
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
    buf.data = malloc(1);
    hbuf.data = malloc(1);
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
                char* h = (char*)malloc(hlen + 1);
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
    return strdup("");
}

const char* dex_http_form_field(const char* form, const char* key, const char* value) {
    size_t flen = strlen(form);
    size_t klen = strlen(key);
    size_t vlen = strlen(value);
    // "F\tkey\tvalue\n"
    size_t newlen = flen + 2 + klen + 1 + vlen + 1 + 1;
    char* result = (char*)malloc(newlen);
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
    char* result = (char*)malloc(newlen);
    if (!result) return "";
    snprintf(result, newlen, "%sP\t%s\t%s\n", form, key, path);
    return result;
}

static Dex_HttpResponse dex_http_post_form_impl(const char* url, const char* form, const char* headers) {
    CURL* curl = curl_easy_init();
    if (!curl) return dex_http_resp_failed("could not initialise the http client");

    dex_http_buf buf = {NULL, 0};
    dex_http_buf hbuf = {NULL, 0};
    buf.data = malloc(1);
    hbuf.data = malloc(1);
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
                char* h = (char*)malloc(hlen + 1);
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
                char* key = (char*)malloc(klen + 1);
                if (!key) goto next_form_line;
                char* val = (char*)malloc(vlen + 1);
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
