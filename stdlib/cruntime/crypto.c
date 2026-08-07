
#include <stdio.h>
#include <stdlib.h>
#include <fcntl.h>
#include <unistd.h>

const char* dex_crypto_uuid(void) {
    unsigned char bytes[16];
    int fd = open("/dev/urandom", O_RDONLY);
    if (fd < 0) {
        // fallback to rand()
        for (int i = 0; i < 16; i++) bytes[i] = (unsigned char)(rand() & 0xFF);
    } else {
        read(fd, bytes, 16);
        close(fd);
    }
    // Set version 4 (bits 6-7 of byte 6)
    bytes[6] = (bytes[6] & 0x0F) | 0x40;
    // Set variant 1 (bits 6-7 of byte 8)
    bytes[8] = (bytes[8] & 0x3F) | 0x80;

    char* result = (char*)malloc(37);
    snprintf(result, 37, "%02x%02x%02x%02x-%02x%02x-%02x%02x-%02x%02x-%02x%02x%02x%02x%02x%02x",
        bytes[0], bytes[1], bytes[2], bytes[3],
        bytes[4], bytes[5],
        bytes[6], bytes[7],
        bytes[8], bytes[9],
        bytes[10], bytes[11], bytes[12], bytes[13], bytes[14], bytes[15]);
    return result;
}
