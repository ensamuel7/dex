#include <stdlib.h>
#include <string.h>

static int freer_calls = 0;

static char* freer_make(const char* s) { return strdup(s); }
static char* freer_none(void) { return NULL; }
static void freer_release(void* p) { freer_calls++; free(p); }
static int freer_count(void) { return freer_calls; }

#define FREER_RELEASE(p) freer_release(p)

static int freer_open(const char* s, char** out) {
    if (!s[0]) return 1;
    if (out) *out = strdup(s);
    return 0;
}
