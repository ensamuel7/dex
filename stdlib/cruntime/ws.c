
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <errno.h>
#include <unistd.h>
#include <sys/socket.h>
#include <netinet/in.h>
#include <netdb.h>
#include <pthread.h>
#include <signal.h>

// --- SSL support (optional, requires OpenSSL) ---

#ifdef __has_include
  #if __has_include(<openssl/ssl.h>)
    #define DEX_SSL_AVAILABLE 1
  #endif
#endif

#ifdef DEX_HAS_SSL
  #define DEX_SSL_AVAILABLE 1
#endif

#ifdef DEX_SSL_AVAILABLE
#include <openssl/ssl.h>
#include <openssl/err.h>
#else
typedef void SSL;
typedef void SSL_CTX;
#endif

#ifdef DEX_SSL_AVAILABLE
static SSL_CTX* dex_ssl_ctx = NULL;

static SSL_CTX* dex_ssl_get_ctx(void) {
    if (!dex_ssl_ctx) {
        SSL_library_init();
        SSL_load_error_strings();
        OpenSSL_add_all_algorithms();
        dex_ssl_ctx = SSL_CTX_new(TLS_client_method());
    }
    return dex_ssl_ctx;
}
#endif

// --- SSL-aware I/O helpers ---

static int dex_ws_write(int fd, SSL* ssl, const void* buf, size_t len) {
#ifdef DEX_SSL_AVAILABLE
    if (ssl) {
        int r = SSL_write(ssl, buf, (int)len);
        return r > 0 ? r : -1;
    }
#endif
    (void)ssl;
    return (int)write(fd, buf, len);
}

static int dex_ws_read(int fd, SSL* ssl, void* buf, size_t len) {
#ifdef DEX_SSL_AVAILABLE
    if (ssl) {
        int r = SSL_read(ssl, buf, (int)len);
        return r > 0 ? r : -1;
    }
#endif
    (void)ssl;
    return (int)read(fd, buf, len);
}

static int dex_ws_read_exact(int fd, SSL* ssl, void* buf, size_t len) {
    size_t total = 0;
    while (total < len) {
        int r = dex_ws_read(fd, ssl, (char*)buf + total, len - total);
        if (r <= 0) return -1;
        total += (size_t)r;
    }
    return (int)total;
}

// --- Minimal SHA-1 implementation (RFC 3174) ---

static void dex_sha1(const unsigned char* msg, size_t len, unsigned char digest[20]) {
    uint32_t h0 = 0x67452301, h1 = 0xEFCDAB89, h2 = 0x98BADCFE, h3 = 0x10325476, h4 = 0xC3D2E1F0;
    size_t new_len = len + 1;
    while (new_len % 64 != 56) new_len++;
    unsigned char* padded = (unsigned char*)calloc(new_len + 8, 1);
    if (!padded) return;
    memcpy(padded, msg, len);
    padded[len] = 0x80;
    uint64_t bits = (uint64_t)len * 8;
    for (int i = 0; i < 8; i++) padded[new_len + 7 - i] = (unsigned char)(bits >> (i * 8));

    for (size_t offset = 0; offset < new_len + 8; offset += 64) {
        uint32_t w[80];
        for (int i = 0; i < 16; i++) {
            w[i] = ((uint32_t)padded[offset + i*4] << 24) |
                   ((uint32_t)padded[offset + i*4+1] << 16) |
                   ((uint32_t)padded[offset + i*4+2] << 8) |
                   ((uint32_t)padded[offset + i*4+3]);
        }
        for (int i = 16; i < 80; i++) {
            uint32_t t = w[i-3] ^ w[i-8] ^ w[i-14] ^ w[i-16];
            w[i] = (t << 1) | (t >> 31);
        }
        uint32_t a = h0, b = h1, c = h2, d = h3, e = h4;
        for (int i = 0; i < 80; i++) {
            uint32_t f, k;
            if (i < 20)      { f = (b & c) | ((~b) & d); k = 0x5A827999; }
            else if (i < 40) { f = b ^ c ^ d;            k = 0x6ED9EBA1; }
            else if (i < 60) { f = (b & c) | (b & d) | (c & d); k = 0x8F1BBCDC; }
            else              { f = b ^ c ^ d;            k = 0xCA62C1D6; }
            uint32_t temp = ((a << 5) | (a >> 27)) + f + e + k + w[i];
            e = d; d = c; c = (b << 30) | (b >> 2); b = a; a = temp;
        }
        h0 += a; h1 += b; h2 += c; h3 += d; h4 += e;
    }
    free(padded);
    uint32_t h[5] = {h0, h1, h2, h3, h4};
    for (int i = 0; i < 5; i++) {
        digest[i*4]   = (unsigned char)(h[i] >> 24);
        digest[i*4+1] = (unsigned char)(h[i] >> 16);
        digest[i*4+2] = (unsigned char)(h[i] >> 8);
        digest[i*4+3] = (unsigned char)(h[i]);
    }
}

// --- Base64 encode ---

static const char dex_b64_table[] = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";

static void dex_base64_encode(const unsigned char* in, size_t len, char* out) {
    size_t i, j;
    for (i = 0, j = 0; i < len; i += 3) {
        uint32_t v = (uint32_t)in[i] << 16;
        if (i + 1 < len) v |= (uint32_t)in[i+1] << 8;
        if (i + 2 < len) v |= (uint32_t)in[i+2];
        out[j++] = dex_b64_table[(v >> 18) & 0x3F];
        out[j++] = dex_b64_table[(v >> 12) & 0x3F];
        out[j++] = (i + 1 < len) ? dex_b64_table[(v >> 6) & 0x3F] : '=';
        out[j++] = (i + 2 < len) ? dex_b64_table[v & 0x3F] : '=';
    }
    out[j] = '\0';
}

