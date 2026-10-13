
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

// Escaping lives in core runtime (strings.c) so this module and the struct-array
// encoder in arrays.c share one implementation. These two remain as the names the
// rest of this file already uses.
static size_t dex_json_escaped_len(const char* str) {
    return dex_json_escape_len(str);
}

static size_t dex_json_escape(char* dst, const char* str) {
    return dex_json_escape_write(dst, str);
}

const char* dex_json_new(void) {
    char* s = (char*)malloc(3);
    if (!s) return strdup("");
    s[0] = '{'; s[1] = '}'; s[2] = '\0';
    return s;
}

const char* dex_json_set(const char* obj, const char* key, const char* val) {
    size_t olen = strlen(obj);
    size_t eklen = dex_json_escaped_len(key);
    size_t evlen = dex_json_escaped_len(val);
    // worst case: existing + `, "escaped_key": "escaped_val"}`
    size_t need = olen + eklen + evlen + 10;
    char* result = (char*)malloc(need);
    if (!result) return obj;
    size_t pos = 0;
    if (olen == 2) {
        result[pos++] = '{';
    } else {
        memcpy(result, obj, olen - 1);
        pos = olen - 1;
        result[pos++] = ',';
        result[pos++] = ' ';
    }
    result[pos++] = '"';
    pos += dex_json_escape(result + pos, key);
    result[pos++] = '"';
    result[pos++] = ':';
    result[pos++] = ' ';
    result[pos++] = '"';
    pos += dex_json_escape(result + pos, val);
    result[pos++] = '"';
    result[pos++] = '}';
    result[pos] = '\0';
    return result;
}

const char* dex_json_set_int(const char* obj, const char* key, int val) {
    size_t olen = strlen(obj);
    size_t klen = strlen(key);
    size_t need = olen + klen + 32;
    char* result = (char*)malloc(need);
    if (!result) return obj;
    if (olen == 2) {
        snprintf(result, need, "{\"%s\": %d}", key, val);
    } else {
        memcpy(result, obj, olen - 1);
        snprintf(result + olen - 1, need - (olen - 1), ", \"%s\": %d}", key, val);
    }
    return result;
}

const char* dex_json_set_bool(const char* obj, const char* key, _Bool val) {
    size_t olen = strlen(obj);
    size_t klen = strlen(key);
    size_t need = olen + klen + 16;
    char* result = (char*)malloc(need);
    if (!result) return obj;
    if (olen == 2) {
        snprintf(result, need, "{\"%s\": %s}", key, val ? "true" : "false");
    } else {
        memcpy(result, obj, olen - 1);
        snprintf(result + olen - 1, need - (olen - 1), ", \"%s\": %s}", key, val ? "true" : "false");
    }
    return result;
}

const char* dex_json_set_long(const char* obj, const char* key, long val) {
    size_t olen = strlen(obj);
    size_t klen = strlen(key);
    size_t need = olen + klen + 32;
    char* result = (char*)malloc(need);
    if (!result) return obj;
    if (olen == 2) {
        snprintf(result, need, "{\"%s\": %ld}", key, val);
    } else {
        memcpy(result, obj, olen - 1);
        snprintf(result + olen - 1, need - (olen - 1), ", \"%s\": %ld}", key, val);
    }
    return result;
}

const char* dex_json_set_double(const char* obj, const char* key, double val) {
    size_t olen = strlen(obj);
    size_t klen = strlen(key);
    size_t need = olen + klen + 64;
    char* result = (char*)malloc(need);
    if (!result) return obj;
    if (olen == 2) {
        snprintf(result, need, "{\"%s\": %g}", key, val);
    } else {
        memcpy(result, obj, olen - 1);
        snprintf(result + olen - 1, need - (olen - 1), ", \"%s\": %g}", key, val);
    }
    return result;
}

const char* dex_json_set_obj(const char* obj, const char* key, const char* val) {
    size_t olen = strlen(obj);
    size_t klen = strlen(key);
    size_t vlen = strlen(val);
    // non-empty: `, "key": val}` = 7 literal chars + klen + vlen + null
    size_t need = olen + klen + vlen + 8;
    char* result = (char*)malloc(need);
    if (!result) return obj;
    if (olen == 2) {
        snprintf(result, need, "{\"%s\": %s}", key, val);
    } else {
        memcpy(result, obj, olen - 1);
        snprintf(result + olen - 1, need - (olen - 1), ", \"%s\": %s}", key, val);
    }
    return result;
}

// --- JSON get (parse) ---

static const char* dex_json_skip_value(const char* p);

#define DEX_JSON_SKIP_WS(p) while (*(p)==' '||*(p)=='\t'||*(p)=='\n'||*(p)=='\r') (p)++

