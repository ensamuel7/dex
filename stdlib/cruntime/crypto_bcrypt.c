
#ifdef DEX_HAS_XCRYPT
#include <crypt.h>

static int dex_bcrypt_password(DexString* password, const char** out) {
    const char* p = password ? password->data : "";
    size_t plen = password ? password->len : 0;
    if (memchr(p, '\0', plen) != NULL) return 0;
    *out = p;
    return 1;
}

DexString* dex_crypto_bcrypt_hash(DexString* password, int cost) {
    const char* p;
    if (!dex_bcrypt_password(password, &p)) return dex_string_new("", 0);
    if (cost < 4 || cost > 31) return dex_string_new("", 0);
    char rbytes[16];
    if (!dex_random_bytes((unsigned char*)rbytes, sizeof(rbytes))) return dex_string_new("", 0);
    char setting[CRYPT_GENSALT_OUTPUT_SIZE];
    if (!crypt_gensalt_rn("$2b$", (unsigned long)cost, rbytes, (int)sizeof(rbytes), setting, (int)sizeof(setting))) {
        return dex_string_new("", 0);
    }
    struct crypt_data* data = (struct crypt_data*)dex_xcalloc(1, sizeof(struct crypt_data));
    const char* h = crypt_rn(p, setting, data, (int)sizeof(struct crypt_data));
    DexString* s = (h && h[0] == '$') ? dex_string_new(h, strlen(h)) : dex_string_new("", 0);
    free(data);
    return s;
}

_Bool dex_crypto_bcrypt_verify(DexString* password, DexString* stored) {
    if (!stored || stored->len < 7 || stored->len >= CRYPT_OUTPUT_SIZE) return 0;
    if (memchr(stored->data, '\0', stored->len) != NULL) return 0;
    if (stored->data[0] != '$' || stored->data[1] != '2') return 0;
    const char* p;
    if (!dex_bcrypt_password(password, &p)) return 0;
    struct crypt_data* data = (struct crypt_data*)dex_xcalloc(1, sizeof(struct crypt_data));
    const char* h = crypt_rn(p, stored->data, data, (int)sizeof(struct crypt_data));
    _Bool same = 0;
    if (h && h[0] == '$' && strlen(h) == stored->len) {
        unsigned char diff = 0;
        for (size_t i = 0; i < stored->len; i++) diff |= (unsigned char)(h[i] ^ stored->data[i]);
        same = diff == 0;
    }
    free(data);
    return same;
}
#else
DexString* dex_crypto_bcrypt_hash(DexString* password, int cost) {
    (void)password; (void)cost;
    return dex_string_new("", 0);
}

_Bool dex_crypto_bcrypt_verify(DexString* password, DexString* stored) {
    (void)password; (void)stored;
    return 0;
}
#endif