// --- WebSocket protocol ---

static const char* DEX_WS_MAGIC = "258EAFA5-E914-47DA-95CA-5AB5DC11D697";

static void dex_ws_accept_key(const char* client_key, char* out_accept, size_t out_size) {
    char combined[256];
    snprintf(combined, sizeof(combined), "%s%s", client_key, DEX_WS_MAGIC);
    unsigned char digest[20];
    dex_sha1((unsigned char*)combined, strlen(combined), digest);
    dex_base64_encode(digest, 20, out_accept);
    (void)out_size;
}

// --- Subprotocol config ---

/* Pending registration state. A program may run several WS servers, each
 * configured and started from its own thread (`spawn { handler.start() }`), so
 * this is per-thread: a listener snapshots it into its own DexWsServer when it
 * starts, and one listener's configuration can never clobber another's. */
_Thread_local char dex_ws_subprotocol[128] = {0};

void dex_ws_set_protocol(const char* protocol) {
    strncpy(dex_ws_subprotocol, protocol, sizeof(dex_ws_subprotocol) - 1);
    dex_ws_subprotocol[sizeof(dex_ws_subprotocol) - 1] = '\0';
}

// Send a WebSocket text frame (SSL-aware)
static int dex_ws_frame_send(int fd, SSL* ssl, const char* payload, size_t len, int is_server) {
    unsigned char header[14];
    size_t hlen = 0;

    // FIN + opcode 0x1 (text)
    header[0] = 0x81;
    hlen = 1;

    unsigned char mask_bit = is_server ? 0x00 : 0x80;
    if (len < 126) {
        header[1] = mask_bit | (unsigned char)len;
        hlen = 2;
    } else if (len < 65536) {
        header[1] = mask_bit | 126;
        header[2] = (unsigned char)(len >> 8);
        header[3] = (unsigned char)(len & 0xFF);
        hlen = 4;
    } else {
        header[1] = mask_bit | 127;
        for (int i = 0; i < 8; i++) {
            header[2 + i] = (unsigned char)((len >> ((7 - i) * 8)) & 0xFF);
        }
        hlen = 10;
    }

    // Client frames must be masked (RFC 6455 section 5.3)
    unsigned char mask_key[4] = {0};
    if (!is_server) {
        for (int i = 0; i < 4; i++) mask_key[i] = (unsigned char)(rand() & 0xFF);
        memcpy(header + hlen, mask_key, 4);
        hlen += 4;
    }

    if (dex_ws_write(fd, ssl, header, hlen) < 0) return -1;

    if (!is_server && len > 0) {
        // Mask the payload
        unsigned char* masked = (unsigned char*)malloc(len);
        if (!masked) return -1;
        for (size_t i = 0; i < len; i++) {
            masked[i] = (unsigned char)payload[i] ^ mask_key[i % 4];
        }
        int r = dex_ws_write(fd, ssl, masked, len);
        free(masked);
        return r < 0 ? -1 : 0;
    }

    if (len > 0) {
        if (dex_ws_write(fd, ssl, payload, len) < 0) return -1;
    }
    return 0;
}

// Read a WebSocket frame, unmask if needed, return payload length (-1 on error/close)
static int dex_ws_frame_recv(int fd, SSL* ssl, char* buf, size_t bufsize, int* opcode_out) {
    unsigned char hdr[2];
    if (dex_ws_read_exact(fd, ssl, hdr, 2) < 0) return -1;

    int opcode = hdr[0] & 0x0F;
    int masked = (hdr[1] & 0x80) != 0;
    uint64_t payload_len = hdr[1] & 0x7F;

    if (payload_len == 126) {
        unsigned char ext[2];
        if (dex_ws_read_exact(fd, ssl, ext, 2) < 0) return -1;
        payload_len = ((uint64_t)ext[0] << 8) | ext[1];
    } else if (payload_len == 127) {
        unsigned char ext[8];
        if (dex_ws_read_exact(fd, ssl, ext, 8) < 0) return -1;
        payload_len = 0;
        for (int i = 0; i < 8; i++) payload_len = (payload_len << 8) | ext[i];
    }

    unsigned char mask_key[4] = {0};
    if (masked) {
        if (dex_ws_read_exact(fd, ssl, mask_key, 4) < 0) return -1;
    }

    if (payload_len >= bufsize) payload_len = bufsize - 1;

    size_t total_read = 0;
    while (total_read < payload_len) {
        int r = dex_ws_read(fd, ssl, buf + total_read, payload_len - total_read);
        if (r <= 0) return -1;
        total_read += (size_t)r;
    }

    if (masked) {
        for (size_t i = 0; i < payload_len; i++) {
            buf[i] ^= mask_key[i % 4];
        }
    }

    buf[payload_len] = '\0';
    if (opcode_out) *opcode_out = opcode;

    // Handle ping: auto-reply with pong
    if (opcode == 0x9) {
        unsigned char pong_hdr[2] = {0x8A, 0x00};
        dex_ws_write(fd, ssl, pong_hdr, 2);
        // Recursive call to get next real message
        return dex_ws_frame_recv(fd, ssl, buf, bufsize, opcode_out);
    }

    // Close frame
    if (opcode == 0x8) return -1;

    return (int)payload_len;
}

// --- WebSocket server (event-loop driven) ---

/* Pending handler registration — per-thread for the same reason as the
 * subprotocol above. Generated code assigns these directly before calling
 * dex_ws_listen(), which snapshots them into the listener it creates. */