// Find the raw value for a key in a JSON object string.
// Returns pointer into json (or NULL). Does NOT allocate.
//
// Walks the object one member at a time rather than scanning for a matching
// quoted run, so it only ever matches a real key at this object's own level:
// a key buried in a nested object is not visible here, and a *value* that
// happens to read like the key can never be mistaken for one.
static const char* dex_json_find_value(const char* json, const char* key) {
    size_t klen = strlen(key);
    const char* p = json;

    DEX_JSON_SKIP_WS(p);
    if (*p != '{') return NULL;
    p++;

    while (*p) {
        DEX_JSON_SKIP_WS(p);
        if (*p == ',') { p++; continue; }
        if (*p == '}') return NULL;
        if (*p != '"') return NULL; /* malformed */

        const char* kstart = ++p;
        while (*p && *p != '"') {
            if (*p == '\\' && p[1]) p++;
            p++;
        }
        if (*p != '"') return NULL;
        size_t this_klen = (size_t)(p - kstart);
        p++;

        DEX_JSON_SKIP_WS(p);
        if (*p != ':') return NULL;
        p++;
        DEX_JSON_SKIP_WS(p);

        if (this_klen == klen && memcmp(kstart, key, klen) == 0) {
            return p;
        }
        p = dex_json_skip_value(p);
    }
    return NULL;
}

const char* dex_json_get(const char* json, const char* key) {
    const char* val = dex_json_find_value(json, key);
    if (!val) return strdup("");
    if (*val == '"') {
        // Unescaped here, via the shared decoder. This used to find the closing
        // quote with strchr and copy the bytes verbatim, which cut the value in
        // half at the first \" and left every other escape as literal text.
        const char* cursor = val;
        char* decoded = NULL;
        size_t declen = 0;
        if (!dex_json_unescape_string(&cursor, &decoded, &declen)) return strdup("");
        return decoded;
    }
    // Non-string value — return it raw. Objects and arrays are scanned with
    // brace matching so a nested structure comes back whole rather than being
    // cut at its first comma; scalars still stop at the usual delimiters.
    const char* end = dex_json_skip_value(val);
    size_t len = (size_t)(end - val);
    char* result = (char*)malloc(len + 1);
    if (!result) return strdup("");
    memcpy(result, val, len);
    result[len] = '\0';
    return result;
}

int dex_json_get_int(const char* json, const char* key) {
    const char* val = dex_json_find_value(json, key);
    if (!val) return 0;
    return atoi(val);
}

_Bool dex_json_get_bool(const char* json, const char* key) {
    const char* val = dex_json_find_value(json, key);
    if (!val) return 0;
    return strncmp(val, "true", 4) == 0;
}

long dex_json_get_long(const char* json, const char* key) {
    const char* val = dex_json_find_value(json, key);
    if (!val) return 0;
    return atol(val);
}

double dex_json_get_double(const char* json, const char* key) {
    const char* val = dex_json_find_value(json, key);
    if (!val) return 0.0;
    return atof(val);
}

// --- JSON array support ---

// Skip a JSON value starting at p, tracking nesting. Returns pointer past the value.
static const char* dex_json_skip_value(const char* p) {
    while (*p == ' ' || *p == '\t' || *p == '\n' || *p == '\r') p++;
    if (*p == '"') {
        p++;
        while (*p && *p != '"') {
            if (*p == '\\') p++; // skip escaped char
            if (*p) p++;
        }
        if (*p == '"') p++;
        return p;
    }
    if (*p == '{' || *p == '[') {
        char open = *p, close_ch = (*p == '{') ? '}' : ']';
        int depth = 1;
        p++;
        while (*p && depth > 0) {
            if (*p == '"') {
                p++;
                while (*p && *p != '"') {
                    if (*p == '\\') p++;
                    if (*p) p++;
                }
                if (*p == '"') p++;
                continue;
            }
            if (*p == open) depth++;
            else if (*p == close_ch) depth--;
            p++;
        }
        return p;
    }
    // number, true, false, null
    while (*p && *p != ',' && *p != ']' && *p != '}' && *p != ' ' && *p != '\n' && *p != '\r' && *p != '\t') p++;
    return p;
}

int dex_json_array_len(const char* json) {
    const char* p = json;
    while (*p == ' ' || *p == '\t' || *p == '\n' || *p == '\r') p++;
    if (*p != '[') return 0;
    p++;
    while (*p == ' ' || *p == '\t' || *p == '\n' || *p == '\r') p++;
    if (*p == ']') return 0;
    int count = 0;
    while (*p && *p != ']') {
        p = dex_json_skip_value(p);
        count++;
        while (*p == ' ' || *p == '\t' || *p == '\n' || *p == '\r') p++;
        if (*p == ',') p++;
        while (*p == ' ' || *p == '\t' || *p == '\n' || *p == '\r') p++;
    }
    return count;
}

const char* dex_json_array_get(const char* json, int index) {
    const char* p = json;
    while (*p == ' ' || *p == '\t' || *p == '\n' || *p == '\r') p++;
    if (*p != '[') return strdup("");
    p++;
    int i = 0;
    while (*p && *p != ']') {
        while (*p == ' ' || *p == '\t' || *p == '\n' || *p == '\r') p++;
        const char* start = p;
        p = dex_json_skip_value(p);
        if (i == index) {
            // Extract the value from start to p
            size_t len = (size_t)(p - start);
            // If it's a quoted string, strip quotes
            if (*start == '"' && len >= 2 && *(p-1) == '"') {
                len -= 2;
                char* result = (char*)malloc(len + 1);
                if (!result) return strdup("");
                memcpy(result, start + 1, len);
                result[len] = '\0';
                return result;
            }
            char* result = (char*)malloc(len + 1);
            if (!result) return strdup("");
            memcpy(result, start, len);
            result[len] = '\0';
            return result;
        }
        i++;
        while (*p == ' ' || *p == '\t' || *p == '\n' || *p == '\r') p++;
        if (*p == ',') p++;
    }
    return strdup("");
}

