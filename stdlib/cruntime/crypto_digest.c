
// === Base64 and digests ===
//
// Every function here is RawString: it takes and returns DexString*, so it
// reads ->len rather than calling strlen. That is the whole point — a JPEG,
// a signature, or any decoded base64 will contain NUL bytes, and the ordinary
// const char* boundary would truncate at the first one.

static const char DEX_B64[] =
    "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";

DexString* dex_crypto_base64_encode(DexString* in) {
    if (!in) return dex_string_new("", 0);
    size_t n = in->len;
    const unsigned char* src = (const unsigned char*)in->data;
    size_t outlen = 4 * ((n + 2) / 3);

    DexString* out = (DexString*)dex_obj_alloc(sizeof(DexString) + outlen + 1, dex_string_destroy);
    out->len = outlen;
    char* d = out->data;

    size_t i = 0;
    while (i + 2 < n) {
        unsigned v = (src[i] << 16) | (src[i + 1] << 8) | src[i + 2];
        *d++ = DEX_B64[(v >> 18) & 63];
        *d++ = DEX_B64[(v >> 12) & 63];
        *d++ = DEX_B64[(v >> 6) & 63];
        *d++ = DEX_B64[v & 63];
        i += 3;
    }
    if (i < n) {
        unsigned v = src[i] << 16;
        int rem = (int)(n - i);
        if (rem == 2) v |= src[i + 1] << 8;
        *d++ = DEX_B64[(v >> 18) & 63];
        *d++ = DEX_B64[(v >> 12) & 63];
        *d++ = (rem == 2) ? DEX_B64[(v >> 6) & 63] : '=';
        *d++ = '=';
    }
    *d = '\0';
    return out;
}

static int dex_b64_value(int c) {
    if (c >= 'A' && c <= 'Z') return c - 'A';
    if (c >= 'a' && c <= 'z') return c - 'a' + 26;
    if (c >= '0' && c <= '9') return c - '0' + 52;
    if (c == '+') return 62;
    if (c == '/') return 63;
    return -1; /* '=' and anything else, including whitespace */
}

DexString* dex_crypto_base64_decode(DexString* in) {
    if (!in) return dex_string_new("", 0);
    size_t n = in->len;
    /* Upper bound: every 4 input chars yield at most 3 bytes. */
    size_t cap = (n / 4 + 1) * 3 + 3;
    DexString* out = (DexString*)dex_obj_alloc(sizeof(DexString) + cap + 1, dex_string_destroy);
    unsigned char* d = (unsigned char*)out->data;

    unsigned acc = 0;
    int bits = 0;
    size_t produced = 0;
    for (size_t i = 0; i < n; i++) {
        int v = dex_b64_value((unsigned char)in->data[i]);
        if (v < 0) continue; /* skip padding and newlines */
        acc = (acc << 6) | (unsigned)v;
        bits += 6;
        if (bits >= 8) {
            bits -= 8;
            d[produced++] = (unsigned char)((acc >> bits) & 0xFF);
        }
    }
    out->len = produced;
    out->data[produced] = '\0';
    return out;
}

#ifdef DEX_HAS_SSL
#include <string.h>
#include <openssl/sha.h>
#include <openssl/hmac.h>
#include <openssl/evp.h>

static DexString* dex_hex_string(const unsigned char* raw, size_t n) {
    static const char* hex = "0123456789abcdef";
    DexString* out = (DexString*)dex_obj_alloc(sizeof(DexString) + n * 2 + 1, dex_string_destroy);
    out->len = n * 2;
    for (size_t i = 0; i < n; i++) {
        out->data[i * 2]     = hex[(raw[i] >> 4) & 0xF];
        out->data[i * 2 + 1] = hex[raw[i] & 0xF];
    }
    out->data[n * 2] = '\0';
    return out;
}

/* base64url with no padding, which is the only spelling a JWS uses. A token's
   signature segment is compared against the recomputed MAC directly, so the
   encoding has to come out byte for byte the same as the token's rather than be
   converted on the way — which is why this is not base64Encode plus a
   character swap and a strip. */
static DexString* dex_b64url_string(const unsigned char* raw, size_t n) {
    static const char* b64url =
        "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_";
    size_t out_len = (n * 8 + 5) / 6;   /* ceil(n*8/6): the padding is dropped */
    DexString* out = (DexString*)dex_obj_alloc(sizeof(DexString) + out_len + 1, dex_string_destroy);
    out->len = out_len;
    size_t j = 0;
    for (size_t i = 0; i < n; i += 3) {
        unsigned v = (unsigned)raw[i] << 16;
        if (i + 1 < n) v |= (unsigned)raw[i + 1] << 8;
        if (i + 2 < n) v |= (unsigned)raw[i + 2];
        out->data[j++] = b64url[(v >> 18) & 0x3F];
        out->data[j++] = b64url[(v >> 12) & 0x3F];
        if (i + 1 < n) out->data[j++] = b64url[(v >> 6) & 0x3F];
        if (i + 2 < n) out->data[j++] = b64url[v & 0x3F];
    }
    out->data[out_len] = '\0';
    return out;
}

