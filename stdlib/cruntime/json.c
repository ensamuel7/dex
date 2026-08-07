
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

const char* dex_json_new(void) {
    char* s = (char*)malloc(3);
    s[0] = '{'; s[1] = '}'; s[2] = '\0';
    return s;
}

const char* dex_json_set(const char* obj, const char* key, const char* val) {
    size_t olen = strlen(obj);
    size_t klen = strlen(key);
    size_t vlen = strlen(val);
    // "key": "val"  => 4 quotes + colon + space + comma = 7, plus contents
    size_t need = olen + klen + vlen + 8;
    char* result = (char*)malloc(need);
    if (olen == 2) {
        // empty {}
        snprintf(result, need, "{\"%s\": \"%s\"}", key, val);
    } else {
        // insert before closing }
        memcpy(result, obj, olen - 1);
        snprintf(result + olen - 1, need - (olen - 1), ", \"%s\": \"%s\"}", key, val);
    }
    return result;
}

const char* dex_json_set_int(const char* obj, const char* key, int val) {
    size_t olen = strlen(obj);
    size_t klen = strlen(key);
    size_t need = olen + klen + 32;
    char* result = (char*)malloc(need);
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
    if (olen == 2) {
        snprintf(result, need, "{\"%s\": %g}", key, val);
    } else {
        memcpy(result, obj, olen - 1);
        snprintf(result + olen - 1, need - (olen - 1), ", \"%s\": %g}", key, val);
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
        memcpy(result, val, len);
        result[len] = '\0';
        return result;
    }
    // Non-string value — return raw up to delimiter
    const char* end = val;
    while (*end && *end != ',' && *end != '}' && *end != ']' && *end != ' ' && *end != '\n') end++;
    size_t len = (size_t)(end - val);
    char* result = (char*)malloc(len + 1);
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
                memcpy(result, start + 1, len);
                result[len] = '\0';
                return result;
            }
            char* result = (char*)malloc(len + 1);
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
    s[0] = '['; s[1] = ']'; s[2] = '\0';
    return s;
}

const char* dex_json_array_push_str(const char* arr, const char* val) {
    size_t alen = strlen(arr);
    size_t vlen = strlen(val);
    size_t need = alen + vlen + 8;
    char* result = (char*)malloc(need);
    if (alen == 2) {
        snprintf(result, need, "[\"%s\"]", val);
    } else {
        memcpy(result, arr, alen - 1);
        snprintf(result + alen - 1, need - (alen - 1), ", \"%s\"]", val);
    }
    return result;
}

const char* dex_json_array_push_int(const char* arr, int val) {
    size_t alen = strlen(arr);
    size_t need = alen + 32;
    char* result = (char*)malloc(need);
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
    const char* vs = val ? "true" : "false";
    if (alen == 2) {
        snprintf(result, need, "[%s]", vs);
    } else {
        memcpy(result, arr, alen - 1);
        snprintf(result + alen - 1, need - (alen - 1), ", %s]", vs);
    }
    return result;
}

const char* dex_json_array_push_obj(const char* arr, const char* obj) {
    size_t alen = strlen(arr);
    size_t olen = strlen(obj);
    size_t need = alen + olen + 4;
    char* result = (char*)malloc(need);
    if (alen == 2) {
        snprintf(result, need, "[%s]", obj);
    } else {
        memcpy(result, arr, alen - 1);
        snprintf(result + alen - 1, need - (alen - 1), ", %s]", obj);
    }
    return result;
}