const char* dex_json_array_get_raw(const char* json, int index) {
    const char* p = json;
    while (*p == ' ' || *p == '\t' || *p == '\n' || *p == '\r') p++;
    if (*p != '[') return strdup("");
    p++;
    int i = 0;
    while (*p && *p != ']') {
        while (*p == ' ' || *p == '\t' || *p == '\n' || *p == '\r') p++;
        const char* start = p;
        p = dex_json_skip_value(p);
        if (i == index) {
            size_t len = (size_t)(p - start);
            char* result = (char*)malloc(len + 1);
            if (!result) return strdup("");
            memcpy(result, start, len);
            result[len] = '\0';
            return result;
        }
        i++;
        while (*p == ' ' || *p == '\t' || *p == '\n' || *p == '\r') p++;
        if (*p == ',') p++;
    }
    return strdup("");
}

const char* dex_json_array_new(void) {
    char* s = (char*)malloc(3);
    if (!s) return strdup("");
    s[0] = '['; s[1] = ']'; s[2] = '\0';
    return s;
}

const char* dex_json_array_push_str(const char* arr, const char* val) {
    size_t alen = strlen(arr);
    size_t evlen = dex_json_escaped_len(val);
    size_t need = alen + evlen + 8;
    char* result = (char*)malloc(need);
    if (!result) return arr;
    size_t pos = 0;
    if (alen == 2) {
        result[pos++] = '[';
    } else {
        memcpy(result, arr, alen - 1);
        pos = alen - 1;
        result[pos++] = ',';
        result[pos++] = ' ';
    }
    result[pos++] = '"';
    pos += dex_json_escape(result + pos, val);
    result[pos++] = '"';
    result[pos++] = ']';
    result[pos] = '\0';
    return result;
}

const char* dex_json_array_push_int(const char* arr, int val) {
    size_t alen = strlen(arr);
    size_t need = alen + 32;
    char* result = (char*)malloc(need);
    if (!result) return arr;
    if (alen == 2) {
        snprintf(result, need, "[%d]", val);
    } else {
        memcpy(result, arr, alen - 1);
        snprintf(result + alen - 1, need - (alen - 1), ", %d]", val);
    }
    return result;
}

const char* dex_json_array_push_long(const char* arr, long val) {
    size_t alen = strlen(arr);
    size_t need = alen + 32;
    char* result = (char*)malloc(need);
    if (!result) return arr;
    if (alen == 2) {
        snprintf(result, need, "[%ld]", val);
    } else {
        memcpy(result, arr, alen - 1);
        snprintf(result + alen - 1, need - (alen - 1), ", %ld]", val);
    }
    return result;
}

const char* dex_json_array_push_double(const char* arr, double val) {
    size_t alen = strlen(arr);
    size_t need = alen + 64;
    char* result = (char*)malloc(need);
    if (!result) return arr;
    if (alen == 2) {
        snprintf(result, need, "[%g]", val);
    } else {
        memcpy(result, arr, alen - 1);
        snprintf(result + alen - 1, need - (alen - 1), ", %g]", val);
    }
    return result;
}

const char* dex_json_array_push_bool(const char* arr, _Bool val) {
    size_t alen = strlen(arr);
    size_t need = alen + 16;
    char* result = (char*)malloc(need);
    if (!result) return arr;
    const char* vs = val ? "true" : "false";
    if (alen == 2) {
        snprintf(result, need, "[%s]", vs);
    } else {
        memcpy(result, arr, alen - 1);
        snprintf(result + alen - 1, need - (alen - 1), ", %s]", vs);
    }
    return result;
}

// --- Struct encode/decode ---

#ifndef DEX_STRUCT_FIELD_DESC_DEFINED
#define DEX_STRUCT_FIELD_DESC_DEFINED
typedef struct DexStructFieldDesc {
    const char* name;
    size_t offset;
    int kind; // 0=int,1=bool,2=string,3=long,4=double,5=nested struct,6=array,7=unsupported
    int num_sub;                     // kind 5: field count of the nested struct
    struct DexStructFieldDesc* sub;  // kind 5: descriptors for the nested struct
    // kind 6: codecs emitted by codegen. Held as pointers so this runtime never
    // has to link against the array runtime, which is emitted conditionally.
    const char* (*enc_arr)(void* field);
    void (*dec_arr)(const char* json, void* field);
    int flags;                       // #[json(...)] / #[tuple] modifiers
} DexStructFieldDesc;
#endif

#ifndef DEX_JSON_FLAGS_DEFINED
#define DEX_JSON_FLAGS_DEFINED
#define DEX_JSON_F_OMITEMPTY 1  // skip on encode when the field holds its zero value
#define DEX_JSON_F_RAW       2  // string field spliced verbatim, not quoted
#define DEX_JSON_F_SUBTUPLE  4  // kind 5 nested struct is #[tuple]-encoded
#endif

