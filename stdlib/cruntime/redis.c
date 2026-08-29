// Redis client.
//
// RESP is small enough to speak directly — a command is an array of bulk
// strings, a reply is one of five things — so this depends on nothing but
// sockets. That keeps `dex build` working on a machine with no Redis headers
// installed, which a linked client library could not promise.
//
// Each handle owns a pool of sockets because a Redis connection carries one
// request at a time: two threads sharing one socket would read each other's
// replies. A subscriber holds a socket of its own, since SUBSCRIBE takes the
// connection out of request/reply for good.

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <errno.h>
#include <unistd.h>
#include <sys/socket.h>
#include <netinet/in.h>
#include <netinet/tcp.h>
#include <netdb.h>
#include <pthread.h>

#define DEX_REDIS_MAX_CONNS 16
#define DEX_REDIS_POOL 8

// --- Socket with a read buffer ---

typedef struct {
    int fd;              // -1 when not connected
    char* buf;
    size_t cap;
    size_t len;          // bytes held
    size_t pos;          // bytes consumed
} DexRedisSock;

typedef struct {
    int used;
    char host[256];
    int port;
    char password[256];
    DexRedisSock socks[DEX_REDIS_POOL];
    int in_use[DEX_REDIS_POOL];
    pthread_mutex_t lock;
    pthread_cond_t available;
    DexRedisSock sub;           // subscriber socket, dialled on first subscribe
    char sub_channel[256];      // channel of the most recent message
} DexRedisConn;

static DexRedisConn dex_redis_conns[DEX_REDIS_MAX_CONNS];
static pthread_mutex_t dex_redis_table_lock = PTHREAD_MUTEX_INITIALIZER;

// A string handed back to Dex is owned by the caller: the generated code wraps
// it with dex_string_from_cstr, which copies the bytes and frees the buffer. So
// every return here is freshly allocated, never a literal or a static.
static char* dex_redis_own(const char* s, size_t len) {
    char* out = (char*)malloc(len + 1);
    if (!out) return NULL;
    if (len > 0) memcpy(out, s, len);
    out[len] = '\0';
    return out;
}

static char* dex_redis_empty(void) { return dex_redis_own("", 0); }

// --- Connecting ---

static int dex_redis_dial(const char* host, int port) {
    char portstr[16];
    snprintf(portstr, sizeof(portstr), "%d", port);

    struct addrinfo hints, *res = NULL;
    memset(&hints, 0, sizeof(hints));
    hints.ai_family = AF_UNSPEC;
    hints.ai_socktype = SOCK_STREAM;
    if (getaddrinfo(host, portstr, &hints, &res) != 0) return -1;

    int fd = -1;
    for (struct addrinfo* a = res; a; a = a->ai_next) {
        fd = socket(a->ai_family, a->ai_socktype, a->ai_protocol);
        if (fd < 0) continue;
        if (connect(fd, a->ai_addr, a->ai_addrlen) == 0) break;
        close(fd);
        fd = -1;
    }
    freeaddrinfo(res);
    if (fd < 0) return -1;

    int one = 1;
    setsockopt(fd, IPPROTO_TCP, TCP_NODELAY, &one, sizeof(one));
    return fd;
}

static void dex_redis_sock_reset(DexRedisSock* s) {
    if (s->fd >= 0) close(s->fd);
    s->fd = -1;
    s->len = 0;
    s->pos = 0;
}

// --- Writing a command ---

static int dex_redis_write_all(int fd, const char* buf, size_t len) {
    size_t sent = 0;
    while (sent < len) {
        ssize_t n = write(fd, buf + sent, len - sent);
        if (n <= 0) {
            if (n < 0 && errno == EINTR) continue;
            return -1;
        }
        sent += (size_t)n;
    }
    return 0;
}