_Thread_local void (*dex_ws_on_message)(Dex_Conn, DexString*) = NULL;
_Thread_local void (*dex_ws_on_connect)(Dex_Conn, DexString*) = NULL;
_Thread_local void (*dex_ws_on_disconnect)(Dex_Conn) = NULL;

typedef enum {
    WS_HANDSHAKE_READ,
    WS_HANDSHAKE_WRITE,
    WS_READING_FRAME,
    WS_DISPATCHED,
    WS_CLOSING
} DexWsState;

/* Write queue item for thread-safe sends */
typedef struct DexWsWriteItem {
    unsigned char*          data;
    int                     len;
    int                     pos;
    struct DexWsWriteItem*  next;
} DexWsWriteItem;

typedef struct DexWsServer DexWsServer;

typedef struct DexWsConn {
    int           fd;
    DexWsState    state;
    Dex_Conn      dex_conn;
    DexWsServer*  srv;   /* listener this connection was accepted by */
    int           close_requested; /* ws.close() called from a handler */

    /* Handshake read buffer */
    char*         read_buf;
    int           read_len;
    int           read_cap;

    /* Request path (extracted during handshake) */
    char          path[1024];

    /* Handshake write buffer */
    char*         hs_write_buf;
    int           hs_write_len;
    int           hs_write_pos;

    /* Frame parser state */
    unsigned char frame_hdr[14]; /* max header: 2 + 8 + 4 */
    int           frame_hdr_len;
    int           frame_hdr_need;
    int           frame_opcode;
    int           frame_masked;
    unsigned char frame_mask_key[4];
    uint64_t      frame_payload_len;
    char*         frame_payload;
    uint64_t      frame_payload_read;

    /* Write queue (thread-safe: protected by write_mutex) */
    DexWsWriteItem* write_head;
    DexWsWriteItem* write_tail;
    pthread_mutex_t write_mutex;

    /* Linked list for completed queue */
    struct DexWsConn* next;
} DexWsConn;

/* Per-listener server state. Each dex_ws_listen() owns one of these, so running
 * several WS servers in one process (e.g. OCPP 1.6 on one port and 2.0.1 on
 * another) keeps its own event loop, thread pool and handlers. */
struct DexWsServer {
    DexEventLoop*  loop;
    DexThreadPool* pool;
    DexNotifyPipe  notify;
    int            server_fd;

    void (*on_message)(Dex_Conn, DexString*);
    void (*on_connect)(Dex_Conn, DexString*);
    void (*on_disconnect)(Dex_Conn);
    char subprotocol[128];

    /* Completed message dispatches (worker → event loop) */
    DexWsConn*      completed_head;
    pthread_mutex_t completed_mutex;

    /* Highest fd this server has ever accepted. The notify sweep below walks the
     * connection table, and fds are small in practice, so this keeps that walk
     * proportional to the connections actually in use instead of the table's
     * 65536-slot capacity. Only ever grows, and only the event-loop thread
     * writes it. */
    int             max_fd;
};

/* Connection table indexed by fd. This one stays process-wide: fds are unique
 * across listeners, so there is nothing to collide, and ws.send() needs to find
 * a connection knowing only its fd. */
#define DEX_WS_MAX_FD 65536
static DexWsConn*     dex_ws_conn_table[DEX_WS_MAX_FD];

static DexWsConn* dex_ws_conn_new(DexWsServer* srv, int fd) {
    DexWsConn* conn = (DexWsConn*)calloc(1, sizeof(DexWsConn));
    if (!conn) return NULL;
    conn->srv = srv;
    conn->fd = fd;
    conn->state = WS_HANDSHAKE_READ;
    conn->read_cap = 4096;
    conn->read_buf = (char*)malloc(conn->read_cap);
    if (!conn->read_buf) {
        free(conn);
        return NULL;
    }
    conn->read_len = 0;
    strcpy(conn->path, "/");
    conn->dex_conn.fd = fd;
    conn->dex_conn.isServer = 1;
    conn->dex_conn.ssl = 0;
    pthread_mutex_init(&conn->write_mutex, NULL);
    if (fd >= 0 && fd < DEX_WS_MAX_FD) {
        dex_ws_conn_table[fd] = conn;
        if (srv && fd > srv->max_fd) srv->max_fd = fd;
    }
    return conn;
}

static void dex_ws_conn_free(DexWsConn* conn) {
    if (!conn) return;
    if (conn->fd >= 0 && conn->fd < DEX_WS_MAX_FD) {
        dex_ws_conn_table[conn->fd] = NULL;
    }
    free(conn->read_buf);
    free(conn->hs_write_buf);
    free(conn->frame_payload);
    /* Free write queue */
    DexWsWriteItem* item = conn->write_head;
    while (item) {
        DexWsWriteItem* next = item->next;
        free(item->data);
        free(item);
        item = next;
    }
    pthread_mutex_destroy(&conn->write_mutex);
    free(conn);
}

/* Build a WebSocket frame into a buffer (for queued writes).
 * Server frames are unmasked. Returns allocated buffer + length. */
static unsigned char* dex_ws_build_frame(const char* payload, size_t len, int* out_len) {
    int hlen;
    unsigned char hdr[10];

    hdr[0] = 0x81; /* FIN + text opcode */
    if (len < 126) {
        hdr[1] = (unsigned char)len;
        hlen = 2;
    } else if (len < 65536) {
        hdr[1] = 126;
        hdr[2] = (unsigned char)(len >> 8);
        hdr[3] = (unsigned char)(len & 0xFF);
        hlen = 4;
    } else {
        hdr[1] = 127;
        for (int i = 0; i < 8; i++) {
            hdr[2 + i] = (unsigned char)((len >> ((7 - i) * 8)) & 0xFF);
        }
        hlen = 10;
    }

    int total = hlen + (int)len;
    unsigned char* buf = (unsigned char*)malloc(total);
    if (!buf) {
        *out_len = 0;
        return NULL;
    }
    memcpy(buf, hdr, hlen);
    if (len > 0) memcpy(buf + hlen, payload, len);
    *out_len = total;
    return buf;
}

