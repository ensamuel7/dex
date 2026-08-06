// DexLang String Methods Runtime
// Built-in methods on string values: len, contains, startsWith, endsWith,
// indexOf, toLower, toUpper, trim, split, substring, replace, charAt.

#include <ctype.h>

static int dex_str_len(DexString* s) {
    return (int)s->len;
}

static _Bool dex_str_contains(DexString* s, DexString* sub) {
    if (sub->len == 0) return 1;
    if (sub->len > s->len) return 0;
    return strstr(s->data, sub->data) != NULL;
}

static _Bool dex_str_startsWith(DexString* s, DexString* prefix) {
    if (prefix->len > s->len) return 0;
    return memcmp(s->data, prefix->data, prefix->len) == 0;
}

static _Bool dex_str_endsWith(DexString* s, DexString* suffix) {
    if (suffix->len > s->len) return 0;
    return memcmp(s->data + s->len - suffix->len, suffix->data, suffix->len) == 0;
}

static int dex_str_indexOf(DexString* s, DexString* sub) {
    if (sub->len == 0) return 0;
    if (sub->len > s->len) return -1;
    char* found = strstr(s->data, sub->data);
    if (!found) return -1;
    return (int)(found - s->data);
}

static DexString* dex_str_toLower(DexString* s) {
    DexString* result = dex_string_new(s->data, s->len);
    for (size_t i = 0; i < result->len; i++) {
        result->data[i] = (char)tolower((unsigned char)result->data[i]);
    }
    return result;
}

static DexString* dex_str_toUpper(DexString* s) {
    DexString* result = dex_string_new(s->data, s->len);
    for (size_t i = 0; i < result->len; i++) {
        result->data[i] = (char)toupper((unsigned char)result->data[i]);
    }
    return result;
}

static DexString* dex_str_trim(DexString* s) {
    const char* start = s->data;
    const char* end = s->data + s->len;
    while (start < end && isspace((unsigned char)*start)) start++;
    while (end > start && isspace((unsigned char)*(end - 1))) end--;
    return dex_string_new(start, (size_t)(end - start));
}

static DexArrayString* dex_str_split(DexString* s, DexString* delim) {
    DexArrayString* result = dex_array_string_new();
    if (delim->len == 0) {
        // Split into individual characters
        for (size_t i = 0; i < s->len; i++) {
            DexString* ch = dex_string_new(s->data + i, 1);
            dex_array_string_push(result, ch);
            dex_release(ch);
        }
        return result;
    }
    const char* p = s->data;
    const char* end = s->data + s->len;
    while (p <= end) {
        const char* found = strstr(p, delim->data);
        if (!found || found > end) {
            DexString* part = dex_string_new(p, (size_t)(end - p));
            dex_array_string_push(result, part);
            dex_release(part);
            break;
        }
        DexString* part = dex_string_new(p, (size_t)(found - p));
        dex_array_string_push(result, part);
        dex_release(part);
        p = found + delim->len;
    }
    return result;
}

static DexString* dex_str_substring(DexString* s, int start, int end) {
    if (start < 0) start = 0;
    if (end > (int)s->len) end = (int)s->len;
    if (start >= end) return dex_string_new("", 0);
    return dex_string_new(s->data + start, (size_t)(end - start));
}

static DexString* dex_str_replace(DexString* s, DexString* old, DexString* new_str) {
    if (old->len == 0) return dex_string_new(s->data, s->len);
    // Count occurrences for precise allocation
    int count = 0;
    const char* p = s->data;
    while ((p = strstr(p, old->data)) != NULL) {
        count++;
        p += old->len;
    }
    if (count == 0) return dex_string_new(s->data, s->len);
    size_t new_len = s->len + (size_t)count * (new_str->len - old->len);
    DexString* result = (DexString*)dex_obj_alloc(sizeof(DexString) + new_len + 1, dex_string_destroy);
    result->len = new_len;
    char* dst = result->data;
    p = s->data;
    while (1) {
        const char* found = strstr(p, old->data);
        if (!found) {
            size_t remaining = (size_t)(s->data + s->len - p);
            memcpy(dst, p, remaining);
            dst += remaining;
            break;
        }
        size_t chunk = (size_t)(found - p);
        memcpy(dst, p, chunk);
        dst += chunk;
        memcpy(dst, new_str->data, new_str->len);
        dst += new_str->len;
        p = found + old->len;
    }
    result->data[new_len] = '\0';
    return result;
}

static unsigned char dex_str_charAt(DexString* s, int index) {
    if (index < 0 || index >= (int)s->len) {
        dex_panic("charAt index out of bounds");
    }
    return (unsigned char)s->data[index];
}