static int dex_redis_send(DexRedisSock* s, int argc, const char** argv, const size_t* lens) {
    // Sized up front so the whole command leaves in one write: Redis pipelines
    // fine, but a command split across writes gives Nagle something to think about.
    size_t need = 16;
    for (int i = 0; i < argc; i++) need += lens[i] + 32;

    char* out = (char*)malloc(need);
    if (!out) return -1;
    int pos = snprintf(out, need, "*%d\r\n", argc);
    for (int i = 0; i < argc; i++) {
        pos += snprintf(out + pos, need - pos, "$%zu\r\n", lens[i]);
        memcpy(out + pos, argv[i], lens[i]);
        pos += (int)lens[i];
        out[pos++] = '\r';
        out[pos++] = '\n';
    }
    int rc = dex_redis_write_all(s->fd, out, (size_t)pos);
    free(out);
    return rc;
}

// --- Reading a reply ---

typedef struct DexRedisReply {
    char type;                       // '+' '-' ':' '$' '*', or 0 on failure
    long long integer;
    char* str;
    size_t len;
    struct DexRedisReply** elems;
    int nelems;
    int is_nil;
} DexRedisReply;

static void dex_redis_reply_free(DexRedisReply* r) {
    if (!r) return;
    for (int i = 0; i < r->nelems; i++) dex_redis_reply_free(r->elems[i]);
    free(r->elems);
    free(r->str);
    free(r);
}

static int dex_redis_fill(DexRedisSock* s) {
    if (s->pos > 0 && s->pos == s->len) { s->pos = 0; s->len = 0; }
    if (s->len == s->cap) {
        size_t want = s->cap ? s->cap * 2 : 16384;
        char* grown = (char*)realloc(s->buf, want);
        if (!grown) return -1;
        s->buf = grown;
        s->cap = want;
    }
    ssize_t n = read(s->fd, s->buf + s->len, s->cap - s->len);
    if (n <= 0) {
        if (n < 0 && errno == EINTR) return 0;
        return -1;
    }
    s->len += (size_t)n;
    return 0;
}

// Returns a pointer to the line's bytes (without CRLF) inside the socket buffer.
static char* dex_redis_read_line(DexRedisSock* s, size_t* out_len) {
    for (;;) {
        for (size_t i = s->pos; i + 1 < s->len; i++) {
            if (s->buf[i] == '\r' && s->buf[i + 1] == '\n') {
                char* line = s->buf + s->pos;
                *out_len = i - s->pos;
                s->pos = i + 2;
                return line;
            }
        }
        if (dex_redis_fill(s) < 0) return NULL;
    }
}

static int dex_redis_read_exact(DexRedisSock* s, char* dst, size_t want) {
    size_t got = 0;
    while (got < want) {
        size_t have = s->len - s->pos;
        if (have == 0) {
            if (dex_redis_fill(s) < 0) return -1;
            continue;
        }
        size_t take = have < want - got ? have : want - got;
        memcpy(dst + got, s->buf + s->pos, take);
        s->pos += take;
        got += take;
    }
    return 0;
}

static DexRedisReply* dex_redis_read_reply(DexRedisSock* s) {
    size_t line_len = 0;
    char* line = dex_redis_read_line(s, &line_len);
    if (!line) return NULL;

    DexRedisReply* r = (DexRedisReply*)calloc(1, sizeof(DexRedisReply));
    if (!r) return NULL;
    r->type = line[0];

    switch (r->type) {
    case '+':
    case '-':
        r->len = line_len - 1;
        r->str = (char*)malloc(r->len + 1);
        if (!r->str) { free(r); return NULL; }
        memcpy(r->str, line + 1, r->len);
        r->str[r->len] = '\0';
        return r;

    case ':':
        r->integer = strtoll(line + 1, NULL, 10);
        return r;

    case '$': {
        long long n = strtoll(line + 1, NULL, 10);
        if (n < 0) { r->is_nil = 1; return r; }
        r->str = (char*)malloc((size_t)n + 1);
        if (!r->str) { free(r); return NULL; }
        if (dex_redis_read_exact(s, r->str, (size_t)n) < 0) { dex_redis_reply_free(r); return NULL; }
        r->str[n] = '\0';
        r->len = (size_t)n;
        // Trailing CRLF
        size_t skip = 0;
        if (!dex_redis_read_line(s, &skip) && skip == 0) { /* connection gone; reply still usable */ }
        return r;
    }

    case '*': {
        long long n = strtoll(line + 1, NULL, 10);
        if (n < 0) { r->is_nil = 1; return r; }
        r->nelems = (int)n;
        if (n > 0) {
            r->elems = (DexRedisReply**)calloc((size_t)n, sizeof(DexRedisReply*));
            if (!r->elems) { free(r); return NULL; }
            for (int i = 0; i < r->nelems; i++) {
                r->elems[i] = dex_redis_read_reply(s);
                if (!r->elems[i]) { dex_redis_reply_free(r); return NULL; }
            }
        }
        return r;
    }

    default:
        free(r);
        return NULL;
    }
}