/* Reset frame parser state for next frame */
static void dex_ws_reset_frame_parser(DexWsConn* conn) {
    conn->frame_hdr_len = 0;
    conn->frame_hdr_need = 2; /* minimum header: 2 bytes */
    conn->frame_opcode = 0;
    conn->frame_masked = 0;
    memset(conn->frame_mask_key, 0, 4);
    conn->frame_payload_len = 0;
    free(conn->frame_payload);
    conn->frame_payload = NULL;
    conn->frame_payload_read = 0;
}

/* Incremental frame parser. Returns:
 *   1 = complete frame available
 *   0 = need more data
 *  -1 = error / close */
static int dex_ws_parse_frame_incremental(DexWsConn* conn) {
    /* Phase 1: read frame header */
    if (conn->frame_hdr_len < conn->frame_hdr_need) {
        int need = conn->frame_hdr_need - conn->frame_hdr_len;
        int r = (int)read(conn->fd, conn->frame_hdr + conn->frame_hdr_len, need);
        if (r <= 0) {
            if (r < 0 && (errno == EAGAIN || errno == EWOULDBLOCK)) return 0;
            return -1;
        }
        conn->frame_hdr_len += r;
        if (conn->frame_hdr_len < conn->frame_hdr_need) return 0;

        /* After first 2 bytes, determine extended length */
        if (conn->frame_hdr_need == 2) {
            conn->frame_opcode = conn->frame_hdr[0] & 0x0F;
            conn->frame_masked = (conn->frame_hdr[1] & 0x80) != 0;
            uint64_t base_len = conn->frame_hdr[1] & 0x7F;

            int extra = 0;
            if (base_len == 126) extra = 2;
            else if (base_len == 127) extra = 8;
            if (conn->frame_masked) extra += 4;

            if (extra > 0) {
                conn->frame_hdr_need = 2 + extra;
                return dex_ws_parse_frame_incremental(conn); /* try to read more */
            }

            /* No extended length */
            conn->frame_payload_len = base_len;
        } else {
            /* Extended length bytes are available */
            uint64_t base_len = conn->frame_hdr[1] & 0x7F;
            int offset = 2;
            if (base_len == 126) {
                conn->frame_payload_len = ((uint64_t)conn->frame_hdr[2] << 8) |
                                           (uint64_t)conn->frame_hdr[3];
                offset = 4;
            } else if (base_len == 127) {
                conn->frame_payload_len = 0;
                for (int i = 0; i < 8; i++) {
                    conn->frame_payload_len = (conn->frame_payload_len << 8) | conn->frame_hdr[2 + i];
                }
                offset = 10;
            } else {
                conn->frame_payload_len = base_len;
            }
            if (conn->frame_masked) {
                memcpy(conn->frame_mask_key, conn->frame_hdr + offset, 4);
            }
        }

        /* Cap payload to 64KB for safety */
        if (conn->frame_payload_len > 65535) conn->frame_payload_len = 65535;

        /* Allocate payload buffer */
        conn->frame_payload = (char*)malloc(conn->frame_payload_len + 1);
        if (!conn->frame_payload) return -1;
        conn->frame_payload_read = 0;

        if (conn->frame_payload_len == 0) {
            conn->frame_payload[0] = '\0';
            return 1; /* empty frame */
        }
    }

    /* Phase 2: read payload */
    if (conn->frame_payload_read < conn->frame_payload_len) {
        uint64_t remaining = conn->frame_payload_len - conn->frame_payload_read;
        int r = (int)read(conn->fd, conn->frame_payload + conn->frame_payload_read,
                          (size_t)(remaining > 8192 ? 8192 : remaining));
        if (r <= 0) {
            if (r < 0 && (errno == EAGAIN || errno == EWOULDBLOCK)) return 0;
            return -1;
        }
        conn->frame_payload_read += (uint64_t)r;
    }

    if (conn->frame_payload_read < conn->frame_payload_len) return 0;

    /* Unmask if needed */
    if (conn->frame_masked) {
        for (uint64_t i = 0; i < conn->frame_payload_len; i++) {
            conn->frame_payload[i] ^= conn->frame_mask_key[i % 4];
        }
    }
    conn->frame_payload[conn->frame_payload_len] = '\0';
    return 1;
}

/* Message dispatch work item */
typedef struct {
    DexWsConn* conn;
    char*      message;
} DexWsWorkItem;

static void dex_ws_worker_func(void* arg) {
    DexWsWorkItem* item = (DexWsWorkItem*)arg;
    DexWsConn* conn = item->conn;
    DexWsServer* srv = conn->srv;

    if (srv->on_message) {
        DexString* msg = dex_string_from_cstr(item->message);
        item->message = NULL; /* dex_string_from_cstr already freed it */
        srv->on_message(conn->dex_conn, msg);
        dex_release(msg);
    }

    free(item->message); /* NULL if handler ran, otherwise free the raw buffer */
    free(item);

    /* Signal event loop that this connection is ready to read again */
    pthread_mutex_lock(&srv->completed_mutex);
    conn->next = srv->completed_head;
    srv->completed_head = conn;
    pthread_mutex_unlock(&srv->completed_mutex);
    dex_notify_pipe_signal(&srv->notify);
}

