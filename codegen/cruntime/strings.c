// DexLang String Runtime
// DexString is a refcounted, length-prefixed string.

typedef struct {
    DexObjHeader hdr;
    size_t len;
    char data[];
} DexString;

static void dex_string_destroy(void* ptr) {
    (void)ptr; // data[] is part of the struct, freed with it
}

static DexString* dex_string_new(const char* s, size_t len) {
    DexString* ds = (DexString*)dex_obj_alloc(sizeof(DexString) + len + 1, dex_string_destroy);
    ds->len = len;
    memcpy(ds->data, s, len);
    ds->data[len] = '\0';
    return ds;
}

static DexString* dex_string_from_lit(const char* s) {
    return dex_string_new(s, strlen(s));
}

static DexString* dex_string_from_cstr(const char* s) {
    if (!s) return dex_string_new("", 0);
    DexString* ds = dex_string_new(s, strlen(s));
    free((void*)s);
    return ds;
}

static DexString* dex_string_empty(void) {
    return dex_string_new("", 0);
}

static DexString* dex_str_concat(DexString* a, DexString* b) {
    size_t newlen = a->len + b->len;
    DexString* ds = (DexString*)dex_obj_alloc(sizeof(DexString) + newlen + 1, dex_string_destroy);
    ds->len = newlen;
    memcpy(ds->data, a->data, a->len);
    memcpy(ds->data + a->len, b->data, b->len);
    ds->data[newlen] = '\0';
    return ds;
}

// === JSON string escaping ===
//
// The single implementation. Two separate encoders need it — dex_arr_encode_struct
// in arrays.c and dex_json_encode_struct in the json module — and they are emitted
// into different translation units, so this lives here, in core runtime that is
// always present and always emitted first. Both call these; neither reimplements
// them, because two escapers that can drift apart is how invalid JSON comes back.
//
// Escapes per RFC 8259: the quote, the backslash, the five shorthand controls,
// and everything else below 0x20 as \u00XX. Bytes >= 0x20 pass through, so valid
// UTF-8 stays valid UTF-8.

// Length of the escaped form of str, excluding the surrounding quotes.
static size_t dex_json_escape_len(const char* str) {
    if (!str) return 0;
    size_t len = 0;
    for (const char* p = str; *p; p++) {
        switch (*p) {
        case '"': case '\\':
        case '\b': case '\f': case '\n': case '\r': case '\t':
            len += 2; break;
        default:
            len += ((unsigned char)*p < 0x20) ? 6 : 1;
        }
    }
    return len;
}

// Writes the escaped form of str into dst, excluding the surrounding quotes.
// dst must have room for dex_json_escape_len(str) bytes. Returns bytes written.
static size_t dex_json_escape_write(char* dst, const char* str) {
    if (!str) return 0;
    size_t pos = 0;
    for (const char* p = str; *p; p++) {
        switch (*p) {
        case '"':  dst[pos++] = '\\'; dst[pos++] = '"';  break;
        case '\\': dst[pos++] = '\\'; dst[pos++] = '\\'; break;
        case '\b': dst[pos++] = '\\'; dst[pos++] = 'b';  break;
        case '\f': dst[pos++] = '\\'; dst[pos++] = 'f';  break;
        case '\n': dst[pos++] = '\\'; dst[pos++] = 'n';  break;
        case '\r': dst[pos++] = '\\'; dst[pos++] = 'r';  break;
        case '\t': dst[pos++] = '\\'; dst[pos++] = 't';  break;
        default:
            if ((unsigned char)*p < 0x20) {
                static const char* hex = "0123456789abcdef";
                dst[pos++] = '\\'; dst[pos++] = 'u'; dst[pos++] = '0'; dst[pos++] = '0';
                dst[pos++] = hex[((unsigned char)*p >> 4) & 0xF];
                dst[pos++] = hex[(unsigned char)*p & 0xF];
            } else {
                dst[pos++] = *p;
            }
        }
    }
    return pos;
}