// --- Pooling ---

static DexRedisConn* dex_redis_handle(int h) {
    if (h < 0 || h >= DEX_REDIS_MAX_CONNS) return NULL;
    if (!dex_redis_conns[h].used) return NULL;
    return &dex_redis_conns[h];
}

static int dex_redis_hello(DexRedisConn* c, DexRedisSock* s);

// Dials if the slot is empty or its socket died since last time.
static DexRedisSock* dex_redis_acquire(DexRedisConn* c, int* slot_out) {
    pthread_mutex_lock(&c->lock);
    int slot = -1;
    for (;;) {
        for (int i = 0; i < DEX_REDIS_POOL; i++) {
            if (!c->in_use[i]) { slot = i; break; }
        }
        if (slot >= 0) break;
        pthread_cond_wait(&c->available, &c->lock);
    }
    c->in_use[slot] = 1;
    pthread_mutex_unlock(&c->lock);

    DexRedisSock* s = &c->socks[slot];
    if (s->fd < 0) {
        s->fd = dex_redis_dial(c->host, c->port);
        if (s->fd >= 0 && dex_redis_hello(c, s) < 0) dex_redis_sock_reset(s);
    }
    *slot_out = slot;
    return s;
}

static void dex_redis_release(DexRedisConn* c, int slot) {
    pthread_mutex_lock(&c->lock);
    c->in_use[slot] = 0;
    pthread_cond_signal(&c->available);
    pthread_mutex_unlock(&c->lock);
}

// Runs one command on a pooled socket. A socket that fails mid-command is
// closed rather than returned to the pool holding half a reply.
static DexRedisReply* dex_redis_call(int h, int argc, const char** argv, const size_t* lens) {
    DexRedisConn* c = dex_redis_handle(h);
    if (!c) return NULL;

    int slot = -1;
    DexRedisSock* s = dex_redis_acquire(c, &slot);
    DexRedisReply* reply = NULL;

    for (int attempt = 0; attempt < 2 && !reply; attempt++) {
        if (s->fd < 0) {
            s->fd = dex_redis_dial(c->host, c->port);
            if (s->fd >= 0 && dex_redis_hello(c, s) < 0) dex_redis_sock_reset(s);
            if (s->fd < 0) break;
        }
        if (dex_redis_send(s, argc, argv, lens) < 0) { dex_redis_sock_reset(s); continue; }
        reply = dex_redis_read_reply(s);
        if (!reply) dex_redis_sock_reset(s);   // a dropped connection is worth one retry
    }

    dex_redis_release(c, slot);
    return reply;
}

static int dex_redis_hello(DexRedisConn* c, DexRedisSock* s) {
    if (c->password[0] == '\0') return 0;
    const char* argv[2] = { "AUTH", c->password };
    size_t lens[2] = { 4, strlen(c->password) };
    if (dex_redis_send(s, 2, argv, lens) < 0) return -1;
    DexRedisReply* r = dex_redis_read_reply(s);
    int ok = r && r->type == '+';
    dex_redis_reply_free(r);
    return ok ? 0 : -1;
}

// --- Small helpers over dex_redis_call ---

static DexRedisReply* dex_redis_call1(int h, const char* cmd, const char* a) {
    const char* argv[2] = { cmd, a };
    size_t lens[2] = { strlen(cmd), strlen(a) };
    return dex_redis_call(h, 2, argv, lens);
}