// dex_json_field_is_empty reports whether a field currently holds its zero value,
// which is what #[json(omitempty)] suppresses. Arrays are empty when their codec
// yields "[]"; nested structs are never considered empty, matching Go, where a
// struct value has no meaningful emptiness.
static int dex_json_field_is_empty(void* data, DexStructFieldDesc* fd) {
    void* p = (char*)data + fd->offset;
    switch (fd->kind) {
    case 0: return *(int*)p == 0;
    case 1: return *(_Bool*)p == 0;
    case 2: { DexString* s = *(DexString**)p; return !s || !s->data || s->data[0] == '\0'; }
    case 3: return *(long*)p == 0;
    case 4: return *(double*)p == 0.0;
    case 6: {
        if (!fd->enc_arr) return 1;
        const char* enc = fd->enc_arr(p);
        int empty = !enc || enc[0] == '\0' || (enc[0] == '[' && enc[1] == ']');
        free((void*)enc);
        return empty;
    }
    default: return 0;
    }
}

// dex_json_encode_field_value writes just the value half of a field — no key, no
// separator — growing *buf as needed. Shared by the object and tuple encoders so
// the two never disagree about how a value is rendered. Returns the new position.
static int dex_json_encode_field_value(char** buf, size_t* cap, int pos,
                                       void* data, DexStructFieldDesc* fd);

const char* dex_json_encode_tuple(void* data, int num_fields, DexStructFieldDesc* fields);
void dex_json_decode_tuple(const char* json, void* out, int num_fields, DexStructFieldDesc* fields);

// dex_json_unquote turns the verbatim JSON text of a string value into its
// decoded contents, resolving backslash escapes. A value that is not a quoted
// string is copied through unchanged, so a caller that guessed wrong about the
// shape gets the raw text rather than an empty result.
static char* dex_json_unquote(const char* raw) {
    if (!raw) return strdup("");
    if (*raw != '"') return strdup(raw);
    const char* p = raw + 1;
    size_t cap = strlen(raw) + 1;
    char* out = (char*)malloc(cap);
    if (!out) return strdup("");
    size_t n = 0;
    while (*p && *p != '"') {
        if (*p == '\\' && p[1]) {
            p++;
            switch (*p) {
            case 'n': out[n++] = '\n'; break;
            case 't': out[n++] = '\t'; break;
            case 'r': out[n++] = '\r'; break;
            case 'b': out[n++] = '\b'; break;
            case 'f': out[n++] = '\f'; break;
            case '/': out[n++] = '/'; break;
            case '"': out[n++] = '"'; break;
            case '\\': out[n++] = '\\'; break;
            case 'u': {
                // \uXXXX — decoded to UTF-8. Surrogate pairs are combined so
                // characters outside the BMP survive the round trip.
                unsigned int cp = 0;
                int digits = 0;
                for (int i = 1; i <= 4 && p[i]; i++) {
                    char c = p[i];
                    int v;
                    if (c >= '0' && c <= '9') v = c - '0';
                    else if (c >= 'a' && c <= 'f') v = c - 'a' + 10;
                    else if (c >= 'A' && c <= 'F') v = c - 'A' + 10;
                    else break;
                    cp = (cp << 4) | (unsigned)v;
                    digits++;
                }
                if (digits < 4) { out[n++] = 'u'; break; }
                p += 4;
                if (cp >= 0xD800 && cp <= 0xDBFF && p[1] == '\\' && p[2] == 'u') {
                    unsigned int lo = 0;
                    int ld = 0;
                    for (int i = 3; i <= 6 && p[i]; i++) {
                        char c = p[i];
                        int v;
                        if (c >= '0' && c <= '9') v = c - '0';
                        else if (c >= 'a' && c <= 'f') v = c - 'a' + 10;
                        else if (c >= 'A' && c <= 'F') v = c - 'A' + 10;
                        else break;
                        lo = (lo << 4) | (unsigned)v;
                        ld++;
                    }
                    if (ld == 4 && lo >= 0xDC00 && lo <= 0xDFFF) {
                        cp = 0x10000 + ((cp - 0xD800) << 10) + (lo - 0xDC00);
                        p += 6;
                    }
                }
                if (cp < 0x80) {
                    out[n++] = (char)cp;
                } else if (cp < 0x800) {
                    out[n++] = (char)(0xC0 | (cp >> 6));
                    out[n++] = (char)(0x80 | (cp & 0x3F));
                } else if (cp < 0x10000) {
                    out[n++] = (char)(0xE0 | (cp >> 12));
                    out[n++] = (char)(0x80 | ((cp >> 6) & 0x3F));
                    out[n++] = (char)(0x80 | (cp & 0x3F));
                } else {
                    out[n++] = (char)(0xF0 | (cp >> 18));
                    out[n++] = (char)(0x80 | ((cp >> 12) & 0x3F));
                    out[n++] = (char)(0x80 | ((cp >> 6) & 0x3F));
                    out[n++] = (char)(0x80 | (cp & 0x3F));
                }
                break;
            }
            default: out[n++] = *p; break;
            }
            p++;
            continue;
        }
        out[n++] = *p++;
    }
    out[n] = '\0';
    return out;
}

// dex_json_grow ensures at least `need` free bytes past pos. Returns 0 on
// allocation failure, in which case the caller terminates the buffer as-is.
static int dex_json_grow(char** buf, size_t* cap, int pos, size_t need) {
    while ((size_t)pos + need + 2 > *cap) {
        size_t next = *cap * 2;
        char* tmp = (char*)realloc(*buf, next);
        if (!tmp) return 0;
        *buf = tmp;
        *cap = next;
    }
    return 1;
}

