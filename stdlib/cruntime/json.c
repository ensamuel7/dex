
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
} DexStructFieldDesc;
#endif

const char* dex_json_encode_struct(void* data, int num_fields, DexStructFieldDesc* fields) {
    size_t cap = 256;
    char* buf = (char*)malloc(cap);
    if (!buf) return strdup("");
    int pos = 0;
    buf[pos++] = '{';
    int wrote = 0;
    for (int f = 0; f < num_fields; f++) {
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
            { const char* raw = dex_json_find_value(json, fields[f].name);
              if (raw && strncmp(raw, "null", 4) == 0) {
                  // JSON null decodes to the empty string, not the text "null"
                  *(DexString**)((char*)out + fields[f].offset) = dex_string_from_cstr(strdup(""));
              } else {
                  const char* val = dex_json_get(json, fields[f].name);
                  *(DexString**)((char*)out + fields[f].offset) = dex_string_from_cstr(val);
              } }
            break;
        case 3: // long
            *(long*)((char*)out + fields[f].offset) = dex_json_get_long(json, fields[f].name);
            break;
        case 4: // double
            *(double*)((char*)out + fields[f].offset) = dex_json_get_double(json, fields[f].name);
            break;
        case 6: // array — decoded by a codegen-supplied codec
            { if (fields[f].dec_arr) {
                  const char* raw = dex_json_get(json, fields[f].name);
                  fields[f].dec_arr(raw, (char*)out + fields[f].offset);
                  free((void*)raw);
              } }
            break;
        case 5: // nested struct — decode the raw sub-object recursively
            { const char* sub_json = dex_json_get(json, fields[f].name);
              dex_json_decode_struct(sub_json, (char*)out + fields[f].offset,
                                     fields[f].num_sub, fields[f].sub);
              free((void*)sub_json); }
            break;
        }
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
static int dex_json_check_fields(const char* json, int num_fields, DexStructFieldDesc* fields) {
    for (int f = 0; f < num_fields; f++) {
        const char* v = dex_json_find_value(json, fields[f].name);
        if (!v) continue;
        if (strncmp(v, "null", 4) == 0) continue;
        if (!dex_json_kind_ok(v, fields[f].kind)) return 0;
        if (fields[f].kind == 5) {
            const char* subj = dex_json_get(json, fields[f].name);
            int ok = dex_json_check_fields(subj, fields[f].num_sub, fields[f].sub);
            free((void*)subj);
            if (!ok) return 0;
        }
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
