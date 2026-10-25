#ifndef DEX_FUZZ_RUNTIME_H
#define DEX_FUZZ_RUNTIME_H

#include <stdio.h>
#include <stdbool.h>
#include <string.h>
#include <stddef.h>
#include <stdlib.h>
#include <stdint.h>

#include "../codegen/cruntime/core.c"
#include "../codegen/cruntime/safety.c"
#define DEX_SINGLE_THREADED
#include "../codegen/cruntime/refcount.c"
#include "../codegen/cruntime/closure.c"
#include "../codegen/cruntime/strings.c"
#include "../codegen/cruntime/stringbuilder.c"
#define DEX_HAVE_ARRAYS 1
#include "../codegen/cruntime/arrays.c"
#include "../codegen/cruntime/string_methods.c"
#include "../codegen/cruntime/maps.c"
#include "../codegen/cruntime/threadpool.c"
#include "../codegen/cruntime/optionals.c"

typedef struct DexJsonValue DexJsonValue;
typedef struct Dex_Conn Dex_Conn;
typedef struct Dex_HttpOptions Dex_HttpOptions;
typedef struct Dex_HttpResponse Dex_HttpResponse;
typedef struct Dex_HttpRequest Dex_HttpRequest;
struct Dex_Conn {
    int fd;
    int isServer;
    long ssl;
};
struct Dex_HttpOptions {
    int timeoutMs;
    int connectTimeoutMs;
    int maxRedirects;
    _Bool insecureTls;
};
struct Dex_HttpResponse {
    int statusCode;
    DexString* body;
    DexString* contentType;
    DexString* headers;
};
struct Dex_HttpRequest {
    DexString* method;
    DexString* path;
    DexString* body;
    DexString* query;
    DexMap_str_str* params;
    DexMap_str_str* headers;
};

#include "../codegen/cruntime/event_loop.c"

static char* dex_fuzz_cstr(const uint8_t* data, size_t size) {
    char* s = (char*)malloc(size + 1);
    if (!s) abort();
    if (size) memcpy(s, data, size);
    s[size] = '\0';
    return s;
}

#endif