static int dex_json_encode_field_value(char** buf, size_t* cap, int pos,
                                       void* data, DexStructFieldDesc* fd) {
    void* p = (char*)data + fd->offset;
    switch (fd->kind) {
    case 0: // int
        if (!dex_json_grow(buf, cap, pos, 32)) return pos;
        return pos + snprintf(*buf + pos, *cap - pos, "%d", *(int*)p);
    case 1: // bool
        if (!dex_json_grow(buf, cap, pos, 8)) return pos;
        return pos + snprintf(*buf + pos, *cap - pos, "%s", (*(_Bool*)p) ? "true" : "false");
    case 3: // long
        if (!dex_json_grow(buf, cap, pos, 32)) return pos;
        return pos + snprintf(*buf + pos, *cap - pos, "%ld", *(long*)p);
    case 4: // double
        if (!dex_json_grow(buf, cap, pos, 40)) return pos;
        return pos + snprintf(*buf + pos, *cap - pos, "%g", *(double*)p);
    case 2: { // string — quoted and escaped, or spliced verbatim under #[json(raw)]
        DexString* s = *(DexString**)p;
        const char* sdata = (s && s->data) ? s->data : "";
        if (fd->flags & DEX_JSON_F_RAW) {
            // A raw field carries pre-formed JSON. An empty one would produce
            // invalid output spliced bare, so it degrades to null.
            const char* src = sdata[0] ? sdata : "null";
            size_t len = strlen(src);
            if (!dex_json_grow(buf, cap, pos, len)) return pos;
            memcpy(*buf + pos, src, len);
            return pos + (int)len;
        }
        if (!dex_json_grow(buf, cap, pos, dex_json_escaped_len(sdata) + 4)) return pos;
        (*buf)[pos++] = '"';
        pos += (int)dex_json_escape(*buf + pos, sdata);
        (*buf)[pos++] = '"';
        return pos;
    }
    case 5: { // nested struct — encode recursively and splice in
        const char* sub = (fd->flags & DEX_JSON_F_SUBTUPLE)
                              ? dex_json_encode_tuple(p, fd->num_sub, fd->sub)
                              : dex_json_encode_struct(p, fd->num_sub, fd->sub);
        const char* subs = sub ? sub : "{}";
        size_t len = strlen(subs);
        if (!dex_json_grow(buf, cap, pos, len)) { free((void*)sub); return pos; }
        memcpy(*buf + pos, subs, len);
        pos += (int)len;
        free((void*)sub);
        return pos;
    }
    case 6: { // array — encoded by a codegen-supplied codec
        const char* sub = fd->enc_arr ? fd->enc_arr(p) : NULL;
        const char* subs = sub ? sub : "[]";
        size_t len = strlen(subs);
        if (!dex_json_grow(buf, cap, pos, len)) { free((void*)sub); return pos; }
        memcpy(*buf + pos, subs, len);
        pos += (int)len;
        free((void*)sub);
        return pos;
    }
    default: // kind 7 — no codec; callers skip these before getting here
        if (!dex_json_grow(buf, cap, pos, 8)) return pos;
        memcpy(*buf + pos, "null", 4);
        return pos + 4;
    }
}

// dex_json_field_skipped reports whether a field is left out of the encoded
// object entirely: either it has no codec, or #[json(omitempty)] applies and the
// field currently holds its zero value.
static int dex_json_field_skipped(void* data, DexStructFieldDesc* fd) {
    if (fd->kind == 7) return 1;
    if ((fd->flags & DEX_JSON_F_OMITEMPTY) && dex_json_field_is_empty(data, fd)) return 1;
    return 0;
}

