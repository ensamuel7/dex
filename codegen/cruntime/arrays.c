
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <limits.h>

typedef struct {
    int* data;
    int len;
    int cap;
} DexArrayInt;

DexArrayInt dex_array_int_new(void) {
    DexArrayInt a;
    a.cap = 8;
    a.len = 0;
    a.data = (int*)malloc(sizeof(int) * a.cap);
    if (!a.data) { dex_panic("out of memory"); }
    return a;
}

void dex_array_int_push(DexArrayInt* a, int val) {
    if (a->len == a->cap) {
        if (a->cap > INT_MAX / 2) { dex_panic("array capacity overflow"); }
        a->cap *= 2;
        a->data = (int*)realloc(a->data, sizeof(int) * a->cap);
        if (!a->data) { dex_panic("out of memory"); }
    }
    a->data[a->len++] = val;
}

int dex_array_int_pop(DexArrayInt* a) {
    if (a->len == 0) { dex_panic("pop from empty array"); }
    return a->data[--a->len];
}

void dex_array_int_remove(DexArrayInt* a, int index) {
    if (index < 0 || index >= a->len) { dex_panic("remove index out of bounds"); }
    for (int i = index; i < a->len - 1; i++) {
        a->data[i] = a->data[i + 1];
    }
    a->len--;
}

_Bool dex_array_int_contains(DexArrayInt* a, int val) {
    for (int i = 0; i < a->len; i++) {
        if (a->data[i] == val) return 1;
    }
    return 0;
}

int dex_array_int_indexOf(DexArrayInt* a, int val) {
    for (int i = 0; i < a->len; i++) {
        if (a->data[i] == val) return i;
    }
    return -1;
}

void dex_array_int_reverse(DexArrayInt* a) {
    for (int i = 0, j = a->len - 1; i < j; i++, j--) {
        int tmp = a->data[i];
        a->data[i] = a->data[j];
        a->data[j] = tmp;
    }
}

static int dex_cmp_int_asc(const void* a, const void* b) {
    int x = *(const int*)a, y = *(const int*)b;
    return (x > y) - (x < y);
}

static int dex_cmp_int_desc(const void* a, const void* b) {
    int x = *(const int*)a, y = *(const int*)b;
    return (y > x) - (y < x);
}

void dex_array_int_sort_asc(DexArrayInt* a) {
    qsort(a->data, a->len, sizeof(int), dex_cmp_int_asc);
}

void dex_array_int_sort_desc(DexArrayInt* a) {
    qsort(a->data, a->len, sizeof(int), dex_cmp_int_desc);
}

typedef struct {
    _Bool* data;
    int len;
    int cap;
} DexArrayBool;

DexArrayBool dex_array_bool_new(void) {
    DexArrayBool a;
    a.cap = 8;
    a.len = 0;
    a.data = (_Bool*)malloc(sizeof(_Bool) * a.cap);
    if (!a.data) { dex_panic("out of memory"); }
    return a;
}

void dex_array_bool_push(DexArrayBool* a, _Bool val) {
    if (a->len == a->cap) {
        if (a->cap > INT_MAX / 2) { dex_panic("array capacity overflow"); }
        a->cap *= 2;
        a->data = (_Bool*)realloc(a->data, sizeof(_Bool) * a->cap);
        if (!a->data) { dex_panic("out of memory"); }
    }
    a->data[a->len++] = val;
}

_Bool dex_array_bool_pop(DexArrayBool* a) {
    if (a->len == 0) { dex_panic("pop from empty array"); }
    return a->data[--a->len];
}

void dex_array_bool_remove(DexArrayBool* a, int index) {
    if (index < 0 || index >= a->len) { dex_panic("remove index out of bounds"); }
    for (int i = index; i < a->len - 1; i++) {
        a->data[i] = a->data[i + 1];
    }
    a->len--;
}

_Bool dex_array_bool_contains(DexArrayBool* a, _Bool val) {
    for (int i = 0; i < a->len; i++) {
        if (a->data[i] == val) return 1;
    }
    return 0;
}

int dex_array_bool_indexOf(DexArrayBool* a, _Bool val) {
    for (int i = 0; i < a->len; i++) {
        if (a->data[i] == val) return i;
    }
    return -1;
}

void dex_array_bool_reverse(DexArrayBool* a) {
    for (int i = 0, j = a->len - 1; i < j; i++, j--) {
        _Bool tmp = a->data[i];
        a->data[i] = a->data[j];
        a->data[j] = tmp;
    }
}

