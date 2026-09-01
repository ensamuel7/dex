// DexLang WebSocket Handshake
//
// The RFC 6455 opening handshake: the accept-key derivation both sides use, and
// the subprotocol negotiation and response validation built on it. Kept apart
// from the server so it can be compiled and tested on its own — nothing here
// touches sockets, threads or the Dex object runtime.

#ifndef DEX_WS_HANDSHAKE_H
#define DEX_WS_HANDSHAKE_H

#include <stdio.h>
#include <string.h>
#include <stddef.h>
#include <stdint.h>
#include <stdlib.h>

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
//
// Lives in base64.h, which ws.go prepends to this unit — the smtp module needs
// the same encoder and neither should carry its own. Compiled on its own (the
// handshake test builds this header with -I cruntime) the guard is still unset,
// and the #include below picks it up instead.

#ifndef DEX_BASE64_H
#include "base64.h"
#endif

// --- WebSocket protocol ---

static const char* DEX_WS_MAGIC = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11";

/* RFC 6455 forbids naming a subprotocol the client did not offer — a browser
 * fails the connection when we do. Returns 1 only when `proto` appears in the
 * request's comma-separated offer list. A client offering none (as many OCPP
 * chargers do) returns 0, and the header is then omitted rather than refused:
 * the connection's protocol is whichever listener's port it arrived on.
 * Written without strcasestr/strncasecmp, which need _GNU_SOURCE on glibc. */
static int dex_ws_ci_eq(const char* a, const char* b, size_t n) {
    for (size_t i = 0; i < n; i++) {
        unsigned char ca = (unsigned char)a[i], cb = (unsigned char)b[i];
        if (ca >= 'A' && ca <= 'Z') ca = (unsigned char)(ca - 'A' + 'a');
        if (cb >= 'A' && cb <= 'Z') cb = (unsigned char)(cb - 'A' + 'a');
        if (ca != cb) return 0;
    }
    return 1;
}

/* On a match, copies the client's own spelling of the token into `out`: a
 * browser compares the echoed value against its offer list case-sensitively,
 * so answering "ocpp1.6" to a charger that asked for "OCPP1.6" is refused. */
static int dex_ws_client_offers(const char* request, const char* proto,
                                char* out, size_t out_size) {
    if (!proto || !*proto) return 0;

    static const char NAME[] = "sec-websocket-protocol:";
    const size_t namelen = sizeof(NAME) - 1;
    const char* hdr = NULL;
    for (const char* line = request; line && *line; ) {
        if (dex_ws_ci_eq(line, NAME, namelen)) { hdr = line + namelen; break; }
        const char* nl = strstr(line, "\r\n");
        if (!nl) break;
        line = nl + 2;
        if (line[0] == '\r' && line[1] == '\n') break;   /* end of headers */
    }
    if (!hdr) return 0;

    const char* end = strstr(hdr, "\r\n");
    if (!end) return 0;

    size_t plen = strlen(proto);
    while (hdr < end) {
        while (hdr < end && (*hdr == ' ' || *hdr == '\t' || *hdr == ',')) hdr++;
        const char* tok = hdr;
        while (hdr < end && *hdr != ',') hdr++;
        const char* stop = hdr;
        while (stop > tok && (stop[-1] == ' ' || stop[-1] == '\t')) stop--;
        if ((size_t)(stop - tok) == plen && dex_ws_ci_eq(tok, proto, plen)) {
            if (out && out_size) {
                size_t n = plen < out_size - 1 ? plen : out_size - 1;
                memcpy(out, tok, n);
                out[n] = '\0';
            }
            return 1;
        }
    }
    return 0;
}

static void dex_ws_accept_key(const char* client_key, char* out_accept, size_t out_size) {
    char combined[256];
    snprintf(combined, sizeof(combined), "%s%s", client_key, DEX_WS_MAGIC);
    unsigned char digest[20];
    dex_sha1((unsigned char*)combined, strlen(combined), digest);
    dex_base64_encode(digest, 20, out_accept);
    (void)out_size;
}

/* Verifies a server handshake: a 101 status and a Sec-WebSocket-Accept that is
 * exactly base64(sha1(our key + GUID)). RFC 6455 requires the client to fail
 * the connection otherwise. */
static int dex_ws_response_valid(const char* response, const char* sent_key) {
    if (strncmp(response, "HTTP/1.1 101", 12) != 0 &&
        strncmp(response, "HTTP/1.0 101", 12) != 0) return 0;

    static const char NAME[] = "sec-websocket-accept:";
    const size_t namelen = sizeof(NAME) - 1;
    const char* hdr = NULL;
    for (const char* line = response; line && *line; ) {
        if (dex_ws_ci_eq(line, NAME, namelen)) { hdr = line + namelen; break; }
        const char* nl = strstr(line, "\r\n");
        if (!nl) break;
        line = nl + 2;
        if (line[0] == '\r' && line[1] == '\n') break;
    }
    if (!hdr) return 0;
    while (*hdr == ' ' || *hdr == '\t') hdr++;
    const char* end = strstr(hdr, "\r\n");
    if (!end) return 0;
    while (end > hdr && (end[-1] == ' ' || end[-1] == '\t')) end--;

    char expected[64];
    dex_ws_accept_key(sent_key, expected, sizeof(expected));
    size_t elen = strlen(expected);
    return (size_t)(end - hdr) == elen && memcmp(hdr, expected, elen) == 0;
}

#endif /* DEX_WS_HANDSHAKE_H */
