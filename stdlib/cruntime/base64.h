// Base64 encoding, shared by the module runtimes that need it.
//
// The WebSocket handshake needs it for Sec-WebSocket-Key; SMTP needs it for
// AUTH and for non-ASCII subject lines. Rather than each carrying its own copy
// — two encoders drifting apart is how one of them ends up wrong — they prepend
// this file.
//
// It is guarded rather than #included because a module runtime is pasted into
// the generated C, which is compiled with no include path back to here. The
// guard is what makes the paste idempotent: a program importing both `ws` and
// `smtp` gets this text twice in one translation unit, and the second copy
// compiles to nothing.
//
// (The crypto module has its own encoder in crypto_digest.c. That one is a
// different shape on purpose — it takes and returns DexString*, so it survives
// NUL bytes and hands the result to Dex. This one writes into a caller's
// buffer and never touches the object runtime.)
#ifndef DEX_BASE64_H
#define DEX_BASE64_H

#include <stddef.h>
#include <stdint.h>

static const char dex_b64_table[] =
    "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";

/* Writes 4 * ceil(len/3) characters plus a terminating NUL, so `out` needs
 * 4 * ((len + 2) / 3) + 1 bytes. Binary-safe: `len` is honoured, not strlen. */
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

#endif /* DEX_BASE64_H */
