
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

// Compute the length of a JSON-escaped version of str (without quotes).
static size_t dex_json_escaped_len(const char* str) {
    size_t len = 0;
    for (const char* p = str; *p; p++) {
        switch (*p) {
        case '"': case '\\': case '/': len += 2; break;
        case '\b': case '\f': case '\n': case '\r': case '\t': len += 2; break;
        default:
            if ((unsigned char)*p < 0x20) {
                len += 6; // \uXXXX
            } else {
                len += 1;
            }
        }
    }
    return len;
}

// Write JSON-escaped version of str into dst (without quotes). Returns chars written.
static size_t dex_json_escape(char* dst, const char* str) {
    size_t pos = 0;
    for (const char* p = str; *p; p++) {
        switch (*p) {
        case '"':  dst[pos++] = '\\'; dst[pos++] = '"'; break;
        case '\\': dst[pos++] = '\\'; dst[pos++] = '\\'; break;
        case '/':  dst[pos++] = '\\'; dst[pos++] = '/'; break;
        case '\b': dst[pos++] = '\\'; dst[pos++] = 'b'; break;
        case '\f': dst[pos++] = '\\'; dst[pos++] = 'f'; break;
        case '\n': dst[pos++] = '\\'; dst[pos++] = 'n'; break;
        case '\r': dst[pos++] = '\\'; dst[pos++] = 'r'; break;
        case '\t': dst[pos++] = '\\'; dst[pos++] = 't'; break;
        default:
            if ((unsigned char)*p < 0x20) {
                pos += snprintf(dst + pos, 7, "\\u%04x", (unsigned char)*p);
            } else {
                dst[pos++] = *p;
            }
        }
    }
    return pos;
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

// Find the raw value for a key in a JSON object string.
// Returns pointer into json (or NULL). Does NOT allocate.
static const char* dex_json_find_value(const char* json, const char* key) {
    size_t klen = strlen(key);
    const char* p = json;
    while (*p) {
        // Find next quote
        p = strchr(p, '"');
        if (!p) return NULL;
        p++; // skip opening quote
        // Check if this key matches
        if (strncmp(p, key, klen) == 0 && p[klen] == '"') {
            p += klen + 1; // past closing quote
            // Skip whitespace and colon
            while (*p == ' ' || *p == '\t' || *p == ':') p++;
            return p;
        }
        // Skip past this quoted string
        const char* end = strchr(p, '"');
        if (!end) return NULL;
        p = end + 1;
    }
    return NULL;
}

const char* dex_json_get(const char* json, const char* key) {
    const char* val = dex_json_find_value(json, key);
    if (!val) return strdup("");
    if (*val == '"') {
        val++;
        const char* end = strchr(val, '"');
        if (!end) return strdup("");
        size_t len = (size_t)(end - val);
        char* result = (char*)malloc(len + 1);
        if (!result) return strdup("");
        memcpy(result, val, len);
        result[len] = '\0';
        return result;
    }
    // Non-string value — return raw up to delimiter
    const char* end = val;
    while (*end && *end != ',' && *end != '}' && *end != ']' && *end != ' ' && *end != '\n') end++;
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
typedef struct {
    const char* name;
    size_t offset;
    int kind; // 0=int, 1=bool, 2=string, 3=long, 4=double
} DexStructFieldDesc;
#endif

const char* dex_json_encode_struct(void* data, int num_fields, DexStructFieldDesc* fields) {
    size_t cap = 256;
    char* buf = (char*)malloc(cap);
    if (!buf) return strdup("");
    int pos = 0;
    buf[pos++] = '{';
    for (int f = 0; f < num_fields; f++) {
        if (f > 0) { buf[pos++] = ','; buf[pos++] = ' '; }
        if ((size_t)pos + 256 > cap) {
            cap *= 2;
            char* tmp = (char*)realloc(buf, cap);
            if (!tmp) { buf[pos] = '\0'; return buf; }
            buf = tmp;
        }
        int n = 0;
        switch (fields[f].kind) {
        case 0: // int
            n = snprintf(buf + pos, cap - pos, "\"%s\": %d", fields[f].name, *(int*)((char*)data + fields[f].offset));
            break;
        case 1: // bool
            n = snprintf(buf + pos, cap - pos, "\"%s\": %s", fields[f].name, (*(_Bool*)((char*)data + fields[f].offset)) ? "true" : "false");
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
              buf[pos++] = ' ';
              buf[pos++] = '"';
              pos += dex_json_escape(buf + pos, sdata);
              buf[pos++] = '"';
              n = 0; }
            break;
        case 3: // long
            n = snprintf(buf + pos, cap - pos, "\"%s\": %ld", fields[f].name, *(long*)((char*)data + fields[f].offset));
            break;
        case 4: // double
            n = snprintf(buf + pos, cap - pos, "\"%s\": %g", fields[f].name, *(double*)((char*)data + fields[f].offset));
            break;
        }
        pos += n;
        if ((size_t)pos + 256 > cap) {
            cap *= 2;
            char* tmp = (char*)realloc(buf, cap);
            if (!tmp) { buf[pos] = '\0'; return buf; }
            buf = tmp;
        }
    }
    buf[pos++] = '}';
    buf[pos] = '\0';
    return buf;
}

void dex_json_decode_struct(const char* json, void* out, int num_fields, DexStructFieldDesc* fields) {
    for (int f = 0; f < num_fields; f++) {
        switch (fields[f].kind) {
        case 0: // int
            *(int*)((char*)out + fields[f].offset) = dex_json_get_int(json, fields[f].name);
            break;
        case 1: // bool
            *(_Bool*)((char*)out + fields[f].offset) = dex_json_get_bool(json, fields[f].name);
            break;
        case 2: // string
            { const char* val = dex_json_get(json, fields[f].name);
              *(DexString**)((char*)out + fields[f].offset) = dex_string_from_cstr(val); }
            break;
        case 3: // long
            *(long*)((char*)out + fields[f].offset) = dex_json_get_long(json, fields[f].name);
            break;
        case 4: // double
            *(double*)((char*)out + fields[f].offset) = dex_json_get_double(json, fields[f].name);
            break;
        }
    }
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