/* Send a WebSocket close frame, then tear the connection down. Used when a
 * handler called ws.close(): the peer sees a clean close rather than the
 * connection simply going quiet. */
static void dex_ws_ev_close_conn(DexWsConn* conn);

static int dex_ws_flush_writes(DexWsConn* conn);

static void dex_ws_ev_finish_close(DexWsConn* conn) {
    /* Deliver anything the handler queued before asking to close — a final
     * reply is a normal thing to send on the way out. Bounded so a peer that
     * has stopped reading cannot wedge the event loop; whatever is still
     * unsent at that point is dropped along with the connection. */
    for (int i = 0; i < 64 && dex_ws_flush_writes(conn) == 1; i++) {
        /* partial write — retry */
    }
    static const unsigned char close_frame[2] = {0x88, 0x00};
    dex_ws_write(conn->fd, (SSL*)conn->dex_conn.ssl, close_frame, sizeof(close_frame));

    /* Half-close first. close() on a socket still holding unread input resets
     * the connection and throws away everything queued for sending, which would
     * lose the close frame we just wrote; shutting the write side down pushes it
     * out and sends FIN before the teardown below releases the fd. */
    shutdown(conn->fd, SHUT_WR);
    dex_ws_ev_close_conn(conn);
}

/* Close a server-side WS connection from within the event loop */
static void dex_ws_ev_close_conn(DexWsConn* conn) {
    DexWsServer* srv = conn->srv;
    if (srv->on_disconnect) {
        srv->on_disconnect(conn->dex_conn);
    }
    dex_ev_del(srv->loop, conn->fd);
    close(conn->fd);
    dex_ws_conn_free(conn);
}

/* Try to flush queued write items. Returns 0 if all flushed, 1 if more to write, -1 on error. */
static int dex_ws_flush_writes(DexWsConn* conn) {
    pthread_mutex_lock(&conn->write_mutex);
    while (conn->write_head) {
        DexWsWriteItem* item = conn->write_head;
        int remaining = item->len - item->pos;
        int w = (int)write(conn->fd, item->data + item->pos, remaining);
        if (w <= 0) {
            if (w < 0 && (errno == EAGAIN || errno == EWOULDBLOCK)) {
                pthread_mutex_unlock(&conn->write_mutex);
                return 1; /* need to wait for writable */
            }
            pthread_mutex_unlock(&conn->write_mutex);
            return -1;
        }
        item->pos += w;
        if (item->pos >= item->len) {
            conn->write_head = item->next;
            if (!conn->write_head) conn->write_tail = NULL;
            free(item->data);
            free(item);
        } else {
            pthread_mutex_unlock(&conn->write_mutex);
            return 1; /* partial write */
        }
    }
    pthread_mutex_unlock(&conn->write_mutex);
    return 0;
}

/* Listening fds, recorded so the signal handler can close every server, not
 * just the most recently started one. */
#define DEX_WS_MAX_SERVERS 32
static volatile int dex_ws_server_fds[DEX_WS_MAX_SERVERS];
static volatile int dex_ws_server_fd_count = 0;

static void dex_ws_shutdown_handler(int sig) {
    (void)sig;
    int n = dex_ws_server_fd_count;
    for (int i = 0; i < n && i < DEX_WS_MAX_SERVERS; i++) {
        if (dex_ws_server_fds[i] >= 0) close(dex_ws_server_fds[i]);
    }
    _exit(0);
}