static DexRedisReply* dex_redis_call2(int h, const char* cmd, const char* a, const char* b) {
    const char* argv[3] = { cmd, a, b };
    size_t lens[3] = { strlen(cmd), strlen(a), strlen(b) };
    return dex_redis_call(h, 3, argv, lens);
}

static DexRedisReply* dex_redis_call3(int h, const char* cmd, const char* a, const char* b, const char* c) {
    const char* argv[4] = { cmd, a, b, c };
    size_t lens[4] = { strlen(cmd), strlen(a), strlen(b), strlen(c) };
    return dex_redis_call(h, 4, argv, lens);
}

static long long dex_redis_int_of(DexRedisReply* r) {
    if (!r) return 0;
    long long v = 0;
    if (r->type == ':') v = r->integer;
    else if (r->type == '$' && !r->is_nil && r->str) v = strtoll(r->str, NULL, 10);
    dex_redis_reply_free(r);
    return v;
}

static _Bool dex_redis_ok_of(DexRedisReply* r) {
    if (!r) return 0;
    _Bool ok = (r->type == '+') || (r->type == ':' && r->integer >= 0);
    dex_redis_reply_free(r);
    return ok;
}

// A missing key reads as the empty string: callers branch on emptiness rather
// than on a separate absence flag, the way the rest of the stdlib behaves.
static char* dex_redis_str_of(DexRedisReply* r) {
    char* out = (r && !r->is_nil && r->str) ? dex_redis_own(r->str, r->len) : dex_redis_empty();
    dex_redis_reply_free(r);
    return out;
}

// --- The surface Dex calls ---

int dex_redis_connect(const char* host, int port, const char* password) {
    pthread_mutex_lock(&dex_redis_table_lock);
    int h = -1;
    for (int i = 0; i < DEX_REDIS_MAX_CONNS; i++) {
        if (!dex_redis_conns[i].used) { h = i; break; }
    }
    if (h < 0) { pthread_mutex_unlock(&dex_redis_table_lock); return -1; }

    DexRedisConn* c = &dex_redis_conns[h];
    memset(c, 0, sizeof(*c));
    c->used = 1;
    snprintf(c->host, sizeof(c->host), "%s", host ? host : "127.0.0.1");
    c->port = port > 0 ? port : 6379;
    snprintf(c->password, sizeof(c->password), "%s", password ? password : "");
    for (int i = 0; i < DEX_REDIS_POOL; i++) c->socks[i].fd = -1;
    c->sub.fd = -1;
    pthread_mutex_init(&c->lock, NULL);
    pthread_cond_init(&c->available, NULL);
    pthread_mutex_unlock(&dex_redis_table_lock);

    // One socket is dialled now so a bad address fails at connect rather than
    // at the first command.
    int slot = -1;
    DexRedisSock* s = dex_redis_acquire(c, &slot);
    int alive = s->fd >= 0;
    dex_redis_release(c, slot);
    if (!alive) {
        pthread_mutex_lock(&dex_redis_table_lock);
        c->used = 0;
        pthread_mutex_unlock(&dex_redis_table_lock);
        return -1;
    }
    return h;
}

void dex_redis_close(int h) {
    DexRedisConn* c = dex_redis_handle(h);
    if (!c) return;
    pthread_mutex_lock(&c->lock);
    for (int i = 0; i < DEX_REDIS_POOL; i++) {
        dex_redis_sock_reset(&c->socks[i]);
        free(c->socks[i].buf);
        c->socks[i].buf = NULL;
        c->socks[i].cap = 0;
    }
    dex_redis_sock_reset(&c->sub);
    free(c->sub.buf);
    c->sub.buf = NULL;
    c->sub.cap = 0;
    pthread_mutex_unlock(&c->lock);

    pthread_mutex_lock(&dex_redis_table_lock);
    c->used = 0;
    pthread_mutex_unlock(&dex_redis_table_lock);
}

_Bool dex_redis_ping(int h) {
    const char* argv[1] = { "PING" };
    size_t lens[1] = { 4 };
    return dex_redis_ok_of(dex_redis_call(h, 1, argv, lens));
}

_Bool dex_redis_set(int h, const char* key, const char* value) {
    return dex_redis_ok_of(dex_redis_call2(h, "SET", key, value));
}

