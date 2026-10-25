#include "runtime.h"
#include "../stdlib/cruntime/http.c"

static Dex_HttpResponse fuzz_handler(void* env, Dex_HttpRequest req) {
    (void)env;
    DexString* ct_key = dex_string_new("content-type", 12);
    DexString* ct = dex_map_str_str_get(req.headers, ct_key);
    DexString* id_key = dex_string_new("id", 2);
    DexString* id = dex_map_str_str_get(req.params, id_key);
    size_t n = req.method->len + req.path->len + req.query->len + req.body->len + ct->len + id->len;
    char* joined = (char*)malloc(n + 1);
    if (!joined) abort();
    size_t at = 0;
    DexString* parts[] = {req.method, req.path, req.query, req.body, ct, id};
    for (size_t i = 0; i < sizeof(parts) / sizeof(parts[0]); i++) {
        memcpy(joined + at, parts[i]->data, parts[i]->len);
        at += parts[i]->len;
    }
    Dex_HttpResponse resp;
    resp.statusCode = (int)(n % 7) == 0 ? 404 : 200;
    resp.body = dex_string_new(joined, at);
    resp.contentType = dex_string_new("text/plain", 10);
    resp.headers = dex_string_new("X-A: 1\nbad line\nX-B: 2\n", 23);
    free(joined);
    dex_release(ct);
    dex_release(ct_key);
    dex_release(id);
    dex_release(id_key);
    return resp;
}

static void fuzz_setup(void) {
    static int ready = 0;
    if (ready) return;
    ready = 1;
    if (dex_notify_pipe_init(&dex_http_notify) != 0) abort();
    DexClosure* h = dex_closure_new((void*)fuzz_handler, NULL);
    dex_route("GET", "/", h);
    dex_route("POST", "/echo", h);
    dex_route("GET", "/posts/:id", h);
    dex_route("PUT", "/posts/:id/comments/:cid", h);
    dex_route_not_found(h);
}

static void fuzz_dispatch(DexHttpConn* conn) {
    dex_http_worker_func(conn);
    dex_notify_pipe_drain(&dex_http_notify);
    dex_http_completed_head = NULL;
    if (conn->write_len < 0 || (conn->write_len > 0 && !conn->write_buf)) abort();
}

int LLVMFuzzerTestOneInput(const uint8_t* data, size_t size) {
    fuzz_setup();
    if (size == 0) return 0;
    size_t step = 1 + (size_t)data[0] * 37;
    data++;
    size--;

    DexHttpConn* conn = dex_http_conn_new(-1);
    if (!conn) abort();
    size_t off = 0;
    while (off < size) {
        int ceiling = DEX_HTTP_MAX_HEADER_BYTES;
        if (conn->state == HTTP_READING_BODY) ceiling = conn->headers_end + conn->content_length;
        if (conn->read_len >= ceiling) break;
        if (conn->read_len >= conn->read_cap - 1) {
            int new_cap = conn->read_cap * 2;
            if (new_cap > ceiling + 1) new_cap = ceiling + 1;
            char* nb = (char*)realloc(conn->read_buf, (size_t)new_cap);
            if (!nb) abort();
            conn->read_buf = nb;
            conn->read_cap = new_cap;
        }
        int want = conn->read_cap - conn->read_len - 1;
        if (want <= 0) break;
        size_t r = size - off;
        if (r > step) r = step;
        if (r > (size_t)want) r = (size_t)want;
        memcpy(conn->read_buf + conn->read_len, data + off, r);
        off += r;
        conn->read_len += (int)r;
        conn->read_buf[conn->read_len] = '\0';

        if (conn->state == HTTP_READING_HEADERS) {
            int parsed = dex_http_try_parse_headers(conn);
            if (parsed < 0) break;
            if (parsed == 1) conn->state = HTTP_READING_BODY;
        }
        if (conn->state == HTTP_READING_BODY && conn->read_len - conn->headers_end >= conn->content_length) {
            conn->state = HTTP_DISPATCHED;
            fuzz_dispatch(conn);
            if (!conn->keep_alive) break;
            dex_http_conn_reset(conn);
        }
    }
    dex_http_conn_free(conn);
    return 0;
}
