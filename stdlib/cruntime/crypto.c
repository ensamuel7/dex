
#include <stdio.h>
#include <stdlib.h>
#include <fcntl.h>
#include <unistd.h>
#include <errno.h>
#include <string.h>
#if defined(__APPLE__)
#include <sys/random.h>
#endif

// Returns "" rather than a predictable id when the system has no entropy to
// give: callers use a uuid as a secret often enough that a guessable one is
// worse than a failure they can see.
static int dex_random_bytes(unsigned char* out, size_t want) {
#if defined(__APPLE__) || defined(__GLIBC__) || defined(__OpenBSD__) || defined(__FreeBSD__)
    if (getentropy(out, want) == 0) return 1;
#endif
    int fd = open("/dev/urandom", O_RDONLY);
    if (fd < 0) return 0;
    size_t got = 0;
    while (got < want) {
        ssize_t n = read(fd, out + got, want - got);
        if (n > 0) {
            got += (size_t)n;
            continue;
        }
        if (n < 0 && errno == EINTR) continue;
        close(fd);
        return 0;
    }
    close(fd);
    return 1;
}

const char* dex_crypto_uuid(void) {
    unsigned char bytes[16];
    if (!dex_random_bytes(bytes, sizeof(bytes))) {
        return strdup("");
    }
    // Set version 4 (bits 6-7 of byte 6)
    bytes[6] = (bytes[6] & 0x0F) | 0x40;
    // Set variant 1 (bits 6-7 of byte 8)
    bytes[8] = (bytes[8] & 0x3F) | 0x80;

    char* result = (char*)malloc(37);
    if (!result) return strdup("");
    snprintf(result, 37, "%02x%02x%02x%02x-%02x%02x-%02x%02x-%02x%02x-%02x%02x%02x%02x%02x%02x",
        bytes[0], bytes[1], bytes[2], bytes[3],
        bytes[4], bytes[5],
        bytes[6], bytes[7],
        bytes[8], bytes[9],
        bytes[10], bytes[11], bytes[12], bytes[13], bytes[14], bytes[15]);
    return result;
}
