#include "runtime.h"
#include "../stdlib/cruntime/base64.h"
#include "../stdlib/cruntime/crypto_digest.c"

int LLVMFuzzerTestOneInput(const uint8_t* data, size_t size) {
    size_t outlen = 4 * ((size + 2) / 3);
    char* encoded = (char*)malloc(outlen + 1);
    if (!encoded) abort();
    dex_base64_encode(data, size, encoded);
    if (strlen(encoded) != outlen) abort();

    DexString* raw = dex_string_new((const char*)data, size);
    DexString* enc = dex_crypto_base64_encode(raw);
    if (enc->len != outlen || memcmp(enc->data, encoded, outlen) != 0) abort();

    DexString* dec = dex_crypto_base64_decode(enc);
    if (dec->len != size || memcmp(dec->data, data, size) != 0) abort();

    DexString* garbage = dex_crypto_base64_decode(raw);
    if (garbage->data[garbage->len] != '\0') abort();

    dex_release(garbage);
    dex_release(dec);
    dex_release(enc);
    dex_release(raw);
    free(encoded);
    return 0;
}
