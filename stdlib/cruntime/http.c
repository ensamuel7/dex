
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <errno.h>
#include <sys/socket.h>
#include <sys/uio.h>
#include <netinet/in.h>
#include <unistd.h>
#include <pthread.h>
#include <signal.h>
#include <curl/curl.h>

typedef Dex_HttpResponse (*dex_handler_fn)(Dex_HttpRequest);

typedef struct {
    const char* method;
    const char* path;
    dex_handler_fn handler;
} dex_route_entry;

#define DEX_MAX_ROUTES 64
static dex_route_entry dex_routes[DEX_MAX_ROUTES];
static int dex_route_count = 0;

void dex_route(const char* method, const char* path, dex_handler_fn handler) {
    if (dex_route_count < DEX_MAX_ROUTES) {
        dex_routes[dex_route_count].method = method;
        dex_routes[dex_route_count].path = path;
        dex_routes[dex_route_count].handler = handler;
        dex_route_count++;
    }
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
    int           keep_alive;

    /* Write buffer (response) */
    char*         write_buf;
    int           write_len;
    int           write_pos;       /* how much has been flushed */

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
    conn->fd = fd;
    conn->state = HTTP_READING_HEADERS;
    conn->read_cap = 8192;
    conn->read_buf = (char*)malloc(conn->read_cap);
    conn->read_len = 0;
    conn->content_length = -1;
    conn->headers_end = -1;
    conn->keep_alive = 1;
    if (fd >= 0 && fd < DEX_HTTP_MAX_FD) {
        dex_http_conn_table[fd] = conn;
    }
    return conn;
}

static void dex_http_conn_free(DexHttpConn* conn) {
    if (!conn) return;
    if (conn->fd >= 0 && conn->fd < DEX_HTTP_MAX_FD) {
        dex_http_conn_table[conn->fd] = NULL;
    }
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
    memset(conn->method, 0, sizeof(conn->method));
    memset(conn->path, 0, sizeof(conn->path));
    memset(conn->query, 0, sizeof(conn->query));
    free(conn->write_buf);
    conn->write_buf = NULL;
    conn->write_len = 0;
    conn->write_pos = 0;
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

    /* Parse method and path from request line */
    char raw_path[2048] = {0};
    sscanf(conn->read_buf, "%15s %2047s", conn->method, raw_path);

    /* Split path and query at '?' */
    char* qmark = strchr(raw_path, '?');
    if (qmark) {
        size_t plen = (size_t)(qmark - raw_path);
        if (plen >= sizeof(conn->path)) plen = sizeof(conn->path) - 1;
        memcpy(conn->path, raw_path, plen);
        conn->path[plen] = '\0';
        strncpy(conn->query, qmark + 1, sizeof(conn->query) - 1);
    } else {
        strncpy(conn->path, raw_path, sizeof(conn->path) - 1);
    }

    /* Check keep-alive */
    conn->keep_alive = (strstr(conn->read_buf, "HTTP/1.1") != NULL);
    if (strstr(conn->read_buf, "Connection: close")) conn->keep_alive = 0;

    /* Parse Content-Length */
    const char* cl = strstr(conn->read_buf, "Content-Length:");
    if (!cl) cl = strstr(conn->read_buf, "content-length:");
    if (cl) {
        conn->content_length = atoi(cl + 15);
    } else {
        conn->content_length = 0;
    }

    conn->read_buf[conn->headers_end] = save;
    return 1;
}

