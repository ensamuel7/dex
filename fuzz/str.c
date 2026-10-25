#include "runtime.h"
#include "../stdlib/cruntime/str.c"

int LLVMFuzzerTestOneInput(const uint8_t* data, size_t size) {
    char* text = dex_fuzz_cstr(data, size);

    int i = dex_str_toInt(text);
    long l = dex_str_toLong(text);
    double d = dex_str_toDouble(text);

    DexString* si = dex_str_fromInt(i);
    if (dex_str_toInt(si->data) != i) abort();
    DexString* sl = dex_str_fromLong(l);
    if (dex_str_toLong(sl->data) != l) abort();
    DexString* sd = dex_str_fromDouble(d);
    (void)dex_str_toDouble(sd->data);
    DexString* sb = dex_str_fromBool(size & 1);

    dex_release(sb);
    dex_release(sd);
    dex_release(sl);
    dex_release(si);
    free(text);
    return 0;
}