const char* dex_json_encode_struct(void* data, int num_fields, DexStructFieldDesc* fields) {
    size_t cap = 256;
    char* buf = (char*)malloc(cap);
    if (!buf) return strdup("{}");
    int pos = 0;
    buf[pos++] = '{';
    int wrote = 0;
    for (int f = 0; f < num_fields; f++) {
<<<<<<< Updated upstream
        if (fields[f].kind == 7) continue; // no codec — omit rather than emit garbage
        if (wrote) { buf[pos++] = ','; }
        wrote = 1;
        if ((size_t)pos + 256 > cap) {
            cap *= 2;
            char* tmp = (char*)realloc(buf, cap);
            if (!tmp) { buf[pos] = '\0'; return buf; }
            buf = tmp;
        }
        int n = 0;
        switch (fields[f].kind) {
        case 0: // int
            n = snprintf(buf + pos, cap - pos, "\"%s\":%d", fields[f].name, *(int*)((char*)data + fields[f].offset));
            break;
        case 1: // bool
            n = snprintf(buf + pos, cap - pos, "\"%s\":%s", fields[f].name, (*(_Bool*)((char*)data + fields[f].offset)) ? "true" : "false");
            break;
        case 2: // string
            { DexString* s = *(DexString**)((char*)data + fields[f].offset);
              const char* sdata = s ? s->data : "";
              size_t eklen = dex_json_escaped_len(fields[f].name);
              size_t evlen = dex_json_escaped_len(sdata);
              size_t field_need = eklen + evlen + 8; // "key": "val"
              while ((size_t)pos + field_need > cap) {
                  cap *= 2;
                  char* tmp = (char*)realloc(buf, cap);
                  if (!tmp) { buf[pos] = '\0'; return buf; }
                  buf = tmp;
              }
              buf[pos++] = '"';
              pos += dex_json_escape(buf + pos, fields[f].name);
              buf[pos++] = '"';
              buf[pos++] = ':';
              buf[pos++] = '"';
              pos += dex_json_escape(buf + pos, sdata);
              buf[pos++] = '"';
              n = 0; }
            break;
        case 3: // long
            n = snprintf(buf + pos, cap - pos, "\"%s\":%ld", fields[f].name, *(long*)((char*)data + fields[f].offset));
            break;
        case 4: // double
            n = snprintf(buf + pos, cap - pos, "\"%s\":%g", fields[f].name, *(double*)((char*)data + fields[f].offset));
            break;
        case 6: // array — encoded by a codegen-supplied codec
            { const char* sub = fields[f].enc_arr ? fields[f].enc_arr((char*)data + fields[f].offset) : NULL;
              const char* subs = sub ? sub : "[]";
              size_t eklen = dex_json_escaped_len(fields[f].name);
              size_t sublen = strlen(subs);
              size_t field_need = eklen + sublen + 8;
              while ((size_t)pos + field_need > cap) {
                  cap *= 2;
                  char* tmp = (char*)realloc(buf, cap);
                  if (!tmp) { buf[pos] = '\0'; free((void*)sub); return buf; }
                  buf = tmp;
              }
              buf[pos++] = '"';
              pos += dex_json_escape(buf + pos, fields[f].name);
              buf[pos++] = '"';
              buf[pos++] = ':';
              memcpy(buf + pos, subs, sublen);
              pos += (int)sublen;
              free((void*)sub);
              n = 0; }
            break;
        case 5: // nested struct — encode recursively and splice in
            { const char* sub = dex_json_encode_struct((char*)data + fields[f].offset,
                                                       fields[f].num_sub, fields[f].sub);
              size_t eklen = dex_json_escaped_len(fields[f].name);
              size_t sublen = strlen(sub);
              size_t field_need = eklen + sublen + 8; // "key": {...}
              while ((size_t)pos + field_need > cap) {
                  cap *= 2;
                  char* tmp = (char*)realloc(buf, cap);
                  if (!tmp) { buf[pos] = '\0'; free((void*)sub); return buf; }
                  buf = tmp;
              }
              buf[pos++] = '"';
              pos += dex_json_escape(buf + pos, fields[f].name);
              buf[pos++] = '"';
              buf[pos++] = ':';
              memcpy(buf + pos, sub, sublen);
              pos += (int)sublen;
              free((void*)sub);
              n = 0; }
            break;
        }
        pos += n;
        if ((size_t)pos + 256 > cap) {
            cap *= 2;
            char* tmp = (char*)realloc(buf, cap);
            if (!tmp) { buf[pos] = '\0'; return buf; }
            buf = tmp;
        }
=======
        if (dex_json_field_skipped(data, &fields[f])) continue;
        if (!dex_json_grow(&buf, &cap, pos, dex_json_escaped_len(fields[f].name) + 8)) break;
        if (wrote) { buf[pos++] = ','; buf[pos++] = ' '; }
        wrote = 1;
        buf[pos++] = '"';
        pos += (int)dex_json_escape(buf + pos, fields[f].name);
        buf[pos++] = '"';
        buf[pos++] = ':';
        buf[pos++] = ' ';
        pos = dex_json_encode_field_value(&buf, &cap, pos, data, &fields[f]);
>>>>>>> Stashed changes
    }
    if (!dex_json_grow(&buf, &cap, pos, 2)) { buf[pos] = '\0'; return buf; }
    buf[pos++] = '}';
    buf[pos] = '\0';
    return buf;
}

// dex_json_encode_tuple renders a #[tuple] struct as a positional JSON array.
// Field names never reach the wire, so #[json("name")] is inert here; the field
// order in the source is the array order. #[json(omitempty)] is ignored too —
// dropping an element would shift every position after it.
const char* dex_json_encode_tuple(void* data, int num_fields, DexStructFieldDesc* fields) {
    size_t cap = 256;
    char* buf = (char*)malloc(cap);
    if (!buf) return strdup("[]");
    int pos = 0;
    buf[pos++] = '[';
    for (int f = 0; f < num_fields; f++) {
        if (f > 0) {
            if (!dex_json_grow(&buf, &cap, pos, 2)) break;
            buf[pos++] = ',';
        }
        pos = dex_json_encode_field_value(&buf, &cap, pos, data, &fields[f]);
    }
    if (!dex_json_grow(&buf, &cap, pos, 2)) { buf[pos] = '\0'; return buf; }
    buf[pos++] = ']';
    buf[pos] = '\0';
    return buf;
}

