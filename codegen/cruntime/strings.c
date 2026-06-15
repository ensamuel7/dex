
#include <string.h>
#include <stdlib.h>

const char* dex_str_concat(const char* a, const char* b) {
    size_t la = strlen(a), lb = strlen(b);
    char* result = (char*)malloc(la + lb + 1);
    memcpy(result, a, la);
    memcpy(result + la, b, lb + 1);
    return result;
}

const char* dex_str_concat_len(const char* a, size_t a_len, const char* b, size_t* out_len) {
    size_t lb = strlen(b);
    *out_len = a_len + lb;
    char* result = (char*)malloc(*out_len + 1);
    memcpy(result, a, a_len);
    memcpy(result + a_len, b, lb + 1);
    return result;
}
