
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