// dex_json_decode_field_value reads one already-extracted JSON value `raw` into a
// field. `raw` is the verbatim JSON text of the value, quotes included for
// strings. Shared by the object and tuple decoders.
static void dex_json_decode_field_value(const char* raw, void* out, DexStructFieldDesc* fd) {
    void* p = (char*)out + fd->offset;
    int is_null = (!raw || strncmp(raw, "null", 4) == 0);
    switch (fd->kind) {
    case 0: *(int*)p = is_null ? 0 : atoi(raw); break;
    case 1: *(_Bool*)p = (!is_null && strncmp(raw, "true", 4) == 0); break;
    case 3: *(long*)p = is_null ? 0 : atol(raw); break;
    case 4: *(double*)p = is_null ? 0.0 : atof(raw); break;
    case 2: { // string
        if (fd->flags & DEX_JSON_F_RAW) {
            // Raw fields keep the JSON text exactly as it appeared, so an object
            // or array payload survives a decode/encode round trip untouched.
            *(DexString**)p = dex_string_from_cstr(strdup(is_null ? "" : raw));
        } else if (is_null) {
            *(DexString**)p = dex_string_from_cstr(strdup(""));
        } else {
            *(DexString**)p = dex_string_from_cstr(dex_json_unquote(raw));
        }
        break;
    }
    case 5: { // nested struct
        const char* sub = is_null ? "{}" : raw;
        if (fd->flags & DEX_JSON_F_SUBTUPLE) {
            dex_json_decode_tuple(sub, p, fd->num_sub, fd->sub);
        } else {
            dex_json_decode_struct(sub, p, fd->num_sub, fd->sub);
        }
        break;
    }
    case 6: // array
        if (fd->dec_arr) fd->dec_arr(is_null ? "[]" : raw, p);
        break;
    }
}

// dex_json_extract_value returns a freshly allocated copy of the verbatim JSON
// text for `key`, or NULL when the key is absent.
static char* dex_json_extract_value(const char* json, const char* key) {
    const char* val = dex_json_find_value(json, key);
    if (!val) return NULL;
    const char* end = dex_json_skip_value(val);
    size_t len = (size_t)(end - val);
    char* result = (char*)malloc(len + 1);
    if (!result) return NULL;
    memcpy(result, val, len);
    result[len] = '\0';
    return result;
}

void dex_json_decode_struct(const char* json, void* out, int num_fields, DexStructFieldDesc* fields) {
    for (int f = 0; f < num_fields; f++) {
        if (fields[f].kind == 7) continue;
        char* raw = dex_json_extract_value(json, fields[f].name);
        // An absent key leaves the field at its zero value, matching Go. Passing
        // NULL through still initialises strings and arrays, which must not be
        // left as null pointers.
        dex_json_decode_field_value(raw, out, &fields[f]);
        free(raw);
    }
}

// dex_json_decode_tuple reads a positional JSON array into a #[tuple] struct.
// Element i fills field i; a short array leaves the remaining fields zeroed.
void dex_json_decode_tuple(const char* json, void* out, int num_fields, DexStructFieldDesc* fields) {
    int len = dex_json_array_len(json);
    for (int f = 0; f < num_fields; f++) {
        if (fields[f].kind == 7) continue;
        char* raw = NULL;
        if (f < len) raw = (char*)dex_json_array_get_raw(json, f);
        dex_json_decode_field_value(raw, out, &fields[f]);
        free(raw);
    }
}

// --- Strict validation, used by the checked (optional-returning) decode ---

static int dex_json_check_value(const char** pp);

static int dex_json_check_string(const char** pp) {
    const char* p = *pp;
    if (*p != '"') return 0;
    p++;
    while (*p && *p != '"') {
        if (*p == '\\') { p++; if (!*p) return 0; }
        p++;
    }
    if (*p != '"') return 0;
    *pp = p + 1;
    return 1;
}

static int dex_json_check_value(const char** pp) {
    const char* p = *pp;
    DEX_JSON_SKIP_WS(p);
    if (*p == '"') { *pp = p; return dex_json_check_string(pp); }
    if (*p == '{') {
        p++;
        DEX_JSON_SKIP_WS(p);
        if (*p == '}') { *pp = p + 1; return 1; }
        for (;;) {
            DEX_JSON_SKIP_WS(p);
            *pp = p;
            if (!dex_json_check_string(pp)) return 0;
            p = *pp;
            DEX_JSON_SKIP_WS(p);
            if (*p != ':') return 0;
            p++;
            *pp = p;
            if (!dex_json_check_value(pp)) return 0;
            p = *pp;
            DEX_JSON_SKIP_WS(p);
            if (*p == ',') { p++; continue; }
            if (*p == '}') { *pp = p + 1; return 1; }
            return 0;
        }
    }
    if (*p == '[') {
        p++;
        DEX_JSON_SKIP_WS(p);
        if (*p == ']') { *pp = p + 1; return 1; }
        for (;;) {
            *pp = p;
            if (!dex_json_check_value(pp)) return 0;
            p = *pp;
            DEX_JSON_SKIP_WS(p);
            if (*p == ',') { p++; continue; }
            if (*p == ']') { *pp = p + 1; return 1; }
            return 0;
        }
    }
    if (strncmp(p, "true", 4) == 0)  { *pp = p + 4; return 1; }
    if (strncmp(p, "false", 5) == 0) { *pp = p + 5; return 1; }
    if (strncmp(p, "null", 4) == 0)  { *pp = p + 4; return 1; }
    if (*p == '-') p++;
    if (!(*p >= '0' && *p <= '9')) return 0;
    while (*p >= '0' && *p <= '9') p++;
    if (*p == '.') {
        p++;
        if (!(*p >= '0' && *p <= '9')) return 0;
        while (*p >= '0' && *p <= '9') p++;
    }
    if (*p == 'e' || *p == 'E') {
        p++;
        if (*p == '+' || *p == '-') p++;
        if (!(*p >= '0' && *p <= '9')) return 0;
        while (*p >= '0' && *p <= '9') p++;
    }
    *pp = p;
    return 1;
}

