
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

// === Base32 (RFC 4648) ===
//
// For TOTP secrets: authenticator apps take the shared key in base32, upper
// case and unpadded, which is what an otpauth:// URI carries.

static const char DEX_B32[] = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";

static DexString* dex_b32_encode_raw(const unsigned char* src, size_t n) {
    size_t outlen = (n * 8 + 4) / 5;
    DexString* out = (DexString*)dex_obj_alloc(sizeof(DexString) + outlen + 1, dex_string_destroy);
    out->len = outlen;
    char* d = out->data;
    unsigned acc = 0;
    int bits = 0;
    for (size_t i = 0; i < n; i++) {
        acc = ((acc << 8) | src[i]) & 0xFFFF;
        bits += 8;
        while (bits >= 5) {
            bits -= 5;
            *d++ = DEX_B32[(acc >> bits) & 31];
        }
    }
    if (bits > 0) *d++ = DEX_B32[(acc << (5 - bits)) & 31];
    *d = '\0';
    return out;
}

DexString* dex_crypto_base32_encode(DexString* in) {
    if (!in) return dex_string_new("", 0);
    return dex_b32_encode_raw((const unsigned char*)in->data, in->len);
}

/* Case-insensitive; spaces, hyphens and '=' are skipped, since people copy
   secrets in groups. Any other character makes the whole input invalid: "". */
static size_t dex_b32_decode_raw(const char* src, size_t n, unsigned char* d, int* ok) {
    unsigned acc = 0;
    int bits = 0;
    size_t produced = 0;
    *ok = 1;
    for (size_t i = 0; i < n; i++) {
        int c = (unsigned char)src[i];
        int v;
        if (c >= 'A' && c <= 'Z') v = c - 'A';
        else if (c >= 'a' && c <= 'z') v = c - 'a';
        else if (c >= '2' && c <= '7') v = c - '2' + 26;
        else if (c == ' ' || c == '-' || c == '=') continue;
        else { *ok = 0; return 0; }
        acc = ((acc << 5) | (unsigned)v) & 0xFFFF;
        bits += 5;
        if (bits >= 8) {
            bits -= 8;
            d[produced++] = (unsigned char)((acc >> bits) & 0xFF);
        }
    }
    return produced;
}

DexString* dex_crypto_base32_decode(DexString* in) {
    if (!in) return dex_string_new("", 0);
    size_t cap = in->len * 5 / 8 + 1;
    DexString* out = (DexString*)dex_obj_alloc(sizeof(DexString) + cap + 1, dex_string_destroy);
    int ok = 0;
    size_t produced = dex_b32_decode_raw(in->data, in->len, (unsigned char*)out->data, &ok);
    if (!ok) produced = 0;
    out->len = produced;
    out->data[produced] = '\0';
    return out;
}