void dex_ws_listen(int port) {
    signal(SIGPIPE, SIG_IGN);
    signal(SIGINT, dex_ws_shutdown_handler);
    signal(SIGTERM, dex_ws_shutdown_handler);

    int server_fd = socket(AF_INET, SOCK_STREAM, 0);
    if (server_fd < 0) { perror("ws socket"); return; }

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
        perror("ws bind"); close(server_fd); return;
    }
    if (listen(server_fd, 1024) < 0) {
        perror("ws listen"); close(server_fd); return;
    }

    dex_set_nonblocking(server_fd);

    /* This listener's own state: event loop, thread pool, notify pipe and the
     * handlers registered on this thread before listen() was called. */
    DexWsServer* srv = (DexWsServer*)calloc(1, sizeof(DexWsServer));
    if (!srv) { perror("ws server alloc"); close(server_fd); return; }
    srv->server_fd    = server_fd;
    srv->loop         = dex_ev_create(1024);
    srv->pool         = dex_pool_create(0);
    srv->on_message   = dex_ws_on_message;
    srv->on_connect   = dex_ws_on_connect;
    srv->on_disconnect= dex_ws_on_disconnect;
    memcpy(srv->subprotocol, dex_ws_subprotocol, sizeof(srv->subprotocol));
    pthread_mutex_init(&srv->completed_mutex, NULL);
    dex_notify_pipe_init(&srv->notify);

    if (dex_ws_server_fd_count < DEX_WS_MAX_SERVERS) {
        dex_ws_server_fds[dex_ws_server_fd_count++] = server_fd;
    }

    dex_ev_add(srv->loop, server_fd, DEX_EV_READ, NULL);
    dex_ev_add(srv->loop, srv->notify.read_fd, DEX_EV_READ, (void*)(intptr_t)-2);

    printf("Dex WebSocket server listening on port %d\n", port);
    fflush(stdout);

    DexEvent events[256];

    for (;;) {
        int n = dex_ev_wait(srv->loop, events, 256, -1);

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
                    DexWsConn* conn = dex_ws_conn_new(srv, client_fd);
                    if (!conn) {
                        close(client_fd);
                        break;
                    }
                    dex_ev_add(srv->loop, client_fd, DEX_EV_READ, conn);
                }
                continue;
            }

            /* Notify pipe: drain and process completed dispatches */
            if (ev->user_data == (void*)(intptr_t)-2) {
                dex_notify_pipe_drain(&srv->notify);

                pthread_mutex_lock(&srv->completed_mutex);
                DexWsConn* completed = srv->completed_head;
                srv->completed_head = NULL;
                pthread_mutex_unlock(&srv->completed_mutex);

                while (completed) {
                    DexWsConn* next = completed->next;
                    completed->next = NULL;
                    completed->state = WS_READING_FRAME;
                    dex_ws_reset_frame_parser(completed);
                    /* Handler closed this connection while dispatching */
                    if (completed->close_requested) {
                        dex_ws_ev_finish_close(completed);
                        completed = next;
                        continue;
                    }
                    /* Re-register for read (and write if pending) */
                    int ev_flags = DEX_EV_READ;
                    pthread_mutex_lock(&completed->write_mutex);
                    if (completed->write_head) ev_flags |= DEX_EV_WRITE;
                    pthread_mutex_unlock(&completed->write_mutex);
                    dex_ev_add(srv->loop, completed->fd, ev_flags, completed);
                    completed = next;
                }

                /* Also check all connections for pending writes from dex_ws_send,
                 * and for closes requested off the event-loop thread.
                 * (both signal the notify pipe) */
                for (int j = 0; j <= srv->max_fd; j++) {
                    DexWsConn* wc = dex_ws_conn_table[j];
                    if (wc && wc->srv == srv && wc->state == WS_READING_FRAME) {
                        if (wc->close_requested) {
                            dex_ws_ev_finish_close(wc);
                            continue;
                        }
                        pthread_mutex_lock(&wc->write_mutex);
                        int has_writes = (wc->write_head != NULL);
                        pthread_mutex_unlock(&wc->write_mutex);
                        if (has_writes) {
                            dex_ev_mod(srv->loop, wc->fd, DEX_EV_READ | DEX_EV_WRITE, wc);
                        }
                    }
                }
                continue;
            }

            /* Client connection */
            DexWsConn* conn = (DexWsConn*)ev->user_data;

            if (ev->events & DEX_EV_ERROR) {
                dex_ws_ev_close_conn(conn);
                continue;
            }

            /* Writable: flush queued writes */
            if (ev->events & DEX_EV_WRITE) {
                int fr = dex_ws_flush_writes(conn);
                if (fr < 0) {
                    dex_ws_ev_close_conn(conn);
                    continue;
                }
                if (fr == 0) {
                    /* All writes flushed, go back to read-only */
                    if (conn->state == WS_HANDSHAKE_WRITE) {
                        /* Handshake sent — transition to reading frames */
                        conn->state = WS_READING_FRAME;
                        dex_ws_reset_frame_parser(conn);

                        /* Call on_connect callback */
                        if (srv->on_connect) {
                            DexString* path_str = dex_string_new(conn->path, strlen(conn->path));
                            srv->on_connect(conn->dex_conn, path_str);
                            dex_release(path_str);
                        }
                        /* Handler rejected this client via ws.close() */
                        if (conn->close_requested) {
                            dex_ws_ev_finish_close(conn);
                            continue;
                        }
                    }
                    dex_ev_mod(srv->loop, conn->fd, DEX_EV_READ, conn);
                }
            }

            /* Readable */
            if (ev->events & DEX_EV_READ) {
                /* Handshake: accumulate HTTP upgrade request */
                if (conn->state == WS_HANDSHAKE_READ) {
                    if (conn->read_len >= conn->read_cap - 1) {
                        int new_cap = conn->read_cap * 2;
                        char* new_buf = (char*)realloc(conn->read_buf, new_cap);
                        if (!new_buf) {
                            dex_ws_ev_close_conn(conn);
                            continue;
                        }
                        conn->read_buf = new_buf;
                        conn->read_cap = new_cap;
                    }
                    int r = (int)read(conn->fd, conn->read_buf + conn->read_len,
                                      conn->read_cap - conn->read_len - 1);
                    if (r <= 0) {
                        if (r < 0 && (errno == EAGAIN || errno == EWOULDBLOCK)) continue;
                        dex_ws_ev_close_conn(conn);
                        continue;
                    }
                    conn->read_len += r;
                    conn->read_buf[conn->read_len] = '\0';

                    /* Check if headers are complete */
                    if (!strstr(conn->read_buf, "\r\n\r\n")) continue;

                    /* Validate WebSocket upgrade */
                    if (!strstr(conn->read_buf, "Upgrade: websocket") &&
                        !strstr(conn->read_buf, "Upgrade: WebSocket")) {
                        const char* bad = "HTTP/1.1 400 Bad Request\r\nContent-Length: 0\r\n\r\n";
                        write(conn->fd, bad, strlen(bad));
                        dex_ws_ev_close_conn(conn);
                        continue;
                    }

                    /* Extract path */
                    if (strncmp(conn->read_buf, "GET ", 4) == 0) {
                        const char* pstart = conn->read_buf + 4;
                        const char* pend = strchr(pstart, ' ');
                        if (pend) {
                            size_t plen = (size_t)(pend - pstart);
                            if (plen >= sizeof(conn->path)) plen = sizeof(conn->path) - 1;
                            memcpy(conn->path, pstart, plen);
                            conn->path[plen] = '\0';
                        }
                    }

                    /* Extract Sec-WebSocket-Key */
                    const char* key_hdr = strstr(conn->read_buf, "Sec-WebSocket-Key:");
                    if (!key_hdr) {
                        dex_ws_ev_close_conn(conn);
                        continue;
                    }
                    key_hdr += 18;
                    while (*key_hdr == ' ') key_hdr++;
                    char ws_key[64] = {0};
                    int ki = 0;
                    while (key_hdr[ki] && key_hdr[ki] != '\r' && key_hdr[ki] != '\n' && ki < 63) {
                        ws_key[ki] = key_hdr[ki];
                        ki++;
                    }
                    ws_key[ki] = '\0';

                    char accept_key[64];
                    dex_ws_accept_key(ws_key, accept_key, sizeof(accept_key));

                    /* Build handshake response */
                    char response[512];
                    int rlen;
                    if (srv->subprotocol[0] != '\0') {
                        rlen = snprintf(response, sizeof(response),
                            "HTTP/1.1 101 Switching Protocols\r\n"
                            "Upgrade: websocket\r\n"
                            "Connection: Upgrade\r\n"
                            "Sec-WebSocket-Accept: %s\r\n"
                            "Sec-WebSocket-Protocol: %s\r\n"
                            "\r\n", accept_key, srv->subprotocol);
                    } else {
                        rlen = snprintf(response, sizeof(response),
                            "HTTP/1.1 101 Switching Protocols\r\n"
                            "Upgrade: websocket\r\n"
                            "Connection: Upgrade\r\n"
                            "Sec-WebSocket-Accept: %s\r\n"
                            "\r\n", accept_key);
                    }

                    /* Queue handshake response as a write item */
                    DexWsWriteItem* wi = (DexWsWriteItem*)calloc(1, sizeof(DexWsWriteItem));
                    if (!wi) {
                        dex_ws_ev_close_conn(conn);
                        continue;
                    }
                    wi->data = (unsigned char*)malloc(rlen);
                    if (!wi->data) {
                        free(wi);
                        dex_ws_ev_close_conn(conn);
                        continue;
                    }
                    memcpy(wi->data, response, rlen);
                    wi->len = rlen;
                    wi->pos = 0;
                    conn->write_head = wi;
                    conn->write_tail = wi;

                    conn->state = WS_HANDSHAKE_WRITE;
                    dex_ev_mod(srv->loop, conn->fd, DEX_EV_WRITE, conn);
                    continue;
                }

                /* Frame reading */
                if (conn->state == WS_READING_FRAME) {
                    int result = dex_ws_parse_frame_incremental(conn);
                    if (result < 0) {
                        dex_ws_ev_close_conn(conn);
                        continue;
                    }
                    if (result == 0) continue; /* need more data */

                    /* Complete frame */
                    int opcode = conn->frame_opcode;

                    /* Ping: auto-reply with pong inline */
                    if (opcode == 0x9) {
                        unsigned char pong[2] = {0x8A, 0x00};
                        write(conn->fd, pong, 2);
                        dex_ws_reset_frame_parser(conn);
                        continue;
                    }

                    /* Close frame */
                    if (opcode == 0x8) {
                        dex_ws_ev_close_conn(conn);
                        continue;
                    }

                    /* Text frame: dispatch to worker pool */
                    if (opcode == 0x1 && srv->on_message) {
                        conn->state = WS_DISPATCHED;
                        dex_ev_del(srv->loop, conn->fd);

                        DexWsWorkItem* item = (DexWsWorkItem*)malloc(sizeof(DexWsWorkItem));
                        if (!item) {
                            dex_ws_reset_frame_parser(conn);
                            continue;
                        }
                        item->conn = conn;
                        item->message = conn->frame_payload;
                        conn->frame_payload = NULL; /* ownership transferred */
                        dex_pool_submit(srv->pool, dex_ws_worker_func, item);
                    } else {
                        /* Unknown opcode or no handler, skip */
                        dex_ws_reset_frame_parser(conn);
                    }
                }
            }
        }
    }
}