_Bool dex_redis_setex(int h, const char* key, const char* value, int seconds) {
    char ttl[32];
    snprintf(ttl, sizeof(ttl), "%d", seconds);
    const char* argv[5] = { "SET", key, value, "EX", ttl };
    size_t lens[5] = { 3, strlen(key), strlen(value), 2, strlen(ttl) };
    return dex_redis_ok_of(dex_redis_call(h, 5, argv, lens));
}

char* dex_redis_get(int h, const char* key) {
    return dex_redis_str_of(dex_redis_call1(h, "GET", key));
}

_Bool dex_redis_exists(int h, const char* key) {
    return dex_redis_int_of(dex_redis_call1(h, "EXISTS", key)) > 0;
}

long long dex_redis_del(int h, const char* key) {
    return dex_redis_int_of(dex_redis_call1(h, "DEL", key));
}

_Bool dex_redis_expire(int h, const char* key, int seconds) {
    char ttl[32];
    snprintf(ttl, sizeof(ttl), "%d", seconds);
    return dex_redis_int_of(dex_redis_call2(h, "EXPIRE", key, ttl)) > 0;
}

long long dex_redis_ttl(int h, const char* key) {
    return dex_redis_int_of(dex_redis_call1(h, "TTL", key));
}

long long dex_redis_incr(int h, const char* key) {
    return dex_redis_int_of(dex_redis_call1(h, "INCR", key));
}

_Bool dex_redis_hset(int h, const char* key, const char* field, const char* value) {
    DexRedisReply* r = dex_redis_call3(h, "HSET", key, field, value);
    if (!r) return 0;
    _Bool ok = (r->type == ':');
    dex_redis_reply_free(r);
    return ok;
}

char* dex_redis_hget(int h, const char* key, const char* field) {
    return dex_redis_str_of(dex_redis_call2(h, "HGET", key, field));
}

long long dex_redis_hdel(int h, const char* key, const char* field) {
    return dex_redis_int_of(dex_redis_call2(h, "HDEL", key, field));
}

long long dex_redis_sadd(int h, const char* key, const char* member) {
    return dex_redis_int_of(dex_redis_call2(h, "SADD", key, member));
}

long long dex_redis_srem(int h, const char* key, const char* member) {
    return dex_redis_int_of(dex_redis_call2(h, "SREM", key, member));
}

_Bool dex_redis_sismember(int h, const char* key, const char* member) {
    return dex_redis_int_of(dex_redis_call2(h, "SISMEMBER", key, member)) > 0;
}

long long dex_redis_publish(int h, const char* channel, const char* message) {
    return dex_redis_int_of(dex_redis_call2(h, "PUBLISH", channel, message));
}

// Array replies become string arrays; a nested or nil element reads as "".
#ifdef DEX_HAVE_ARRAYS
static DexArrayString* dex_redis_arr_of(DexRedisReply* r) {
    DexArrayString* out = dex_array_string_new();
    if (r && r->type == '*') {
        for (int i = 0; i < r->nelems; i++) {
            DexRedisReply* e = r->elems[i];
            DexString* s = (e && !e->is_nil && e->str) ? dex_string_new(e->str, e->len) : dex_string_new("", 0);
            dex_array_string_push(out, s);
            dex_release(s);
        }
    }
    dex_redis_reply_free(r);
    return out;
}

DexArrayString* dex_redis_keys(int h, const char* pattern) {
    return dex_redis_arr_of(dex_redis_call1(h, "KEYS", pattern));
}

DexArrayString* dex_redis_smembers(int h, const char* key) {
    return dex_redis_arr_of(dex_redis_call1(h, "SMEMBERS", key));
}

DexArrayString* dex_redis_hkeys(int h, const char* key) {
    return dex_redis_arr_of(dex_redis_call1(h, "HKEYS", key));
}

// HGETALL as a flat array: field, value, field, value. A map return would be
// friendlier, but flat keeps this module free of the map runtime.
DexArrayString* dex_redis_hgetall(int h, const char* key) {
    return dex_redis_arr_of(dex_redis_call1(h, "HGETALL", key));
}