DexString* dex_crypto_sha256_hex(DexString* in) {
    unsigned char digest[SHA256_DIGEST_LENGTH];
    const void* data = in ? (const void*)in->data : (const void*)"";
    size_t len = in ? in->len : 0;
    SHA256((const unsigned char*)data, len, digest);
    return dex_hex_string(digest, SHA256_DIGEST_LENGTH);
}

DexString* dex_crypto_hmac_sha256_hex(DexString* key, DexString* msg) {
    unsigned char digest[EVP_MAX_MD_SIZE];
    unsigned int dlen = 0;
    const void* kdata = key ? (const void*)key->data : (const void*)"";
    int klen = key ? (int)key->len : 0;
    const unsigned char* mdata = msg ? (const unsigned char*)msg->data : (const unsigned char*)"";
    size_t mlen = msg ? msg->len : 0;
    HMAC(EVP_sha256(), kdata, klen, mdata, mlen, digest, &dlen);
    return dex_hex_string(digest, dlen);
}

/* The same MAC as hmacSha256Hex, in the encoding a JWT carries it in. Verifying
   HS256 with the hex one means decoding the token's signature from base64url to
   compare — base64url decoding in the hot path of every authenticated request,
   to reach a value this produces directly. */
DexString* dex_crypto_hmac_sha256_b64url(DexString* key, DexString* msg) {
    unsigned char digest[EVP_MAX_MD_SIZE];
    unsigned int dlen = 0;
    const void* kdata = key ? (const void*)key->data : (const void*)"";
    int klen = key ? (int)key->len : 0;
    const unsigned char* mdata = msg ? (const unsigned char*)msg->data : (const unsigned char*)"";
    size_t mlen = msg ? msg->len : 0;
    HMAC(EVP_sha256(), kdata, klen, mdata, mlen, digest, &dlen);
    return dex_b64url_string(digest, dlen);
}

/* SHA-512 as well as SHA-256, because payment providers do not agree on which
   they sign with: Paystack uses SHA-512, Stripe SHA-256. */
DexString* dex_crypto_hmac_sha512_hex(DexString* key, DexString* msg) {
    unsigned char digest[EVP_MAX_MD_SIZE];
    unsigned int dlen = 0;
    const void* kdata = key ? (const void*)key->data : (const void*)"";
    int klen = key ? (int)key->len : 0;
    const unsigned char* mdata = msg ? (const unsigned char*)msg->data : (const unsigned char*)"";
    size_t mlen = msg ? msg->len : 0;
    HMAC(EVP_sha512(), kdata, klen, mdata, mlen, digest, &dlen);
    return dex_hex_string(digest, dlen);
}

/* Compares two hex digests without leaking, in timing, how much of a prefix
   matched. A signature check that returns early is a signature check an
   attacker can walk one byte at a time. */
_Bool dex_crypto_secure_equals(DexString* a, DexString* b) {
    size_t alen = a ? a->len : 0;
    size_t blen = b ? b->len : 0;
    if (alen != blen || alen == 0) {
        return 0;
    }
    const unsigned char* x = (const unsigned char*)a->data;
    const unsigned char* y = (const unsigned char*)b->data;
    unsigned char diff = 0;
    for (size_t i = 0; i < alen; i++) {
        diff |= (unsigned char)(x[i] ^ y[i]);
    }
    return diff == 0;
}

#else

/* Built without OpenSSL. The module still compiles so a program that only
   mentions a digest on a path it never takes still builds and runs — the same
   contract the Kafka module keeps. */
DexString* dex_crypto_sha256_hex(DexString* in) {
    (void)in;
    fprintf(stderr, "[crypto] sha256Hex needs OpenSSL; this build has none\n");
    return dex_string_new("", 0);
}

DexString* dex_crypto_hmac_sha256_hex(DexString* key, DexString* msg) {
    (void)key; (void)msg;
    fprintf(stderr, "[crypto] hmacSha256Hex needs OpenSSL; this build has none\n");
    return dex_string_new("", 0);
}

DexString* dex_crypto_hmac_sha256_b64url(DexString* key, DexString* msg) {
    (void)key; (void)msg;
    fprintf(stderr, "[crypto] hmacSha256B64Url needs OpenSSL; this build has none\n");
    return dex_string_new("", 0);
}

DexString* dex_crypto_hmac_sha512_hex(DexString* key, DexString* msg) {
    (void)key; (void)msg;
    fprintf(stderr, "[crypto] hmacSha512Hex needs OpenSSL; this build has none\n");
    return dex_string_new("", 0);
}

/* No OpenSSL needed for this one, so it behaves the same in either build. */
_Bool dex_crypto_secure_equals(DexString* a, DexString* b) {
    size_t alen = a ? a->len : 0;
    size_t blen = b ? b->len : 0;
    if (alen != blen || alen == 0) {
        return 0;
    }
    const unsigned char* x = (const unsigned char*)a->data;
    const unsigned char* y = (const unsigned char*)b->data;
    unsigned char diff = 0;
    for (size_t i = 0; i < alen; i++) {
        diff |= (unsigned char)(x[i] ^ y[i]);
    }
    return diff == 0;
}

#endif