typedef struct {
    const char** data;
    int len;
    int cap;
} DexArrayString;

DexArrayString dex_array_string_new(void) {
    DexArrayString a;
    a.cap = 8;
    a.len = 0;
    a.data = (const char**)malloc(sizeof(const char*) * a.cap);
    if (!a.data) { dex_panic("out of memory"); }
    return a;
}

void dex_array_string_push(DexArrayString* a, const char* val) {
    if (a->len == a->cap) {
        if (a->cap > INT_MAX / 2) { dex_panic("array capacity overflow"); }
        a->cap *= 2;
        a->data = (const char**)realloc(a->data, sizeof(const char*) * a->cap);
        if (!a->data) { dex_panic("out of memory"); }
    }
    a->data[a->len++] = val;
}

const char* dex_array_string_pop(DexArrayString* a) {
    if (a->len == 0) { dex_panic("pop from empty array"); }
    return a->data[--a->len];
}

void dex_array_string_remove(DexArrayString* a, int index) {
    if (index < 0 || index >= a->len) { dex_panic("remove index out of bounds"); }
    for (int i = index; i < a->len - 1; i++) {
        a->data[i] = a->data[i + 1];
    }
    a->len--;
}

_Bool dex_array_string_contains(DexArrayString* a, const char* val) {
    for (int i = 0; i < a->len; i++) {
        if (strcmp(a->data[i], val) == 0) return 1;
    }
    return 0;
}

int dex_array_string_indexOf(DexArrayString* a, const char* val) {
    for (int i = 0; i < a->len; i++) {
        if (strcmp(a->data[i], val) == 0) return i;
    }
    return -1;
}

void dex_array_string_reverse(DexArrayString* a) {
    for (int i = 0, j = a->len - 1; i < j; i++, j--) {
        const char* tmp = a->data[i];
        a->data[i] = a->data[j];
        a->data[j] = tmp;
    }
}

static int dex_cmp_string_asc(const void* a, const void* b) {
    return strcmp(*(const char**)a, *(const char**)b);
}

static int dex_cmp_string_desc(const void* a, const void* b) {
    return strcmp(*(const char**)b, *(const char**)a);
}

void dex_array_string_sort_asc(DexArrayString* a) {
    qsort(a->data, a->len, sizeof(const char*), dex_cmp_string_asc);
}

void dex_array_string_sort_desc(DexArrayString* a) {
    qsort(a->data, a->len, sizeof(const char*), dex_cmp_string_desc);
}

typedef struct {
    long* data;
    int len;
    int cap;
} DexArrayLong;

DexArrayLong dex_array_long_new(void) {
    DexArrayLong a;
    a.cap = 8;
    a.len = 0;
    a.data = (long*)malloc(sizeof(long) * a.cap);
    if (!a.data) { dex_panic("out of memory"); }
    return a;
}

void dex_array_long_push(DexArrayLong* a, long val) {
    if (a->len == a->cap) {
        if (a->cap > INT_MAX / 2) { dex_panic("array capacity overflow"); }
        a->cap *= 2;
        a->data = (long*)realloc(a->data, sizeof(long) * a->cap);
        if (!a->data) { dex_panic("out of memory"); }
    }
    a->data[a->len++] = val;
}

long dex_array_long_pop(DexArrayLong* a) {
    if (a->len == 0) { dex_panic("pop from empty array"); }
    return a->data[--a->len];
}

void dex_array_long_remove(DexArrayLong* a, int index) {
    if (index < 0 || index >= a->len) { dex_panic("remove index out of bounds"); }
    for (int i = index; i < a->len - 1; i++) {
        a->data[i] = a->data[i + 1];
    }
    a->len--;
}

_Bool dex_array_long_contains(DexArrayLong* a, long val) {
    for (int i = 0; i < a->len; i++) {
        if (a->data[i] == val) return 1;
    }
    return 0;
}

int dex_array_long_indexOf(DexArrayLong* a, long val) {
    for (int i = 0; i < a->len; i++) {
        if (a->data[i] == val) return i;
    }
    return -1;
}

void dex_array_long_reverse(DexArrayLong* a) {
    for (int i = 0, j = a->len - 1; i < j; i++, j--) {
        long tmp = a->data[i];
        a->data[i] = a->data[j];
        a->data[j] = tmp;
    }
}

static int dex_cmp_long_asc(const void* a, const void* b) {
    long x = *(const long*)a, y = *(const long*)b;
    return (x > y) - (x < y);
}

static int dex_cmp_long_desc(const void* a, const void* b) {
    long x = *(const long*)a, y = *(const long*)b;
    return (y > x) - (y < x);
}

