
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <limits.h>
#include <stddef.h>

// === DexArrayInt ===
typedef struct {
    DexObjHeader hdr;
    int* data;
    int len;
    int cap;
} DexArrayInt;

static void dex_array_int_destroy(void* ptr) {
    DexArrayInt* a = (DexArrayInt*)ptr;
    free(a->data);
}

DexArrayInt* dex_array_int_new(void) {
    DexArrayInt* a = (DexArrayInt*)dex_obj_alloc(sizeof(DexArrayInt), dex_array_int_destroy);
    a->cap = 8;
    a->len = 0;
    a->data = (int*)malloc(sizeof(int) * a->cap);
    if (!a->data) { dex_panic("out of memory"); }
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

DexArrayInt* dex_array_int_slice(DexArrayInt* a, int start, int end) {
    if (start < 0) start = 0;
    if (end > a->len) end = a->len;
    if (start > end) start = end;
    int count = end - start;
    DexArrayInt* result = dex_array_int_new();
    if (count > result->cap) {
        result->cap = count;
        result->data = (int*)realloc(result->data, sizeof(int) * result->cap);
        if (!result->data) { dex_panic("out of memory"); }
    }
    memcpy(result->data, a->data + start, sizeof(int) * count);
    result->len = count;
    return result;
}

// === DexArrayBool ===
typedef struct {
    DexObjHeader hdr;
    _Bool* data;
    int len;
    int cap;
} DexArrayBool;

static void dex_array_bool_destroy(void* ptr) {
    DexArrayBool* a = (DexArrayBool*)ptr;
    free(a->data);
}

DexArrayBool* dex_array_bool_new(void) {
    DexArrayBool* a = (DexArrayBool*)dex_obj_alloc(sizeof(DexArrayBool), dex_array_bool_destroy);
    a->cap = 8;
    a->len = 0;
    a->data = (_Bool*)malloc(sizeof(_Bool) * a->cap);
    if (!a->data) { dex_panic("out of memory"); }
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

DexArrayBool* dex_array_bool_slice(DexArrayBool* a, int start, int end) {
    if (start < 0) start = 0;
    if (end > a->len) end = a->len;
    if (start > end) start = end;
    int count = end - start;
    DexArrayBool* result = dex_array_bool_new();
    if (count > result->cap) {
        result->cap = count;
        result->data = (_Bool*)realloc(result->data, sizeof(_Bool) * result->cap);
        if (!result->data) { dex_panic("out of memory"); }
    }
    memcpy(result->data, a->data + start, sizeof(_Bool) * count);
    result->len = count;
    return result;
}

// === DexArrayString ===
typedef struct {
    DexObjHeader hdr;
    DexString** data;
    int len;
    int cap;
} DexArrayString;

static void dex_array_string_destroy(void* ptr) {
    DexArrayString* a = (DexArrayString*)ptr;
    for (int i = 0; i < a->len; i++) {
        dex_release(a->data[i]);
    }
    free(a->data);
}

DexArrayString* dex_array_string_new(void) {
    DexArrayString* a = (DexArrayString*)dex_obj_alloc(sizeof(DexArrayString), dex_array_string_destroy);
    a->cap = 8;
    a->len = 0;
    a->data = (DexString**)malloc(sizeof(DexString*) * a->cap);
    if (!a->data) { dex_panic("out of memory"); }
    return a;
}

void dex_array_string_push(DexArrayString* a, DexString* val) {
    if (a->len == a->cap) {
        if (a->cap > INT_MAX / 2) { dex_panic("array capacity overflow"); }
        a->cap *= 2;
        a->data = (DexString**)realloc(a->data, sizeof(DexString*) * a->cap);
        if (!a->data) { dex_panic("out of memory"); }
    }
    dex_retain(val);
    a->data[a->len++] = val;
}

DexString* dex_array_string_pop(DexArrayString* a) {
    if (a->len == 0) { dex_panic("pop from empty array"); }
    return a->data[--a->len]; // ownership transfers to caller
}

void dex_array_string_remove(DexArrayString* a, int index) {
    if (index < 0 || index >= a->len) { dex_panic("remove index out of bounds"); }
    dex_release(a->data[index]);
    for (int i = index; i < a->len - 1; i++) {
        a->data[i] = a->data[i + 1];
    }
    a->len--;
}

_Bool dex_array_string_contains(DexArrayString* a, DexString* val) {
    for (int i = 0; i < a->len; i++) {
        if (strcmp(a->data[i]->data, val->data) == 0) return 1;
    }
    return 0;
}

int dex_array_string_indexOf(DexArrayString* a, DexString* val) {
    for (int i = 0; i < a->len; i++) {
        if (strcmp(a->data[i]->data, val->data) == 0) return i;
    }
    return -1;
}

void dex_array_string_reverse(DexArrayString* a) {
    for (int i = 0, j = a->len - 1; i < j; i++, j--) {
        DexString* tmp = a->data[i];
        a->data[i] = a->data[j];
        a->data[j] = tmp;
    }
}

static int dex_cmp_string_asc(const void* a, const void* b) {
    DexString* x = *(DexString**)a;
    DexString* y = *(DexString**)b;
    return strcmp(x->data, y->data);
}

static int dex_cmp_string_desc(const void* a, const void* b) {
    DexString* x = *(DexString**)a;
    DexString* y = *(DexString**)b;
    return strcmp(y->data, x->data);
}

void dex_array_string_sort_asc(DexArrayString* a) {
    qsort(a->data, a->len, sizeof(DexString*), dex_cmp_string_asc);
}

void dex_array_string_sort_desc(DexArrayString* a) {
    qsort(a->data, a->len, sizeof(DexString*), dex_cmp_string_desc);
}

DexArrayString* dex_array_string_slice(DexArrayString* a, int start, int end) {
    if (start < 0) start = 0;
    if (end > a->len) end = a->len;
    if (start > end) start = end;
    int count = end - start;
    DexArrayString* result = dex_array_string_new();
    if (count > result->cap) {
        result->cap = count;
        result->data = (DexString**)realloc(result->data, sizeof(DexString*) * result->cap);
        if (!result->data) { dex_panic("out of memory"); }
    }
    for (int i = 0; i < count; i++) {
        result->data[i] = a->data[start + i];
        dex_retain(result->data[i]);
    }
    result->len = count;
    return result;
}

// === DexArrayLong ===
typedef struct {
    DexObjHeader hdr;
    long* data;
    int len;
    int cap;
} DexArrayLong;

static void dex_array_long_destroy(void* ptr) {
    DexArrayLong* a = (DexArrayLong*)ptr;
    free(a->data);
}

DexArrayLong* dex_array_long_new(void) {
    DexArrayLong* a = (DexArrayLong*)dex_obj_alloc(sizeof(DexArrayLong), dex_array_long_destroy);
    a->cap = 8;
    a->len = 0;
    a->data = (long*)malloc(sizeof(long) * a->cap);
    if (!a->data) { dex_panic("out of memory"); }
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

DexArrayLong* dex_array_long_slice(DexArrayLong* a, int start, int end) {
    if (start < 0) start = 0;
    if (end > a->len) end = a->len;
    if (start > end) start = end;
    int count = end - start;
    DexArrayLong* result = dex_array_long_new();
    if (count > result->cap) {
        result->cap = count;
        result->data = (long*)realloc(result->data, sizeof(long) * result->cap);
        if (!result->data) { dex_panic("out of memory"); }
    }
    memcpy(result->data, a->data + start, sizeof(long) * count);
    result->len = count;
    return result;
}

// === DexArrayDouble ===
typedef struct {
    DexObjHeader hdr;
    double* data;
    int len;
    int cap;
} DexArrayDouble;

static void dex_array_double_destroy(void* ptr) {
    DexArrayDouble* a = (DexArrayDouble*)ptr;
    free(a->data);
}

DexArrayDouble* dex_array_double_new(void) {
    DexArrayDouble* a = (DexArrayDouble*)dex_obj_alloc(sizeof(DexArrayDouble), dex_array_double_destroy);
    a->cap = 8;
    a->len = 0;
    a->data = (double*)malloc(sizeof(double) * a->cap);
    if (!a->data) { dex_panic("out of memory"); }
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

DexArrayDouble* dex_array_double_slice(DexArrayDouble* a, int start, int end) {
    if (start < 0) start = 0;
    if (end > a->len) end = a->len;
    if (start > end) start = end;
    int count = end - start;
    DexArrayDouble* result = dex_array_double_new();
    if (count > result->cap) {
        result->cap = count;
        result->data = (double*)realloc(result->data, sizeof(double) * result->cap);
        if (!result->data) { dex_panic("out of memory"); }
    }
    memcpy(result->data, a->data + start, sizeof(double) * count);
    result->len = count;
    return result;
}

// === DexArrayChar ===
typedef struct {
    DexObjHeader hdr;
    unsigned char* data;
    int len;
    int cap;
} DexArrayChar;

static void dex_array_char_destroy(void* ptr) {
    DexArrayChar* a = (DexArrayChar*)ptr;
    free(a->data);
}

DexArrayChar* dex_array_char_new(void) {
    DexArrayChar* a = (DexArrayChar*)dex_obj_alloc(sizeof(DexArrayChar), dex_array_char_destroy);
    a->cap = 8;
    a->len = 0;
    a->data = (unsigned char*)malloc(sizeof(unsigned char) * a->cap);
    if (!a->data) { dex_panic("out of memory"); }
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

DexArrayChar* dex_array_char_slice(DexArrayChar* a, int start, int end) {
    if (start < 0) start = 0;
    if (end > a->len) end = a->len;
    if (start > end) start = end;
    int count = end - start;
    DexArrayChar* result = dex_array_char_new();
    if (count > result->cap) {
        result->cap = count;
        result->data = (unsigned char*)realloc(result->data, sizeof(unsigned char) * result->cap);
        if (!result->data) { dex_panic("out of memory"); }
    }
    memcpy(result->data, a->data + start, sizeof(unsigned char) * count);
    result->len = count;
    return result;
}

// === DexArrayStruct (generic struct array) ===
typedef void (*DexStructElemCleanup)(void* elem);

typedef struct {
    DexObjHeader hdr;
    void* data;
    int len;
    int cap;
    size_t elem_size;
    DexStructElemCleanup cleanup;
} DexArrayStruct;

static void dex_array_struct_destroy(void* ptr) {
    DexArrayStruct* a = (DexArrayStruct*)ptr;
    if (a->cleanup) {
        for (int i = 0; i < a->len; i++) {
            a->cleanup((char*)a->data + (size_t)i * a->elem_size);
        }
    }
    free(a->data);
}

DexArrayStruct* dex_array_struct_new(size_t elem_size, DexStructElemCleanup cleanup) {
    DexArrayStruct* a = (DexArrayStruct*)dex_obj_alloc(sizeof(DexArrayStruct), dex_array_struct_destroy);
    a->cap = 8;
    a->len = 0;
    a->elem_size = elem_size;
    a->cleanup = cleanup;
    a->data = malloc(elem_size * a->cap);
    if (!a->data) { dex_panic("out of memory"); }
    return a;
}

void dex_array_struct_push(DexArrayStruct* a, const void* val) {
    if (a->len == a->cap) {
        if (a->cap > INT_MAX / 2) { dex_panic("array capacity overflow"); }
        a->cap *= 2;
        a->data = realloc(a->data, a->elem_size * a->cap);
        if (!a->data) { dex_panic("out of memory"); }
    }
    memcpy((char*)a->data + (size_t)a->len * a->elem_size, val, a->elem_size);
    a->len++;
}

void* dex_array_struct_get(DexArrayStruct* a, int index) {
    return (char*)a->data + (size_t)index * a->elem_size;
}

void dex_array_struct_pop(DexArrayStruct* a) {
    if (a->len == 0) { dex_panic("pop from empty array"); }
    a->len--;
    if (a->cleanup) {
        a->cleanup((char*)a->data + (size_t)a->len * a->elem_size);
    }
}

void dex_array_struct_remove(DexArrayStruct* a, int index) {
    if (index < 0 || index >= a->len) { dex_panic("remove index out of bounds"); }
    if (a->cleanup) {
        a->cleanup((char*)a->data + (size_t)index * a->elem_size);
    }
    memmove((char*)a->data + (size_t)index * a->elem_size,
            (char*)a->data + (size_t)(index + 1) * a->elem_size,
            a->elem_size * (a->len - index - 1));
    a->len--;
}

void dex_array_struct_reverse(DexArrayStruct* a) {
    if (a->len <= 1) return;
    void* tmp = malloc(a->elem_size);
    if (!tmp) { dex_panic("out of memory"); }
    for (int i = 0, j = a->len - 1; i < j; i++, j--) {
        void* pi = (char*)a->data + (size_t)i * a->elem_size;
        void* pj = (char*)a->data + (size_t)j * a->elem_size;
        memcpy(tmp, pi, a->elem_size);
        memcpy(pi, pj, a->elem_size);
        memcpy(pj, tmp, a->elem_size);
    }
    free(tmp);
}

DexArrayStruct* dex_array_struct_slice(DexArrayStruct* a, int start, int end) {
    if (start < 0) start = 0;
    if (end > a->len) end = a->len;
    if (start > end) start = end;
    int count = end - start;
    DexArrayStruct* result = dex_array_struct_new(a->elem_size, a->cleanup);
    if (count > result->cap) {
        result->cap = count;
        result->data = realloc(result->data, a->elem_size * result->cap);
        if (!result->data) { dex_panic("out of memory"); }
    }
    memcpy(result->data, (char*)a->data + (size_t)start * a->elem_size, a->elem_size * count);
    result->len = count;
    return result;
}

// === Struct array JSON serialization ===
// Field descriptor for generic struct-to-JSON serialization
// kind: 0=int, 1=bool, 2=string, 3=long, 4=double, 5=nested struct
#ifndef DEX_STRUCT_FIELD_DESC_DEFINED
#define DEX_STRUCT_FIELD_DESC_DEFINED
typedef struct DexStructFieldDesc {
    const char* name;
    size_t offset;
    int kind;
    int num_sub;                     // kind 5: field count of the nested struct
    struct DexStructFieldDesc* sub;  // kind 5: descriptors for the nested struct
    const char* (*enc_arr)(void* field);   // kind 6
    void (*dec_arr)(const char* json, void* field);
} DexStructFieldDesc;
#endif

// Encodes one struct instance as a JSON object from field descriptors, recursing
// into nested struct fields (kind 5). Mirrors dex_json_encode_struct in the json
// module runtime, which is emitted after this file and so cannot be called here.
static char* dex_arr_encode_struct(void* elem, int num_fields, DexStructFieldDesc* fields) {
    size_t cap = 256;
    char* buf = (char*)malloc(cap);
    if (!buf) return NULL;
    int pos = 0;
    buf[pos++] = '{';
    int wrote = 0;
    for (int f = 0; f < num_fields; f++) {
        if (fields[f].kind == 7) continue; // no codec — omit rather than emit garbage
        if (wrote) { buf[pos++] = ','; buf[pos++] = ' '; }
        wrote = 1;
        if ((size_t)pos + 256 > cap) {
            cap *= 2;
            char* t = (char*)realloc(buf, cap);
            if (!t) { buf[pos] = '\0'; return buf; }
            buf = t;
        }
        int n = 0;
        switch (fields[f].kind) {
        case 0: // int
            n = snprintf(buf + pos, cap - pos, "\"%s\": %d", fields[f].name, *(int*)((char*)elem + fields[f].offset));
            break;
        case 1: // bool
            n = snprintf(buf + pos, cap - pos, "\"%s\": %s", fields[f].name, (*(_Bool*)((char*)elem + fields[f].offset)) ? "true" : "false");
            break;
        case 2: // string
            { DexString* s = *(DexString**)((char*)elem + fields[f].offset);
              n = snprintf(buf + pos, cap - pos, "\"%s\": \"%s\"", fields[f].name, s ? s->data : ""); }
            break;
        case 3: // long
            n = snprintf(buf + pos, cap - pos, "\"%s\": %ld", fields[f].name, *(long*)((char*)elem + fields[f].offset));
            break;
        case 4: // double
            n = snprintf(buf + pos, cap - pos, "\"%s\": %g", fields[f].name, *(double*)((char*)elem + fields[f].offset));
            break;
        case 6: // array — codegen-supplied codec
            { const char* asub = fields[f].enc_arr ? fields[f].enc_arr((char*)elem + fields[f].offset) : NULL;
              const char* asubs = asub ? asub : "[]";
              size_t need = strlen(fields[f].name) + strlen(asubs) + 8;
              while ((size_t)pos + need > cap) {
                  cap *= 2;
                  char* t = (char*)realloc(buf, cap);
                  if (!t) { buf[pos] = '\0'; free((void*)asub); return buf; }
                  buf = t;
              }
              n = snprintf(buf + pos, cap - pos, "\"%s\": %s", fields[f].name, asubs);
              free((void*)asub); }
            break;
        case 5: // nested struct
            { char* sub = dex_arr_encode_struct((char*)elem + fields[f].offset, fields[f].num_sub, fields[f].sub);
              const char* subs = sub ? sub : "{}";
              size_t need = strlen(fields[f].name) + strlen(subs) + 8;
              while ((size_t)pos + need > cap) {
                  cap *= 2;
                  char* t = (char*)realloc(buf, cap);
                  if (!t) { buf[pos] = '\0'; free(sub); return buf; }
                  buf = t;
              }
              n = snprintf(buf + pos, cap - pos, "\"%s\": %s", fields[f].name, subs);
              free(sub); }
            break;
        }
        pos += n;
        if ((size_t)pos + 256 > cap) {
            cap *= 2;
            char* t = (char*)realloc(buf, cap);
            if (!t) { buf[pos] = '\0'; return buf; }
            buf = t;
        }
    }
    buf[pos++] = '}';
    buf[pos] = '\0';
    return buf;
}

const char* dex_json_set_struct_arr(const char* obj, const char* key, DexArrayStruct* arr, size_t elem_size, int num_fields, DexStructFieldDesc* fields) {
    // Build JSON array of objects
    size_t cap = 1024;
    char* buf = (char*)malloc(cap);
    int pos = 0;
    buf[pos++] = '[';
    for (int i = 0; i < arr->len; i++) {
        if (i > 0) { buf[pos++] = ','; buf[pos++] = ' '; }
        void* elem = (char*)arr->data + (size_t)i * elem_size;
        char* obj_str = dex_arr_encode_struct(elem, num_fields, fields);
        const char* objs = obj_str ? obj_str : "{}";
        size_t objlen = strlen(objs);
        while ((size_t)pos + objlen + 8 > cap) {
            cap *= 2;
            char* t = (char*)realloc(buf, cap);
            if (!t) { free(obj_str); buf[pos] = '\0'; return buf; }
            buf = t;
        }
        memcpy(buf + pos, objs, objlen);
        pos += (int)objlen;
        free(obj_str);
    }
    buf[pos++] = ']';
    buf[pos] = '\0';

    // Now set the array string into the object
    size_t olen = strlen(obj);
    size_t klen = strlen(key);
    size_t alen = (size_t)pos;
    size_t need = olen + klen + alen + 16;
    char* result = (char*)malloc(need);
    if (olen == 2) {
        snprintf(result, need, "{\"%s\": %s}", key, buf);
    } else {
        memcpy(result, obj, olen - 1);
        snprintf(result + olen - 1, need - (olen - 1), ", \"%s\": %s}", key, buf);
    }
    free(buf);
    return result;
}

const char* dex_json_stringify_struct_arr(DexArrayStruct* arr, size_t elem_size, int num_fields, DexStructFieldDesc* fields) {
    // Build JSON array of objects — returns just [...] without wrapping in an object
    size_t cap = 1024;
    char* buf = (char*)malloc(cap);
    int pos = 0;
    buf[pos++] = '[';
    for (int i = 0; i < arr->len; i++) {
        if (i > 0) { buf[pos++] = ','; buf[pos++] = ' '; }
        void* elem = (char*)arr->data + (size_t)i * elem_size;
        char* obj_str = dex_arr_encode_struct(elem, num_fields, fields);
        const char* objs = obj_str ? obj_str : "{}";
        size_t objlen = strlen(objs);
        while ((size_t)pos + objlen + 8 > cap) {
            cap *= 2;
            char* t = (char*)realloc(buf, cap);
            if (!t) { free(obj_str); buf[pos] = '\0'; return buf; }
            buf = t;
        }
        memcpy(buf + pos, objs, objlen);
        pos += (int)objlen;
        free(obj_str);
    }
    buf[pos++] = ']';
    buf[pos] = '\0';
    return buf;
}

// === JSON stringify helpers (still return const char* for stdlib bridging) ===

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
        int n = snprintf(buf + pos, cap - pos, "\"%s\"", a->data[i]->data);
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