// Decodes one JSON string into a freshly malloc'd buffer.
//
// *pp must point at the opening quote. On success *pp is advanced past the
// closing quote, *out receives the buffer (NUL-terminated) and *outlen its
// length, and 1 is returned. On malformed input 0 is returned and nothing is
// allocated.
//
// Like the escaper above, this lives in core runtime because two callers in two
// translation units need it: dex_json_get in the json module, and the json.Value
// parser. Scanning for the closing quote has to respect backslash escapes —
// strchr does not, which is how a field containing \" used to be cut in half.
static int dex_json_unescape_string(const char** pp, char** out, size_t* outlen) {
    const char* p = *pp;
    if (*p != '"') return 0;
    p++;

    size_t cap = 32, n = 0;
    char* buf = (char*)malloc(cap);
    if (!buf) return 0;

    while (*p && *p != '"') {
        // One escape can expand to four UTF-8 bytes.
        if (n + 5 > cap) {
            cap *= 2;
            char* nb = (char*)realloc(buf, cap);
            if (!nb) { free(buf); return 0; }
            buf = nb;
        }
        if (*p != '\\') { buf[n++] = *p++; continue; }

        p++;
        switch (*p) {
        case 'n':  buf[n++] = '\n'; p++; break;
        case 't':  buf[n++] = '\t'; p++; break;
        case 'r':  buf[n++] = '\r'; p++; break;
        case 'b':  buf[n++] = '\b'; p++; break;
        case 'f':  buf[n++] = '\f'; p++; break;
        case '/':  buf[n++] = '/';  p++; break;
        case '"':  buf[n++] = '"';  p++; break;
        case '\\': buf[n++] = '\\'; p++; break;
        case 'u': {
            unsigned int cp = 0;
            int d = 0;
            p++;
            for (; d < 4 && p[d]; d++) {
                char c = p[d];
                int hv;
                if (c >= '0' && c <= '9') hv = c - '0';
                else if (c >= 'a' && c <= 'f') hv = c - 'a' + 10;
                else if (c >= 'A' && c <= 'F') hv = c - 'A' + 10;
                else break;
                cp = (cp << 4) | (unsigned)hv;
            }
            if (d < 4) { free(buf); return 0; }
            p += 4;
            // A high surrogate only means anything paired with its low half;
            // combine them so astral characters survive the round trip.
            if (cp >= 0xD800 && cp <= 0xDBFF && p[0] == '\\' && p[1] == 'u') {
                unsigned int lo = 0;
                int ld = 0;
                for (; ld < 4 && p[2 + ld]; ld++) {
                    char c = p[2 + ld];
                    int hv;
                    if (c >= '0' && c <= '9') hv = c - '0';
                    else if (c >= 'a' && c <= 'f') hv = c - 'a' + 10;
                    else if (c >= 'A' && c <= 'F') hv = c - 'A' + 10;
                    else break;
                    lo = (lo << 4) | (unsigned)hv;
                }
                if (ld == 4 && lo >= 0xDC00 && lo <= 0xDFFF) {
                    cp = 0x10000 + ((cp - 0xD800) << 10) + (lo - 0xDC00);
                    p += 6;
                }
            }
            if (cp < 0x80) {
                buf[n++] = (char)cp;
            } else if (cp < 0x800) {
                buf[n++] = (char)(0xC0 | (cp >> 6));
                buf[n++] = (char)(0x80 | (cp & 0x3F));
            } else if (cp < 0x10000) {
                buf[n++] = (char)(0xE0 | (cp >> 12));
                buf[n++] = (char)(0x80 | ((cp >> 6) & 0x3F));
                buf[n++] = (char)(0x80 | (cp & 0x3F));
            } else {
                buf[n++] = (char)(0xF0 | (cp >> 18));
                buf[n++] = (char)(0x80 | ((cp >> 12) & 0x3F));
                buf[n++] = (char)(0x80 | ((cp >> 6) & 0x3F));
                buf[n++] = (char)(0x80 | (cp & 0x3F));
            }
            break;
        }
        default: free(buf); return 0;
        }
    }

    if (*p != '"') { free(buf); return 0; }
    p++;

    if (n + 1 > cap) {
        char* nb = (char*)realloc(buf, n + 1);
        if (!nb) { free(buf); return 0; }
        buf = nb;
    }
    buf[n] = '\0';

    *pp = p;
    *out = buf;
    *outlen = n;
    return 1;
}