void dex_array_long_sort_asc(DexArrayLong* a) {
    qsort(a->data, a->len, sizeof(long), dex_cmp_long_asc);
}

void dex_array_long_sort_desc(DexArrayLong* a) {
    qsort(a->data, a->len, sizeof(long), dex_cmp_long_desc);
}

typedef struct {
    double* data;
    int len;
    int cap;
} DexArrayDouble;

DexArrayDouble dex_array_double_new(void) {
    DexArrayDouble a;
    a.cap = 8;
    a.len = 0;
    a.data = (double*)malloc(sizeof(double) * a.cap);
    if (!a.data) { dex_panic("out of memory"); }
    return a;
}

void dex_array_double_push(DexArrayDouble* a, double val) {
    if (a->len == a->cap) {
        if (a->cap > INT_MAX / 2) { dex_panic("array capacity overflow"); }
        a->cap *= 2;
        a->data = (double*)realloc(a->data, sizeof(double) * a->cap);
        if (!a->data) { dex_panic("out of memory"); }
    }
    a->data[a->len++] = val;
}

double dex_array_double_pop(DexArrayDouble* a) {
    if (a->len == 0) { dex_panic("pop from empty array"); }
    return a->data[--a->len];
}

void dex_array_double_remove(DexArrayDouble* a, int index) {
    if (index < 0 || index >= a->len) { dex_panic("remove index out of bounds"); }
    for (int i = index; i < a->len - 1; i++) {
        a->data[i] = a->data[i + 1];
    }
    a->len--;
}

_Bool dex_array_double_contains(DexArrayDouble* a, double val) {
    for (int i = 0; i < a->len; i++) {
        if (a->data[i] == val) return 1;
    }
    return 0;
}

int dex_array_double_indexOf(DexArrayDouble* a, double val) {
    for (int i = 0; i < a->len; i++) {
        if (a->data[i] == val) return i;
    }
    return -1;
}

void dex_array_double_reverse(DexArrayDouble* a) {
    for (int i = 0, j = a->len - 1; i < j; i++, j--) {
        double tmp = a->data[i];
        a->data[i] = a->data[j];
        a->data[j] = tmp;
    }
}

static int dex_cmp_double_asc(const void* a, const void* b) {
    double x = *(const double*)a, y = *(const double*)b;
    return (x > y) - (x < y);
}

static int dex_cmp_double_desc(const void* a, const void* b) {
    double x = *(const double*)a, y = *(const double*)b;
    return (y > x) - (y < x);
}

void dex_array_double_sort_asc(DexArrayDouble* a) {
    qsort(a->data, a->len, sizeof(double), dex_cmp_double_asc);
}

void dex_array_double_sort_desc(DexArrayDouble* a) {
    qsort(a->data, a->len, sizeof(double), dex_cmp_double_desc);
}

typedef struct {
    unsigned char* data;
    int len;
    int cap;
} DexArrayChar;

DexArrayChar dex_array_char_new(void) {
    DexArrayChar a;
    a.cap = 8;
    a.len = 0;
    a.data = (unsigned char*)malloc(sizeof(unsigned char) * a.cap);
    if (!a.data) { dex_panic("out of memory"); }
    return a;
}

void dex_array_char_push(DexArrayChar* a, unsigned char val) {
    if (a->len == a->cap) {
        if (a->cap > INT_MAX / 2) { dex_panic("array capacity overflow"); }
        a->cap *= 2;
        a->data = (unsigned char*)realloc(a->data, sizeof(unsigned char) * a->cap);
        if (!a->data) { dex_panic("out of memory"); }
    }
    a->data[a->len++] = val;
}

unsigned char dex_array_char_pop(DexArrayChar* a) {
    if (a->len == 0) { dex_panic("pop from empty array"); }
    return a->data[--a->len];
}

void dex_array_char_remove(DexArrayChar* a, int index) {
    if (index < 0 || index >= a->len) { dex_panic("remove index out of bounds"); }
    for (int i = index; i < a->len - 1; i++) {
        a->data[i] = a->data[i + 1];
    }
    a->len--;
}

_Bool dex_array_char_contains(DexArrayChar* a, unsigned char val) {
    for (int i = 0; i < a->len; i++) {
        if (a->data[i] == val) return 1;
    }
    return 0;
}

int dex_array_char_indexOf(DexArrayChar* a, unsigned char val) {
    for (int i = 0; i < a->len; i++) {
        if (a->data[i] == val) return i;
    }
    return -1;
}