// --- WebSocket client ---

Dex_Conn dex_ws_connect(const char* url) {
    Dex_Conn conn = {-1, 0, 0};

    // Detect wss:// vs ws://
    int use_ssl = 0;
    const char* p = url;
    if (strncmp(p, "wss://", 6) == 0) {
        use_ssl = 1;
        p += 6;
    } else if (strncmp(p, "ws://", 5) == 0) {
        p += 5;
    }

    char host[256] = {0};
    int port = use_ssl ? 443 : 80;
    char path[1024] = "/";

    const char* colon = strchr(p, ':');
    const char* slash = strchr(p, '/');

    if (colon && (!slash || colon < slash)) {
        size_t hlen = (size_t)(colon - p);
        if (hlen >= sizeof(host)) hlen = sizeof(host) - 1;
        memcpy(host, p, hlen);
        host[hlen] = '\0';
        port = atoi(colon + 1);
        if (slash) {
            strncpy(path, slash, sizeof(path) - 1);
        }
    } else if (slash) {
        size_t hlen = (size_t)(slash - p);
        if (hlen >= sizeof(host)) hlen = sizeof(host) - 1;
        memcpy(host, p, hlen);
        host[hlen] = '\0';
        strncpy(path, slash, sizeof(path) - 1);
    } else {
        strncpy(host, p, sizeof(host) - 1);
    }

    // Resolve host
    struct hostent* he = gethostbyname(host);
    if (!he) return conn;

    int fd = socket(AF_INET, SOCK_STREAM, 0);
    if (fd < 0) return conn;

    struct sockaddr_in addr;
    addr.sin_family = AF_INET;
    addr.sin_port = htons(port);
    memcpy(&addr.sin_addr, he->h_addr_list[0], (size_t)he->h_length);

    if (connect(fd, (struct sockaddr*)&addr, sizeof(addr)) < 0) {
        close(fd);
        return conn;
    }

    // SSL handshake if wss://
    SSL* ssl_ptr = NULL;
#ifdef DEX_SSL_AVAILABLE
    if (use_ssl) {
        SSL_CTX* ctx = dex_ssl_get_ctx();
        if (!ctx) { close(fd); return conn; }
        ssl_ptr = SSL_new(ctx);
        if (!ssl_ptr) { close(fd); return conn; }
        SSL_set_fd(ssl_ptr, fd);
        SSL_set_tlsext_host_name(ssl_ptr, host);
        if (SSL_connect(ssl_ptr) <= 0) {
            SSL_free(ssl_ptr);
            close(fd);
            return conn;
        }
    }
#else
    if (use_ssl) {
        fprintf(stderr, "wss:// not supported (compiled without OpenSSL)\n");
        close(fd);
        return conn;
    }
#endif

    // Generate a random key (16 bytes base64-encoded)
    unsigned char raw_key[16];
    for (int i = 0; i < 16; i++) raw_key[i] = (unsigned char)(rand() & 0xFF);
    char ws_key[32];
    dex_base64_encode(raw_key, 16, ws_key);

    // Send upgrade request
    char request[1024];
    int reqlen;
    if (dex_ws_subprotocol[0] != '\0') {
        reqlen = snprintf(request, sizeof(request),
            "GET %s HTTP/1.1\r\n"
            "Host: %s:%d\r\n"
            "Upgrade: websocket\r\n"
            "Connection: Upgrade\r\n"
            "Sec-WebSocket-Key: %s\r\n"
            "Sec-WebSocket-Version: 13\r\n"
            "Sec-WebSocket-Protocol: %s\r\n"
            "\r\n", path, host, port, ws_key, dex_ws_subprotocol);
    } else {
        reqlen = snprintf(request, sizeof(request),
            "GET %s HTTP/1.1\r\n"
            "Host: %s:%d\r\n"
            "Upgrade: websocket\r\n"
            "Connection: Upgrade\r\n"
            "Sec-WebSocket-Key: %s\r\n"
            "Sec-WebSocket-Version: 13\r\n"
            "\r\n", path, host, port, ws_key);
    }
    dex_ws_write(fd, ssl_ptr, request, reqlen);

    // Read response
    char resp[4096];
    int rn = dex_ws_read(fd, ssl_ptr, resp, sizeof(resp) - 1);
    if (rn <= 0 || !strstr(resp, "101")) {
#ifdef DEX_SSL_AVAILABLE
        if (ssl_ptr) { SSL_shutdown(ssl_ptr); SSL_free(ssl_ptr); }
#endif
        close(fd);
        return conn;
    }
    resp[rn] = '\0';

    conn.fd = fd;
    conn.isServer = 0;
    conn.ssl = (long)ssl_ptr;
    return conn;
}

