
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <sys/socket.h>
#include <netinet/in.h>
#include <netdb.h>
#include <pthread.h>
#include <signal.h>

// --- Minimal SHA-1 implementation (RFC 3174) ---

static void dex_sha1(const unsigned char* msg, size_t len, unsigned char digest[20]) {
    uint32_t h0 = 0x67452301, h1 = 0xEFCDAB89, h2 = 0x98BADCFE, h3 = 0x10325476, h4 = 0xC3D2E1F0;
    size_t new_len = len + 1;
    while (new_len % 64 != 56) new_len++;
    unsigned char* padded = (unsigned char*)calloc(new_len + 8, 1);
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

// Send a WebSocket text frame
static int dex_ws_frame_send(int fd, const char* payload, size_t len, int is_server) {
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

    if (write(fd, header, hlen) < 0) return -1;

    if (!is_server && len > 0) {
        // Mask the payload
        unsigned char* masked = (unsigned char*)malloc(len);
        for (size_t i = 0; i < len; i++) {
            masked[i] = (unsigned char)payload[i] ^ mask_key[i % 4];
        }
        int r = (int)write(fd, masked, len);
        free(masked);
        return r < 0 ? -1 : 0;
    }

    if (len > 0) {
        if (write(fd, payload, len) < 0) return -1;
    }
    return 0;
}

// Read a WebSocket frame, unmask if needed, return payload length (-1 on error/close)
static int dex_ws_frame_recv(int fd, char* buf, size_t bufsize, int* opcode_out) {
    unsigned char hdr[2];
    if (read(fd, hdr, 2) != 2) return -1;

    int opcode = hdr[0] & 0x0F;
    int masked = (hdr[1] & 0x80) != 0;
    uint64_t payload_len = hdr[1] & 0x7F;

    if (payload_len == 126) {
        unsigned char ext[2];
        if (read(fd, ext, 2) != 2) return -1;
        payload_len = ((uint64_t)ext[0] << 8) | ext[1];
    } else if (payload_len == 127) {
        unsigned char ext[8];
        if (read(fd, ext, 8) != 8) return -1;
        payload_len = 0;
        for (int i = 0; i < 8; i++) payload_len = (payload_len << 8) | ext[i];
    }

    unsigned char mask_key[4] = {0};
    if (masked) {
        if (read(fd, mask_key, 4) != 4) return -1;
    }

    if (payload_len >= bufsize) payload_len = bufsize - 1;

    size_t total_read = 0;
    while (total_read < payload_len) {
        ssize_t r = read(fd, buf + total_read, payload_len - total_read);
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
        write(fd, pong_hdr, 2);
        // Recursive call to get next real message
        return dex_ws_frame_recv(fd, buf, bufsize, opcode_out);
    }

    // Close frame
    if (opcode == 0x8) return -1;

    return (int)payload_len;
}

// --- WebSocket server ---

static void (*dex_ws_on_message)(Dex_Conn, DexString*) = NULL;

static void dex_ws_handle_connection(int client_fd) {
    char buf[8192];
    int n = (int)read(client_fd, buf, sizeof(buf) - 1);
    if (n <= 0) { close(client_fd); return; }
    buf[n] = '\0';

    // Check for WebSocket upgrade
    if (!strstr(buf, "Upgrade: websocket") && !strstr(buf, "Upgrade: WebSocket")) {
        const char* resp = "HTTP/1.1 400 Bad Request\r\nContent-Length: 0\r\n\r\n";
        write(client_fd, resp, strlen(resp));
        close(client_fd);
        return;
    }

    // Extract Sec-WebSocket-Key
    const char* key_hdr = strstr(buf, "Sec-WebSocket-Key:");
    if (!key_hdr) { close(client_fd); return; }
    key_hdr += 18;
    while (*key_hdr == ' ') key_hdr++;
    char ws_key[64] = {0};
    int ki = 0;
    while (key_hdr[ki] && key_hdr[ki] != '\r' && key_hdr[ki] != '\n' && ki < 63) {
        ws_key[ki] = key_hdr[ki];
        ki++;
    }
    ws_key[ki] = '\0';

    // Compute accept key
    char accept_key[64];
    dex_ws_accept_key(ws_key, accept_key, sizeof(accept_key));

    // Send handshake response
    char response[512];
    int rlen = snprintf(response, sizeof(response),
        "HTTP/1.1 101 Switching Protocols\r\n"
        "Upgrade: websocket\r\n"
        "Connection: Upgrade\r\n"
        "Sec-WebSocket-Accept: %s\r\n"
        "\r\n", accept_key);
    write(client_fd, response, rlen);

    // Enter message loop
    Dex_Conn conn;
    conn.fd = client_fd;
    conn.isServer = 1;

    char msg_buf[65536];
    for (;;) {
        int opcode = 0;
        int msg_len = dex_ws_frame_recv(client_fd, msg_buf, sizeof(msg_buf), &opcode);
        if (msg_len < 0) break;
        if (opcode == 0x1 && dex_ws_on_message) {
            DexString* msg = dex_string_from_cstr(msg_buf);
            dex_ws_on_message(conn, msg);
            dex_release(msg);
        }
    }
    close(client_fd);
}

static void* dex_ws_worker(void* arg) {
    int client_fd = *(int*)arg;
    free(arg);
    dex_ws_handle_connection(client_fd);
    return NULL;
}

static volatile int dex_ws_server_fd = -1;

static void dex_ws_shutdown_handler(int sig) {
    (void)sig;
    if (dex_ws_server_fd >= 0) close(dex_ws_server_fd);
    _exit(0);
}

void dex_ws_listen(int port) {
    signal(SIGPIPE, SIG_IGN);
    signal(SIGINT, dex_ws_shutdown_handler);
    signal(SIGTERM, dex_ws_shutdown_handler);

    int server_fd = socket(AF_INET, SOCK_STREAM, 0);
    if (server_fd < 0) { perror("ws socket"); return; }
    dex_ws_server_fd = server_fd;

    int opt = 1;
    setsockopt(server_fd, SOL_SOCKET, SO_REUSEADDR, &opt, sizeof(opt));

    struct sockaddr_in addr;
    addr.sin_family = AF_INET;
    addr.sin_addr.s_addr = INADDR_ANY;
    addr.sin_port = htons(port);

    if (bind(server_fd, (struct sockaddr*)&addr, sizeof(addr)) < 0) {
        perror("ws bind"); close(server_fd); return;
    }
    if (listen(server_fd, 128) < 0) {
        perror("ws listen"); close(server_fd); return;
    }

    printf("Dex WebSocket server listening on port %d\n", port);
    fflush(stdout);

    while (1) {
        struct sockaddr_in client_addr;
        socklen_t client_len = sizeof(client_addr);
        int client_fd = accept(server_fd, (struct sockaddr*)&client_addr, &client_len);
        if (client_fd < 0) continue;

        int* fd_ptr = (int*)malloc(sizeof(int));
        *fd_ptr = client_fd;
        pthread_t tid;
        pthread_create(&tid, NULL, dex_ws_worker, fd_ptr);
        pthread_detach(tid);
    }
}

// --- WebSocket client ---

Dex_Conn dex_ws_connect(const char* url) {
    Dex_Conn conn = {-1, 0};

    // Parse ws://host:port/path
    const char* p = url;
    if (strncmp(p, "ws://", 5) == 0) p += 5;

    char host[256] = {0};
    int port = 80;
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

    // Generate a random key (16 bytes base64-encoded)
    unsigned char raw_key[16];
    for (int i = 0; i < 16; i++) raw_key[i] = (unsigned char)(rand() & 0xFF);
    char ws_key[32];
    dex_base64_encode(raw_key, 16, ws_key);

    // Send upgrade request
    char request[1024];
    int reqlen = snprintf(request, sizeof(request),
        "GET %s HTTP/1.1\r\n"
        "Host: %s:%d\r\n"
        "Upgrade: websocket\r\n"
        "Connection: Upgrade\r\n"
        "Sec-WebSocket-Key: %s\r\n"
        "Sec-WebSocket-Version: 13\r\n"
        "\r\n", path, host, port, ws_key);
    write(fd, request, reqlen);

    // Read response
    char resp[4096];
    int rn = (int)read(fd, resp, sizeof(resp) - 1);
    if (rn <= 0 || !strstr(resp, "101")) {
        close(fd);
        return conn;
    }

    conn.fd = fd;
    conn.isServer = 0;
    return conn;
}

void dex_ws_send(Dex_Conn conn, const char* msg) {
    if (conn.fd < 0) return;
    dex_ws_frame_send(conn.fd, msg, strlen(msg), conn.isServer);
}

DexString* dex_ws_receive(Dex_Conn conn) {
    if (conn.fd < 0) return dex_string_from_lit("");
    char buf[65536];
    int opcode = 0;
    int n = dex_ws_frame_recv(conn.fd, buf, sizeof(buf), &opcode);
    if (n < 0) return dex_string_from_lit("");
    return dex_string_from_cstr(buf);
}

void dex_ws_close(Dex_Conn conn) {
    if (conn.fd < 0) return;
    // Send close frame
    unsigned char close_frame[2] = {0x88, 0x00};
    write(conn.fd, close_frame, 2);
    shutdown(conn.fd, SHUT_RDWR);
    close(conn.fd);
}