void dex_array_char_reverse(DexArrayChar* a) {
    for (int i = 0, j = a->len - 1; i < j; i++, j--) {
        unsigned char tmp = a->data[i];
        a->data[i] = a->data[j];
        a->data[j] = tmp;
    }
}

static int dex_cmp_char_asc(const void* a, const void* b) {
    unsigned char x = *(const unsigned char*)a, y = *(const unsigned char*)b;
    return (x > y) - (x < y);
}

static int dex_cmp_char_desc(const void* a, const void* b) {
    unsigned char x = *(const unsigned char*)a, y = *(const unsigned char*)b;
    return (y > x) - (y < x);
}

void dex_array_char_sort_asc(DexArrayChar* a) {
    qsort(a->data, a->len, sizeof(unsigned char), dex_cmp_char_asc);
}

void dex_array_char_sort_desc(DexArrayChar* a) {
    qsort(a->data, a->len, sizeof(unsigned char), dex_cmp_char_desc);
}

const char* dex_json_stringify_int(DexArrayInt* a) {
    size_t cap = 64;
    char* buf = (char*)malloc(cap);
    int pos = 0;
    buf[pos++] = '[';
    for (int i = 0; i < a->len; i++) {
        if (i > 0) { buf[pos++] = ','; buf[pos++] = ' '; }
        int n = snprintf(buf + pos, cap - pos, "%d", a->data[i]);
        pos += n;
        if ((size_t)pos + 32 > cap) {
            cap *= 2;
            buf = (char*)realloc(buf, cap);
        }
    }
    buf[pos++] = ']';
    buf[pos] = '\0';
    return buf;
}

const char* dex_json_stringify_bool(DexArrayBool* a) {
    size_t cap = 64;
    char* buf = (char*)malloc(cap);
    int pos = 0;
    buf[pos++] = '[';
    for (int i = 0; i < a->len; i++) {
        if (i > 0) { buf[pos++] = ','; buf[pos++] = ' '; }
        const char* v = a->data[i] ? "true" : "false";
        int n = snprintf(buf + pos, cap - pos, "%s", v);
        pos += n;
        if ((size_t)pos + 32 > cap) {
            cap *= 2;
            buf = (char*)realloc(buf, cap);
        }
    }
    buf[pos++] = ']';
    buf[pos] = '\0';
    return buf;
}

const char* dex_json_stringify_str(DexArrayString* a) {
    size_t cap = 128;
    char* buf = (char*)malloc(cap);
    int pos = 0;
    buf[pos++] = '[';
    for (int i = 0; i < a->len; i++) {
        if (i > 0) { buf[pos++] = ','; buf[pos++] = ' '; }
        int n = snprintf(buf + pos, cap - pos, "\"%s\"", a->data[i]);
        pos += n;
        if ((size_t)pos + 64 > cap) {
            cap *= 2;
            buf = (char*)realloc(buf, cap);
        }
    }
    buf[pos++] = ']';
    buf[pos] = '\0';
    return buf;
}

const char* dex_json_stringify_long(DexArrayLong* a) {
    size_t cap = 64;
    char* buf = (char*)malloc(cap);
    int pos = 0;
    buf[pos++] = '[';
    for (int i = 0; i < a->len; i++) {
        if (i > 0) { buf[pos++] = ','; buf[pos++] = ' '; }
        int n = snprintf(buf + pos, cap - pos, "%ld", a->data[i]);
        pos += n;
        if ((size_t)pos + 32 > cap) {
            cap *= 2;
            buf = (char*)realloc(buf, cap);
        }
    }
    buf[pos++] = ']';
    buf[pos] = '\0';
    return buf;
}

const char* dex_json_stringify_double(DexArrayDouble* a) {
    size_t cap = 128;
    char* buf = (char*)malloc(cap);
    int pos = 0;
    buf[pos++] = '[';
    for (int i = 0; i < a->len; i++) {
        if (i > 0) { buf[pos++] = ','; buf[pos++] = ' '; }
        int n = snprintf(buf + pos, cap - pos, "%g", a->data[i]);
        pos += n;
        if ((size_t)pos + 64 > cap) {
            cap *= 2;
            buf = (char*)realloc(buf, cap);
        }
    }
    buf[pos++] = ']';
    buf[pos] = '\0';
    return buf;
}

