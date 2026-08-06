
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <ctype.h>

static void dex_io_flush_line(void) {
    int ch;
    while ((ch = getchar()) != '\n' && ch != EOF) {}
}

static void dex_io_prompt(const char* msg) {
    printf("%s", msg);
    fflush(stdout);
}

static char* dex_io_read_line(void) {
    char* line = NULL;
    size_t len = 0;
    ssize_t nread = getline(&line, &len, stdin);
    if (nread == -1) {
        free(line);
        return strdup("");
    }
    if (nread > 0 && line[nread - 1] == '\n') line[--nread] = '\0';
    if (nread > 0 && line[nread - 1] == '\r') line[--nread] = '\0';
    return line;
}

static int dex_io_read_int(void) {
    int value = 0;
    scanf("%d", &value);
    dex_io_flush_line();
    return value;
}

static double dex_io_read_double(void) {
    double value = 0.0;
    scanf("%lf", &value);
    dex_io_flush_line();
    return value;
}

static int dex_io_read_bool(void) {
    char buf[16];
    if (scanf("%15s", buf) != 1) {
        dex_io_flush_line();
        return 0;
    }
    dex_io_flush_line();
    for (int i = 0; buf[i]; i++) {
        buf[i] = tolower((unsigned char)buf[i]);
    }
    return strcmp(buf, "true") == 0;
}