void dex_ws_send(Dex_Conn conn, const char* msg) {
    if (conn.fd < 0) return;

    /* Server-side connections use queued writes (thread-safe) */
    if (conn.isServer && conn.fd < DEX_WS_MAX_FD && dex_ws_conn_table[conn.fd]) {
        DexWsConn* wc = dex_ws_conn_table[conn.fd];
        int frame_len;
        unsigned char* frame_data = dex_ws_build_frame(msg, strlen(msg), &frame_len);
        if (!frame_data) return;

        DexWsWriteItem* item = (DexWsWriteItem*)calloc(1, sizeof(DexWsWriteItem));
        if (!item) {
            free(frame_data);
            return;
        }
        item->data = frame_data;
        item->len = frame_len;
        item->pos = 0;
        item->next = NULL;

        pthread_mutex_lock(&wc->write_mutex);
        if (wc->write_tail) {
            wc->write_tail->next = item;
        } else {
            wc->write_head = item;
        }
        wc->write_tail = item;
        pthread_mutex_unlock(&wc->write_mutex);

        /* Signal the owning listener's event loop to register for write */
        dex_notify_pipe_signal(&wc->srv->notify);
        return;
    }

    /* Client-side: direct blocking write (unchanged) */
    dex_ws_frame_send(conn.fd, (SSL*)conn.ssl, msg, strlen(msg), conn.isServer);
}

DexString* dex_ws_receive(Dex_Conn conn) {
    if (conn.fd < 0) return dex_string_from_lit("");
    char buf[65536];
    int opcode = 0;
    int n = dex_ws_frame_recv(conn.fd, (SSL*)conn.ssl, buf, sizeof(buf), &opcode);
    if (n < 0) return dex_string_from_lit("");
    return dex_string_from_cstr(buf);
}

void dex_ws_close(Dex_Conn conn) {
    if (conn.fd < 0) return;

    /* Server-side connections are owned by an event loop: closing the fd here
     * would leave the loop holding a freed registration and risk a double close
     * once the fd is reused. Flag it instead and let the loop tear it down at a
     * safe point. Works from a handler or any other thread. */
    if (conn.isServer && conn.fd < DEX_WS_MAX_FD) {
        DexWsConn* wc = dex_ws_conn_table[conn.fd];
        if (wc) {
            wc->close_requested = 1;
            dex_notify_pipe_signal(&wc->srv->notify);
            return;
        }
    }

    // Send close frame
    unsigned char close_frame[2] = {0x88, 0x00};
    dex_ws_write(conn.fd, (SSL*)conn.ssl, close_frame, 2);
#ifdef DEX_SSL_AVAILABLE
    if (conn.ssl) {
        SSL_shutdown((SSL*)conn.ssl);
        SSL_free((SSL*)conn.ssl);
    }
#endif
    shutdown(conn.fd, SHUT_RDWR);
    close(conn.fd);
}
