// C functions handing back bytes with NULs in them, for externalBytes_test.dx.
#include <stdlib.h>
#include <string.h>

static const char BYTES[] = "ab\0cd";

static const char *bytes_plain(long *n) {
    *n = 5;
    return BYTES;
}

static const char *bytes_int_length(int *n) {
    *n = 5;
    return BYTES;
}

static char *bytes_owned(long *n) {
    char *p = malloc(5);
    memcpy(p, BYTES, 5);
    *n = 5;
    return p;
}

static int bytes_freed = 0;

static void bytes_free(void *p) {
    bytes_freed++;
    free(p);
}

static int bytes_freed_count(void) {
    return bytes_freed;
}

static const char *bytes_null(long *n) {
    *n = 5;
    return NULL;
}

static char *bytes_owned_null(long *n) {
    *n = 5;
    return NULL;
}

static const char *bytes_negative(long *n) {
    *n = -1;
    return BYTES;
}

static char *bytes_owned_negative(long *n) {
    *n = -1;
    return malloc(1);
}

static const char *bytes_empty(long *n) {
    *n = 0;
    return BYTES;
}

// Bytes into C as a pointer and a length, and back out the same way.
static char *bytes_echo(const char *s, long len, long *n) {
    char *p = malloc((size_t)len);
    memcpy(p, s, (size_t)len);
    *n = len;
    return p;
}

// A handle whose payload is read out by a sized call, as a message library's is.
typedef struct { char *data; long len; } BytesMsg;

static BytesMsg *bytes_msg_new(void) {
    BytesMsg *m = malloc(sizeof(BytesMsg));
    m->data = malloc(5);
    memcpy(m->data, BYTES, 5);
    m->len = 5;
    return m;
}

static void bytes_msg_free(BytesMsg *m) {
    free(m->data);
    free(m);
}

static const char *bytes_msg_value(BytesMsg *m, long *n) {
    *n = m->len;
    return m->data;
}