// The escape hatch: anything this module does not wrap, spelled as its
// arguments. `redis.command(conn, ["ZADD", "k", "1", "m"])`.
char* dex_redis_command(int h, DexArrayString* args) {
    if (!args || args->len == 0) return dex_redis_empty();
    int argc = args->len;
    const char** argv = (const char**)malloc(sizeof(char*) * argc);
    size_t* lens = (size_t*)malloc(sizeof(size_t) * argc);
    if (!argv || !lens) { free(argv); free(lens); return dex_redis_empty(); }
    for (int i = 0; i < argc; i++) {
        argv[i] = args->data[i]->data;
        lens[i] = args->data[i]->len;
    }
    DexRedisReply* r = dex_redis_call(h, argc, argv, lens);
    free(argv);
    free(lens);

    if (!r) return dex_redis_empty();
    char* out = NULL;
    if (r->type == ':') {
        char num[32];
        int n = snprintf(num, sizeof(num), "%lld", r->integer);
        out = dex_redis_own(num, (size_t)n);
    } else if (r->str && !r->is_nil) {
        out = dex_redis_own(r->str, r->len);
    } else {
        out = dex_redis_empty();
    }
    dex_redis_reply_free(r);
    return out;
}
#endif

// --- Subscribing ---
//
// SUBSCRIBE takes a connection out of request/reply, so the subscriber gets a
// socket of its own and never returns to the pool. Messages are pulled rather
// than pushed: a caller spawns a thread and loops on nextMessage, which is the
// same shape as a listener anywhere else in Dex.

_Bool dex_redis_subscribe(int h, const char* channel) {
    DexRedisConn* c = dex_redis_handle(h);
    if (!c) return 0;

    pthread_mutex_lock(&c->lock);
    if (c->sub.fd < 0) {
        c->sub.fd = dex_redis_dial(c->host, c->port);
        if (c->sub.fd >= 0 && dex_redis_hello(c, &c->sub) < 0) dex_redis_sock_reset(&c->sub);
    }
    int ok = 0;
    if (c->sub.fd >= 0) {
        const char* argv[2] = { "SUBSCRIBE", channel };
        size_t lens[2] = { 9, strlen(channel) };
        if (dex_redis_send(&c->sub, 2, argv, lens) == 0) {
            DexRedisReply* r = dex_redis_read_reply(&c->sub);
            ok = r && r->type == '*';
            dex_redis_reply_free(r);
        }
        if (!ok) dex_redis_sock_reset(&c->sub);
    }
    pthread_mutex_unlock(&c->lock);
    return ok ? 1 : 0;
}

// Blocks until the next message arrives, and returns its payload. An empty
// string means the subscriber connection dropped — the caller decides whether
// to resubscribe.
char* dex_redis_next_message(int h) {
    DexRedisConn* c = dex_redis_handle(h);
    if (!c || c->sub.fd < 0) return dex_redis_empty();

    for (;;) {
        DexRedisReply* r = dex_redis_read_reply(&c->sub);
        if (!r) {
            pthread_mutex_lock(&c->lock);
            dex_redis_sock_reset(&c->sub);
            pthread_mutex_unlock(&c->lock);
            return dex_redis_empty();
        }
        // A message is ["message", channel, payload]; subscribe confirmations
        // and pings arrive on the same socket and are skipped.
        if (r->type == '*' && r->nelems == 3 && r->elems[0]->str &&
            strcmp(r->elems[0]->str, "message") == 0) {
            if (r->elems[1]->str) {
                snprintf(c->sub_channel, sizeof(c->sub_channel), "%s", r->elems[1]->str);
            }
            char* out = r->elems[2]->str ? dex_redis_own(r->elems[2]->str, r->elems[2]->len) : dex_redis_empty();
            dex_redis_reply_free(r);
            return out;
        }
        dex_redis_reply_free(r);
    }
}

// Which channel the last message came in on, for a subscriber on several.
char* dex_redis_last_channel(int h) {
    DexRedisConn* c = dex_redis_handle(h);
    if (!c) return dex_redis_empty();
    return dex_redis_own(c->sub_channel, strlen(c->sub_channel));
}
