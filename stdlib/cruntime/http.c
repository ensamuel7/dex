
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/uio.h>
#include <netinet/in.h>
#include <unistd.h>
#include <pthread.h>
#include <signal.h>
#include <curl/curl.h>

typedef Dex_HttpResponse (*dex_handler_fn)(void);

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

static void dex_send_response(int fd, const char* status, const char* body, int keep_alive) {
    int body_len = (int)strlen(body);
    char header[512];
    int hlen = snprintf(header, sizeof(header),
        "HTTP/1.1 %s\r\n"
        "Content-Type: application/json\r\n"
        "Content-Length: %d\r\n"
        "Connection: %s\r\n"
        "\r\n",
        status, body_len, keep_alive ? "keep-alive" : "close");
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

static void dex_handle_connection(int client_fd) {
    char buf[4096];

    for (;;) {
        int n = (int)read(client_fd, buf, sizeof(buf) - 1);
        if (n <= 0) break;
        buf[n] = '\0';

        // Parse method and path from request line
        char method[16] = {0};
        char path[256] = {0};
        sscanf(buf, "%15s %255s", method, path);

        // Check for keep-alive (HTTP/1.1 defaults to keep-alive)
        int keep_alive = (strstr(buf, "HTTP/1.1") != NULL);
        if (strstr(buf, "Connection: close")) keep_alive = 0;

        // Match route
        int matched = 0;
        for (int i = 0; i < dex_route_count; i++) {
            if (strcmp(method, dex_routes[i].method) == 0 &&
                strcmp(path, dex_routes[i].path) == 0) {
                Dex_HttpResponse resp = dex_routes[i].handler();
                const char* status = dex_http_status_text(resp.statusCode);
                dex_send_response(client_fd, status, resp.body->data, keep_alive);
                dex_release(resp.body);
                matched = 1;
                break;
            }
        }

        if (!matched) {
            dex_send_response(client_fd, "404 Not Found",
                "{\"error\": \"Not Found\"}", keep_alive);
        }

        if (!keep_alive) break;
    }
    close(client_fd);
}

static void* dex_worker(void* arg) {
    int client_fd = *(int*)arg;
    free(arg);
    dex_handle_connection(client_fd);
    return NULL;
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

    printf("Dex server listening on port %d\n", port);
    fflush(stdout);

    while (1) {
        struct sockaddr_in client_addr;
        socklen_t client_len = sizeof(client_addr);
        int client_fd = accept(server_fd, (struct sockaddr*)&client_addr, &client_len);
        if (client_fd < 0) continue;

        int* fd_ptr = (int*)malloc(sizeof(int));
        *fd_ptr = client_fd;
        pthread_t tid;
        pthread_create(&tid, NULL, dex_worker, fd_ptr);
        pthread_detach(tid);
    }
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
        free(buf.data);
    } else {
        resp.statusCode = 0;
        resp.body = dex_string_from_cstr(curl_easy_strerror(res));
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
        free(buf.data);
    } else {
        resp.statusCode = 0;
        resp.body = dex_string_from_cstr(curl_easy_strerror(res));
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