// Returns 1 when the whole string is one well-formed JSON value.
int dex_json_valid(const char* json) {
    if (!json) return 0;
    const char* p = json;
    if (!dex_json_check_value(&p)) return 0;
    DEX_JSON_SKIP_WS(p);
    return *p == '\0';
}

// Returns 1 when the string is one well-formed JSON array.
int dex_json_is_array(const char* json) {
    if (!dex_json_valid(json)) return 0;
    const char* p = json;
    DEX_JSON_SKIP_WS(p);
    return *p == '[';
}

// Does the raw value at v plausibly fit a field of this descriptor kind?
// Kind 2 (string) also accepts a raw object/array so that raw-JSON passthrough
// into a string field keeps working.
static int dex_json_check_fields(const char* json, int num_fields, DexStructFieldDesc* fields);
static int dex_json_check_tuple_fields(const char* json, int num_fields, DexStructFieldDesc* fields);

static int dex_json_kind_ok(const char* v, int kind) {
    switch (kind) {
    case 0: case 3: case 4: return (*v == '-' || (*v >= '0' && *v <= '9'));
    case 1: return (strncmp(v, "true", 4) == 0 || strncmp(v, "false", 5) == 0);
    case 2: return (*v == '"' || *v == '{' || *v == '[');
    case 5: return (*v == '{');
    case 6: return (*v == '[');
    }
    return 1;
}

// A key that is absent, or explicitly null, is fine — the field keeps its zero
// value, matching what encoding/json does for missing keys. A key that is
// present with an incompatible type is a decode failure.
// dex_json_check_field validates one already-located value against a field
// descriptor. Shared by the object and tuple checkers.
static int dex_json_check_field(const char* v, DexStructFieldDesc* fd) {
    if (!v) return 1;
    if (strncmp(v, "null", 4) == 0) return 1;
    // A #[json(raw)] field is a passthrough for arbitrary JSON, so any
    // well-formed value fits it.
    if ((fd->flags & DEX_JSON_F_RAW) && fd->kind == 2) return 1;
    if (fd->kind == 5 && (fd->flags & DEX_JSON_F_SUBTUPLE)) {
        if (*v != '[') return 0;
        return dex_json_check_tuple_fields(v, fd->num_sub, fd->sub);
    }
    if (!dex_json_kind_ok(v, fd->kind)) return 0;
    if (fd->kind == 5) return dex_json_check_fields(v, fd->num_sub, fd->sub);
    return 1;
}

static int dex_json_check_fields(const char* json, int num_fields, DexStructFieldDesc* fields) {
    for (int f = 0; f < num_fields; f++) {
        if (fields[f].kind == 7) continue;
        const char* v = dex_json_find_value(json, fields[f].name);
        if (!dex_json_check_field(v, &fields[f])) return 0;
    }
    return 1;
}

// dex_json_check_tuple_fields validates a positional array against a #[tuple]
// struct: each present element must fit the field in the same position. A short
// array is accepted — the trailing fields simply keep their zero values, which
// is how an optional trailing element in a wire frame behaves.
static int dex_json_check_tuple_fields(const char* json, int num_fields, DexStructFieldDesc* fields) {
    int len = dex_json_array_len(json);
    for (int f = 0; f < num_fields && f < len; f++) {
        if (fields[f].kind == 7) continue;
        char* v = (char*)dex_json_array_get_raw(json, f);
        int ok = dex_json_check_field(v, &fields[f]);
        free(v);
        if (!ok) return 0;
    }
    return 1;
}

// Decodes only if the input is a well-formed JSON object whose present keys have
// types compatible with the struct. Returns 1 on success, 0 without touching out.
int dex_json_decode_struct_checked(const char* json, void* out, int num_fields, DexStructFieldDesc* fields) {
    if (!dex_json_valid(json)) return 0;
    const char* p = json;
    DEX_JSON_SKIP_WS(p);
    if (*p != '{') return 0;
    if (!dex_json_check_fields(json, num_fields, fields)) return 0;
    dex_json_decode_struct(json, out, num_fields, fields);
    return 1;
}

// dex_json_decode_tuple_checked is the #[tuple] counterpart: the payload must be
// a well-formed JSON array whose elements fit the fields positionally.
int dex_json_decode_tuple_checked(const char* json, void* out, int num_fields, DexStructFieldDesc* fields) {
    if (!dex_json_valid(json)) return 0;
    const char* p = json;
    DEX_JSON_SKIP_WS(p);
    if (*p != '[') return 0;
    if (!dex_json_check_tuple_fields(json, num_fields, fields)) return 0;
    dex_json_decode_tuple(json, out, num_fields, fields);
    return 1;
}

const char* dex_json_array_push_obj(const char* arr, const char* obj) {
    size_t alen = strlen(arr);
    size_t olen = strlen(obj);
    size_t need = alen + olen + 4;
    char* result = (char*)malloc(need);
    if (!result) return arr;
    if (alen == 2) {
        snprintf(result, need, "[%s]", obj);
    } else {
        memcpy(result, arr, alen - 1);
        snprintf(result + alen - 1, need - (alen - 1), ", %s]", obj);
    }
    return result;
}
