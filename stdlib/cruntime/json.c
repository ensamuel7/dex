
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