/* n random bytes, base32: a TOTP secret. 20 bytes is RFC 4226's size. */
DexString* dex_crypto_random_base32(int n) {
    if (n < 1 || n > 1024) return dex_string_new("", 0);
    unsigned char bytes[1024];
    if (!dex_random_bytes(bytes, (size_t)n)) return dex_string_new("", 0);
    DexString* out = dex_b32_encode_raw(bytes, (size_t)n);
    memset(bytes, 0, (size_t)n);
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

/* HMAC-SHA-1. Weak as a hash, still sound as a MAC, and the only one most
   authenticator apps compute TOTP codes with. */
DexString* dex_crypto_hmac_sha1_hex(DexString* key, DexString* msg) {
    unsigned char digest[EVP_MAX_MD_SIZE];
    unsigned int dlen = 0;
    const void* kdata = key ? (const void*)key->data : (const void*)"";
    int klen = key ? (int)key->len : 0;
    const unsigned char* mdata = msg ? (const unsigned char*)msg->data : (const unsigned char*)"";
    size_t mlen = msg ? msg->len : 0;
    HMAC(EVP_sha1(), kdata, klen, mdata, mlen, digest, &dlen);
    return dex_hex_string(digest, dlen);
}

/* RFC 6238 with the parameters every authenticator app assumes: 30-second
   steps, HMAC-SHA-1, six digits. "" for a secret that is empty or not base32. */
DexString* dex_crypto_totp(DexString* secret, long unixSeconds) {
    if (!secret || secret->len == 0) return dex_string_new("", 0);
    unsigned char key[1024];
    if (secret->len * 5 / 8 + 1 > sizeof(key)) return dex_string_new("", 0);
    int ok = 0;
    size_t klen = dex_b32_decode_raw(secret->data, secret->len, key, &ok);
    if (!ok || klen == 0) return dex_string_new("", 0);
    unsigned long long counter = unixSeconds < 0 ? 0 : (unsigned long long)unixSeconds / 30;
    unsigned char msg[8];
    for (int i = 7; i >= 0; i--) {
        msg[i] = (unsigned char)(counter & 0xFF);
        counter >>= 8;
    }
    unsigned char digest[EVP_MAX_MD_SIZE];
    unsigned int dlen = 0;
    HMAC(EVP_sha1(), key, (int)klen, msg, sizeof(msg), digest, &dlen);
    memset(key, 0, klen);
    int offset = digest[dlen - 1] & 0x0F;
    unsigned long code = ((unsigned long)(digest[offset] & 0x7F) << 24) |
                         ((unsigned long)digest[offset + 1] << 16) |
                         ((unsigned long)digest[offset + 2] << 8) |
                         (unsigned long)digest[offset + 3];
    char text[7];
    snprintf(text, sizeof(text), "%06lu", code % 1000000UL);
    return dex_string_new(text, 6);
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


/* Passwords, in one format: pbkdf2-sha256$<iterations>$<salt hex>$<hash hex>.
   The iteration count travels with the hash, so raising it later still
   verifies every password stored before. 600000 is OWASP's figure for
   PBKDF2-HMAC-SHA256. */
#define DEX_PW_ITERATIONS 600000

static char* dex_pw_encode(const char* password, size_t plen, const unsigned char* salt, size_t slen, int iterations) {
    unsigned char key[32];
    if (PKCS5_PBKDF2_HMAC(password, (int)plen, salt, (int)slen, iterations, EVP_sha256(), (int)sizeof(key), key) != 1) {
        return NULL;
    }
    static const char hex[] = "0123456789abcdef";
    size_t cap = 32 + slen * 2 + sizeof(key) * 2;
    char* out = (char*)dex_xmalloc(cap);
    if (!out) return NULL;
    int n = snprintf(out, cap, "pbkdf2-sha256$%d$", iterations);
    for (size_t i = 0; i < slen; i++) { out[n++] = hex[salt[i] >> 4]; out[n++] = hex[salt[i] & 15]; }
    out[n++] = '$';
    for (size_t i = 0; i < sizeof(key); i++) { out[n++] = hex[key[i] >> 4]; out[n++] = hex[key[i] & 15]; }
    out[n] = '\0';
    return out;
}

DexString* dex_crypto_hash_password(DexString* password) {
    unsigned char salt[16];
    if (!dex_random_bytes(salt, sizeof(salt))) return dex_string_new("", 0);
    const char* p = password ? password->data : "";
    size_t plen = password ? password->len : 0;
    char* encoded = dex_pw_encode(p, plen, salt, sizeof(salt), DEX_PW_ITERATIONS);
    if (!encoded) return dex_string_new("", 0);
    DexString* s = dex_string_new(encoded, strlen(encoded));
    free(encoded);
    return s;
}

static int dex_hex_nibble(char c) {
    if (c >= '0' && c <= '9') return c - '0';
    if (c >= 'a' && c <= 'f') return c - 'a' + 10;
    return -1;
}

/* False for every reason: a wrong password, a malformed or empty stored hash,
   a build without OpenSSL. None of them is a reason to let someone in. */
_Bool dex_crypto_verify_password(DexString* password, DexString* stored) {
    if (!stored || stored->len == 0) return 0;
    const char* s = stored->data;
    const char* prefix = "pbkdf2-sha256$";
    if (strncmp(s, prefix, strlen(prefix)) != 0) return 0;
    const char* it = s + strlen(prefix);
    char* end = NULL;
    long iterations = strtol(it, &end, 10);
    if (!end || *end != '$' || iterations < 1000 || iterations > 10000000) return 0;
    const char* salt_hex = end + 1;
    const char* sep = strchr(salt_hex, '$');
    if (!sep) return 0;
    size_t salt_hex_len = (size_t)(sep - salt_hex);
    if (salt_hex_len == 0 || salt_hex_len % 2 != 0 || salt_hex_len > 128) return 0;
    unsigned char salt[64];
    for (size_t i = 0; i < salt_hex_len / 2; i++) {
        int hi = dex_hex_nibble(salt_hex[2 * i]), lo = dex_hex_nibble(salt_hex[2 * i + 1]);
        if (hi < 0 || lo < 0) return 0;
        salt[i] = (unsigned char)(hi * 16 + lo);
    }
    const char* p = password ? password->data : "";
    size_t plen = password ? password->len : 0;
    char* again = dex_pw_encode(p, plen, salt, salt_hex_len / 2, (int)iterations);
    if (!again) return 0;
    size_t alen = strlen(again);
    _Bool same = alen == stored->len && CRYPTO_memcmp(again, s, alen) == 0;
    free(again);
    return same;
}

/* Authenticated encryption for secrets at rest: AES-256-GCM, a fresh 12-byte
   nonce per call, written as base64(nonce || ciphertext || 16-byte tag). The
   key is 32 bytes as 64 hex digits. Anything wrong (a bad key, a short or
   altered input) gives "", because a decryption that half-succeeds is worse
   than one that plainly fails. */
static int dex_aes_key(DexString* hex, unsigned char key[32]) {
    if (!hex || hex->len != 64) return 0;
    for (int i = 0; i < 32; i++) {
        int hi = dex_hex_nibble(hex->data[2 * i]), lo = dex_hex_nibble(hex->data[2 * i + 1]);
        if (hi < 0 || lo < 0) return 0;
        key[i] = (unsigned char)(hi * 16 + lo);
    }
    return 1;
}

DexString* dex_crypto_encrypt(DexString* keyHex, DexString* plain) {
    unsigned char key[32];
    if (!dex_aes_key(keyHex, key)) return dex_string_new("", 0);
    size_t plen = plain ? plain->len : 0;
    unsigned char* buf = (unsigned char*)dex_xmalloc(12 + plen + 16);
    if (!buf) return dex_string_new("", 0);
    if (!dex_random_bytes(buf, 12)) { free(buf); return dex_string_new("", 0); }
    EVP_CIPHER_CTX* ctx = EVP_CIPHER_CTX_new();
    int ok = ctx && EVP_EncryptInit_ex(ctx, EVP_aes_256_gcm(), NULL, key, buf) == 1;
    int n = 0, fin = 0;
    if (ok && plen > 0) ok = EVP_EncryptUpdate(ctx, buf + 12, &n, (const unsigned char*)plain->data, (int)plen) == 1;
    if (ok) ok = EVP_EncryptFinal_ex(ctx, buf + 12 + n, &fin) == 1;
    if (ok) ok = EVP_CIPHER_CTX_ctrl(ctx, EVP_CTRL_GCM_GET_TAG, 16, buf + 12 + n + fin) == 1;
    if (ctx) EVP_CIPHER_CTX_free(ctx);
    if (!ok) { free(buf); return dex_string_new("", 0); }
    DexString* raw = dex_string_new((const char*)buf, (size_t)(12 + n + fin + 16));
    free(buf);
    DexString* out = dex_crypto_base64_encode(raw);
    dex_release(raw);
    return out;
}

DexString* dex_crypto_decrypt(DexString* keyHex, DexString* sealed) {
    unsigned char key[32];
    if (!dex_aes_key(keyHex, key) || !sealed) return dex_string_new("", 0);
    DexString* raw = dex_crypto_base64_decode(sealed);
    if (!raw || raw->len < 12 + 16) { if (raw) dex_release(raw); return dex_string_new("", 0); }
    const unsigned char* in = (const unsigned char*)raw->data;
    size_t clen = raw->len - 12 - 16;
    unsigned char* out = (unsigned char*)dex_xmalloc(clen + 1);
    EVP_CIPHER_CTX* ctx = EVP_CIPHER_CTX_new();
    int ok = out && ctx && EVP_DecryptInit_ex(ctx, EVP_aes_256_gcm(), NULL, key, in) == 1;
    int n = 0, fin = 0;
    if (ok && clen > 0) ok = EVP_DecryptUpdate(ctx, out, &n, in + 12, (int)clen) == 1;
    if (ok) ok = EVP_CIPHER_CTX_ctrl(ctx, EVP_CTRL_GCM_SET_TAG, 16, (void*)(in + 12 + clen)) == 1;
    if (ok) ok = EVP_DecryptFinal_ex(ctx, out + n, &fin) == 1;
    if (ctx) EVP_CIPHER_CTX_free(ctx);
    dex_release(raw);
    if (!ok) { free(out); return dex_string_new("", 0); }
    DexString* result = dex_string_new((const char*)out, (size_t)(n + fin));
    free(out);
    return result;
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

DexString* dex_crypto_hmac_sha1_hex(DexString* key, DexString* msg) {
    (void)key; (void)msg;
    fprintf(stderr, "[crypto] hmacSha1Hex needs OpenSSL; this build has none\n");
    return dex_string_new("", 0);
}

DexString* dex_crypto_totp(DexString* secret, long unixSeconds) {
    (void)secret; (void)unixSeconds;
    fprintf(stderr, "[crypto] totp needs OpenSSL; this build has none\n");
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


DexString* dex_crypto_hash_password(DexString* password) {
    (void)password;
    fprintf(stderr, "[crypto] hashPassword needs OpenSSL; this build has none\n");
    return dex_string_new("", 0);
}

_Bool dex_crypto_verify_password(DexString* password, DexString* stored) {
    (void)password; (void)stored;
    fprintf(stderr, "[crypto] verifyPassword needs OpenSSL; this build has none\n");
    return 0;
}

DexString* dex_crypto_encrypt(DexString* keyHex, DexString* plain) {
    (void)keyHex; (void)plain;
    fprintf(stderr, "[crypto] encrypt needs OpenSSL; this build has none\n");
    return dex_string_new("", 0);
}

DexString* dex_crypto_decrypt(DexString* keyHex, DexString* sealed) {
    (void)keyHex; (void)sealed;
    fprintf(stderr, "[crypto] decrypt needs OpenSSL; this build has none\n");
    return dex_string_new("", 0);
}
#endif
