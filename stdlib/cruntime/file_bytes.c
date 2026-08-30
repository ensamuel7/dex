
// === Binary-safe file I/O ===
//
// The functions above take const char* and call strlen, which is fine for text
// and silently truncates a JPEG at its first NUL. These take and return
// DexString*, which is length-prefixed, so the byte count survives the round
// trip. See the RawString note on stdlib.FuncDef.

#include <sys/stat.h>
#include <errno.h>

DexString* dex_file_read_bytes(const char* path) {
    FILE* f = fopen(path, "rb");
    if (!f) return dex_string_new("", 0);
    fseek(f, 0, SEEK_END);
    long len = ftell(f);
    fseek(f, 0, SEEK_SET);
    if (len < 0) { fclose(f); return dex_string_new("", 0); }

    DexString* out = (DexString*)dex_obj_alloc(sizeof(DexString) + (size_t)len + 1,
                                               dex_string_destroy);
    size_t got = fread(out->data, 1, (size_t)len, f);
    fclose(f);
    out->len = got;
    out->data[got] = '\0';
    return out;
}

_Bool dex_file_write_bytes(const char* path, DexString* content) {
    FILE* f = fopen(path, "wb");
    if (!f) return 0;
    size_t len = content ? content->len : 0;
    size_t written = len ? fwrite(content->data, 1, len, f) : 0;
    fclose(f);
    return written == len;
}

_Bool dex_file_append_bytes(const char* path, DexString* content) {
    FILE* f = fopen(path, "ab");
    if (!f) return 0;
    size_t len = content ? content->len : 0;
    size_t written = len ? fwrite(content->data, 1, len, f) : 0;
    fclose(f);
    return written == len;
}

/* -1 when the file cannot be stat'ed, which a caller can tell from an empty
   file at 0. */
int dex_file_size(const char* path) {
    struct stat st;
    if (stat(path, &st) != 0) return -1;
    return (int)st.st_size;
}

/* mkdir -p. Upload paths are nested by date or by owner, and the directory is
   almost never there the first time. */
_Bool dex_file_mkdirp(const char* path) {
    if (!path || !*path) return 0;
    size_t n = strlen(path);
    char* buf = (char*)malloc(n + 1);
    if (!buf) return 0;
    memcpy(buf, path, n + 1);

    for (size_t i = 1; i < n; i++) {
        if (buf[i] == '/') {
            buf[i] = '\0';
            if (mkdir(buf, 0755) != 0 && errno != EEXIST) { free(buf); return 0; }
            buf[i] = '/';
        }
    }
    _Bool ok = (mkdir(buf, 0755) == 0 || errno == EEXIST);
    free(buf);
    return ok;
}

#ifdef DEX_HAS_SSL
#include <openssl/sha.h>
#include <openssl/evp.h>

/* A local hex helper rather than the crypto module's. Module runtimes are
   emitted in the order the program imports them, so a program that imports
   `file` without `crypto` would otherwise not link. */
static DexString* dex_file_hex_string(const unsigned char* raw, size_t n) {
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

/* SHA-256 of a file's contents, streamed rather than loaded. S3's SigV4 wants
   this for x-amz-content-sha256, and the file may be larger than is sensible to
   hold in memory. */
DexString* dex_file_sha256_hex(const char* path) {
    FILE* f = fopen(path, "rb");
    if (!f) return dex_string_new("", 0);

    EVP_MD_CTX* ctx = EVP_MD_CTX_new();
    EVP_DigestInit_ex(ctx, EVP_sha256(), NULL);
    unsigned char chunk[65536];
    size_t got;
    while ((got = fread(chunk, 1, sizeof(chunk), f)) > 0) {
        EVP_DigestUpdate(ctx, chunk, got);
    }
    fclose(f);

    unsigned char digest[EVP_MAX_MD_SIZE];
    unsigned int dlen = 0;
    EVP_DigestFinal_ex(ctx, digest, &dlen);
    EVP_MD_CTX_free(ctx);
    return dex_file_hex_string(digest, dlen);
}
#else
DexString* dex_file_sha256_hex(const char* path) {
    (void)path;
    fprintf(stderr, "[file] sha256Hex needs OpenSSL; this build has none\n");
    return dex_string_new("", 0);
}
#endif

/* PUT a file's bytes to a URL, with newline-separated `Key: Value` headers —
   the same header-block format the http client functions take. Returns the HTTP
   status, or 0 if the request could not be made.

   The bytes go from disk to the socket without becoming a DexString, which is
   what makes an S3 upload of an arbitrary binary possible at all. */
static size_t dex_put_discard(void* p, size_t size, size_t nmemb, void* u) {
    (void)p; (void)u;
    return size * nmemb;
}

int dex_file_put_url(const char* path, const char* url, const char* headers) {
    FILE* f = fopen(path, "rb");
    if (!f) return 0;
    struct stat st;
    if (stat(path, &st) != 0) { fclose(f); return 0; }

    CURL* curl = curl_easy_init();
    if (!curl) { fclose(f); return 0; }

    struct curl_slist* list = NULL;
    if (headers && *headers) {
        char* copy = strdup(headers);
        char* save = NULL;
        for (char* line = strtok_r(copy, "\n", &save); line; line = strtok_r(NULL, "\n", &save)) {
            if (*line) list = curl_slist_append(list, line);
        }
        free(copy);
    }

    curl_easy_setopt(curl, CURLOPT_URL, url);
    curl_easy_setopt(curl, CURLOPT_UPLOAD, 1L);
    curl_easy_setopt(curl, CURLOPT_READDATA, f);
    curl_easy_setopt(curl, CURLOPT_INFILESIZE_LARGE, (curl_off_t)st.st_size);
    curl_easy_setopt(curl, CURLOPT_WRITEFUNCTION, dex_put_discard);
    if (list) curl_easy_setopt(curl, CURLOPT_HTTPHEADER, list);

    CURLcode res = curl_easy_perform(curl);
    long status = 0;
    if (res == CURLE_OK) curl_easy_getinfo(curl, CURLINFO_RESPONSE_CODE, &status);

    if (list) curl_slist_free_all(list);
    curl_easy_cleanup(curl);
    fclose(f);
    return (int)status;
}