/* Build the write buffer for a response */
static void dex_http_build_response(DexHttpConn* conn, const char* status,
                                     const char* body, const char* content_type) {
    int body_len = (int)strlen(body);
    int needed = 512 + body_len;
    conn->write_buf = (char*)malloc(needed);
    int hlen = snprintf(conn->write_buf, needed,
        "HTTP/1.1 %s\r\n"
        "Content-Type: %s\r\n"
        "Content-Length: %d\r\n"
        "Connection: %s\r\n"
        "\r\n",
        status, content_type, body_len, conn->keep_alive ? "keep-alive" : "close");
    memcpy(conn->write_buf + hlen, body, body_len);
    conn->write_len = hlen + body_len;
    conn->write_pos = 0;
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

    /* Build HttpRequest struct */
    Dex_HttpRequest req;
    req.method = dex_string_from_lit(conn->method);
    req.path = dex_string_from_lit(conn->path);
    req.body = dex_string_from_lit(body_data);
    req.query = dex_string_from_lit(conn->query);

    /* Match route */
    int matched = 0;
    for (int i = 0; i < dex_route_count; i++) {
        if (strcmp(conn->method, dex_routes[i].method) == 0 &&
            strcmp(conn->path, dex_routes[i].path) == 0) {
            Dex_HttpResponse resp = dex_routes[i].handler(req);
            const char* status = dex_http_status_text(resp.statusCode);
            const char* ct = "application/json";
            if (resp.contentType && resp.contentType->data[0] != '\0') {
                ct = resp.contentType->data;
            }
            dex_http_build_response(conn, status, resp.body->data, ct);
            if (resp.contentType) dex_release(resp.contentType);
            dex_release(resp.body);
            matched = 1;
            break;
        }
    }

    /* Release request strings */
    dex_release(req.method);
    dex_release(req.path);
    dex_release(req.body);
    dex_release(req.query);

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

static void dex_shutdown_handler(int sig) {
    (void)sig;
    if (dex_server_fd >= 0) close(dex_server_fd);
    _exit(0);
}

void dex_listen(int port) {
    signal(SIGPIPE, SIG_IGN);
    signal(SIGINT, dex_shutdown_handler);
    signal(SIGTERM, dex_shutdown_handler);

    int server_fd = socket(AF_INET, SOCK_STREAM, 0);
    if (server_fd < 0) { perror("socket"); return; }
    dex_server_fd = server_fd;

    int opt = 1;
    setsockopt(server_fd, SOL_SOCKET, SO_REUSEADDR, &opt, sizeof(opt));
#ifdef SO_REUSEPORT
    setsockopt(server_fd, SOL_SOCKET, SO_REUSEPORT, &opt, sizeof(opt));
#endif

    struct sockaddr_in addr;
    addr.sin_family = AF_INET;
    addr.sin_addr.s_addr = INADDR_ANY;
    addr.sin_port = htons(port);

    if (bind(server_fd, (struct sockaddr*)&addr, sizeof(addr)) < 0) {
        perror("bind"); close(server_fd); return;
    }

    if (listen(server_fd, 1024) < 0) {
        perror("listen"); close(server_fd); return;
    }

    dex_set_nonblocking(server_fd);

    /* Initialize event loop, thread pool, and notify pipe */
    dex_http_loop = dex_ev_create(1024);
    dex_http_pool = dex_pool_create(0); /* auto-size */
    dex_notify_pipe_init(&dex_http_notify);
    memset(dex_http_conn_table, 0, sizeof(dex_http_conn_table));

    /* Register server fd for accept events */
    dex_ev_add(dex_http_loop, server_fd, DEX_EV_READ, NULL);

    /* Register notify pipe for worker completions */
    dex_ev_add(dex_http_loop, dex_http_notify.read_fd, DEX_EV_READ, (void*)(intptr_t)-2);

    printf("Dex server listening on port %d\n", port);
    fflush(stdout);

    DexEvent events[256];

    for (;;) {
        int n = dex_ev_wait(dex_http_loop, events, 256, -1);

        for (int i = 0; i < n; i++) {
            DexEvent* ev = &events[i];

            /* Server fd: accept new connections */
            if (ev->user_data == NULL) {
                for (;;) {
                    struct sockaddr_in client_addr;
                    socklen_t client_len = sizeof(client_addr);
                    int client_fd = accept(server_fd, (struct sockaddr*)&client_addr, &client_len);
                    if (client_fd < 0) break;
                    dex_set_nonblocking(client_fd);
                    DexHttpConn* conn = dex_http_conn_new(client_fd);
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
                /* Grow buffer if needed */
                if (conn->read_len >= conn->read_cap - 1) {
                    conn->read_cap *= 2;
                    conn->read_buf = (char*)realloc(conn->read_buf, conn->read_cap);
                }
                int r = (int)read(conn->fd, conn->read_buf + conn->read_len,
                                  conn->read_cap - conn->read_len - 1);
                if (r <= 0) {
                    /* Connection closed or error */
                    dex_ev_del(dex_http_loop, conn->fd);
                    close(conn->fd);
                    dex_http_conn_free(conn);
                    continue;
                }
                conn->read_len += r;
                conn->read_buf[conn->read_len] = '\0';

                if (conn->state == HTTP_READING_HEADERS) {
                    if (dex_http_try_parse_headers(conn)) {
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

void dex_listen_multi(int port, int num_workers) {
    if (num_workers <= 0) {
        num_workers = (int)sysconf(_SC_NPROCESSORS_ONLN);
        if (num_workers < 1) num_workers = 1;
        if (num_workers > DEX_MAX_WORKERS) num_workers = DEX_MAX_WORKERS;
    }
    if (num_workers == 1) {
        dex_listen(port);
        return;
    }
    if (num_workers > DEX_MAX_WORKERS) num_workers = DEX_MAX_WORKERS;

    signal(SIGPIPE, SIG_IGN);

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
            dex_listen(port);
            _exit(0);
        }
        dex_worker_pids[i] = pid;
    }

    /* Parent: install graceful shutdown handler */
    signal(SIGINT, dex_multi_shutdown_handler);
    signal(SIGTERM, dex_multi_shutdown_handler);

    /* Parent becomes the last worker */
    dex_listen(port);
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

// --- Header builder ---
const char* dex_http_header(const char* headers, const char* key, const char* value) {
    size_t hlen = strlen(headers);
    size_t klen = strlen(key);
    size_t vlen = strlen(value);
    // "Key: Value\n"
    size_t newlen = hlen + klen + 2 + vlen + 1 + 1;
    char* result = (char*)malloc(newlen);
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

static Dex_HttpResponse dex_http_request_impl(const char* method, const char* url, const char* body, const char* headers) {
    Dex_HttpResponse resp = {0, NULL};
    CURL* curl = curl_easy_init();
    if (!curl) return resp;

    dex_http_buf buf = {NULL, 0};
    buf.data = malloc(1);
    buf.data[0] = '\0';

    curl_easy_setopt(curl, CURLOPT_URL, url);
    curl_easy_setopt(curl, CURLOPT_WRITEFUNCTION, dex_http_write_cb);
    curl_easy_setopt(curl, CURLOPT_WRITEDATA, &buf);
    curl_easy_setopt(curl, CURLOPT_CUSTOMREQUEST, method);

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

    CURLcode res = curl_easy_perform(curl);
    if (res == CURLE_OK) {
        long code;
        curl_easy_getinfo(curl, CURLINFO_RESPONSE_CODE, &code);
        resp.statusCode = (int)code;
        resp.body = dex_string_from_cstr(buf.data);
        char* ct = NULL;
        curl_easy_getinfo(curl, CURLINFO_CONTENT_TYPE, &ct);
        resp.contentType = dex_string_from_lit(ct ? ct : "");
    } else {
        resp.statusCode = 0;
        resp.body = dex_string_from_lit(curl_easy_strerror(res));
        resp.contentType = dex_string_from_lit("");
        free(buf.data);
    }

    if (hdr_list) curl_slist_free_all(hdr_list);
    curl_easy_cleanup(curl);
    return resp;
}

Dex_HttpResponse dex_http_get(const char* url) {
    return dex_http_request_impl("GET", url, NULL, NULL);
}

Dex_HttpResponse dex_http_get_h(const char* url, const char* headers) {
    return dex_http_request_impl("GET", url, NULL, headers);
}

Dex_HttpResponse dex_http_post(const char* url, const char* body) {
    return dex_http_request_impl("POST", url, body, "Content-Type: application/json");
}

Dex_HttpResponse dex_http_post_h(const char* url, const char* body, const char* headers) {
    return dex_http_request_impl("POST", url, body, headers);
}

Dex_HttpResponse dex_http_put(const char* url, const char* body) {
    return dex_http_request_impl("PUT", url, body, "Content-Type: application/json");
}

Dex_HttpResponse dex_http_put_h(const char* url, const char* body, const char* headers) {
    return dex_http_request_impl("PUT", url, body, headers);
}

Dex_HttpResponse dex_http_patch(const char* url, const char* body) {
    return dex_http_request_impl("PATCH", url, body, "Content-Type: application/json");
}

Dex_HttpResponse dex_http_patch_h(const char* url, const char* body, const char* headers) {
    return dex_http_request_impl("PATCH", url, body, headers);
}

Dex_HttpResponse dex_http_delete(const char* url) {
    return dex_http_request_impl("DELETE", url, NULL, NULL);
}

Dex_HttpResponse dex_http_delete_h(const char* url, const char* headers) {
    return dex_http_request_impl("DELETE", url, NULL, headers);
}

Dex_HttpResponse dex_http_request(const char* method, const char* url, const char* body, const char* headers) {
    return dex_http_request_impl(method, url, body, headers);
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
    snprintf(result, newlen, "%sP\t%s\t%s\n", form, key, path);
    return result;
}

static Dex_HttpResponse dex_http_post_form_impl(const char* url, const char* form, const char* headers) {
    Dex_HttpResponse resp = {0, NULL};
    CURL* curl = curl_easy_init();
    if (!curl) return resp;

    dex_http_buf buf = {NULL, 0};
    buf.data = malloc(1);
    buf.data[0] = '\0';

    curl_easy_setopt(curl, CURLOPT_URL, url);
    curl_easy_setopt(curl, CURLOPT_WRITEFUNCTION, dex_http_write_cb);
    curl_easy_setopt(curl, CURLOPT_WRITEDATA, &buf);

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
                char* val = (char*)malloc(vlen + 1);
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
        p = (*line_end) ? line_end + 1 : line_end;
    }

    curl_easy_setopt(curl, CURLOPT_MIMEPOST, mime);

    CURLcode res = curl_easy_perform(curl);
    if (res == CURLE_OK) {
        long code;
        curl_easy_getinfo(curl, CURLINFO_RESPONSE_CODE, &code);
        resp.statusCode = (int)code;
        resp.body = dex_string_from_cstr(buf.data);
        char* ct = NULL;
        curl_easy_getinfo(curl, CURLINFO_CONTENT_TYPE, &ct);
        resp.contentType = dex_string_from_cstr(ct ? ct : "");
        free(buf.data);
    } else {
        resp.statusCode = 0;
        resp.body = dex_string_from_cstr(curl_easy_strerror(res));
        resp.contentType = dex_string_from_cstr("");
        free(buf.data);
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