const char* dex_json_set_arr_int(const char* obj, const char* key, DexArrayInt* arr) {
    const char* arrStr = dex_json_stringify_int(arr);
    size_t olen = strlen(obj);
    size_t klen = strlen(key);
    size_t alen = strlen(arrStr);
    size_t need = olen + klen + alen + 8;
    char* result = (char*)malloc(need);
    if (olen == 2) {
        snprintf(result, need, "{\"%s\": %s}", key, arrStr);
    } else {
        memcpy(result, obj, olen - 1);
        snprintf(result + olen - 1, need - (olen - 1), ", \"%s\": %s}", key, arrStr);
    }
    free((void*)arrStr);
    return result;
}

const char* dex_json_set_arr_bool(const char* obj, const char* key, DexArrayBool* arr) {
    const char* arrStr = dex_json_stringify_bool(arr);
    size_t olen = strlen(obj);
    size_t klen = strlen(key);
    size_t alen = strlen(arrStr);
    size_t need = olen + klen + alen + 8;
    char* result = (char*)malloc(need);
    if (olen == 2) {
        snprintf(result, need, "{\"%s\": %s}", key, arrStr);
    } else {
        memcpy(result, obj, olen - 1);
        snprintf(result + olen - 1, need - (olen - 1), ", \"%s\": %s}", key, arrStr);
    }
    free((void*)arrStr);
    return result;
}

const char* dex_json_set_arr_str(const char* obj, const char* key, DexArrayString* arr) {
    const char* arrStr = dex_json_stringify_str(arr);
    size_t olen = strlen(obj);
    size_t klen = strlen(key);
    size_t alen = strlen(arrStr);
    size_t need = olen + klen + alen + 8;
    char* result = (char*)malloc(need);
    if (olen == 2) {
        snprintf(result, need, "{\"%s\": %s}", key, arrStr);
    } else {
        memcpy(result, obj, olen - 1);
        snprintf(result + olen - 1, need - (olen - 1), ", \"%s\": %s}", key, arrStr);
    }
    free((void*)arrStr);
    return result;
}

const char* dex_json_set_arr_long(const char* obj, const char* key, DexArrayLong* arr) {
    const char* arrStr = dex_json_stringify_long(arr);
    size_t olen = strlen(obj);
    size_t klen = strlen(key);
    size_t alen = strlen(arrStr);
    size_t need = olen + klen + alen + 8;
    char* result = (char*)malloc(need);
    if (olen == 2) {
        snprintf(result, need, "{\"%s\": %s}", key, arrStr);
    } else {
        memcpy(result, obj, olen - 1);
        snprintf(result + olen - 1, need - (olen - 1), ", \"%s\": %s}", key, arrStr);
    }
    free((void*)arrStr);
    return result;
}

const char* dex_json_set_arr_double(const char* obj, const char* key, DexArrayDouble* arr) {
    const char* arrStr = dex_json_stringify_double(arr);
    size_t olen = strlen(obj);
    size_t klen = strlen(key);
    size_t alen = strlen(arrStr);
    size_t need = olen + klen + alen + 8;
    char* result = (char*)malloc(need);
    if (olen == 2) {
        snprintf(result, need, "{\"%s\": %s}", key, arrStr);
    } else {
        memcpy(result, obj, olen - 1);
        snprintf(result + olen - 1, need - (olen - 1), ", \"%s\": %s}", key, arrStr);
    }
    free((void*)arrStr);
    return result;
}

const char* dex_json_stringify_char(DexArrayChar* a) {
    size_t cap = 64;
    char* buf = (char*)malloc(cap);
    int pos = 0;
    buf[pos++] = '[';
    for (int i = 0; i < a->len; i++) {
        if (i > 0) { buf[pos++] = ','; buf[pos++] = ' '; }
        int n = snprintf(buf + pos, cap - pos, "%d", (int)a->data[i]);
        pos += n;
        if ((size_t)pos + 32 > cap) {
            cap *= 2;
            buf = (char*)realloc(buf, cap);
        }
    }
    buf[pos++] = ']';
    buf[pos] = '\0';
    return buf;
}

const char* dex_json_set_arr_char(const char* obj, const char* key, DexArrayChar* arr) {
    const char* arrStr = dex_json_stringify_char(arr);
    size_t olen = strlen(obj);
    size_t klen = strlen(key);
    size_t alen = strlen(arrStr);
    size_t need = olen + klen + alen + 8;
    char* result = (char*)malloc(need);
    if (olen == 2) {
        snprintf(result, need, "{\"%s\": %s}", key, arrStr);
    } else {
        memcpy(result, obj, olen - 1);
        snprintf(result + olen - 1, need - (olen - 1), ", \"%s\": %s}", key, arrStr);
    }
    free((void*)arrStr);
    return result;
}
