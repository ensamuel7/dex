// DexLang IO runtime — stdin input functions

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
    // Strip trailing newline
    if (nread > 0 && line[nread - 1] == '\n') {
        line[nread - 1] = '\0';
        nread--;
    }
    // Strip trailing carriage return (Windows line endings)
    if (nread > 0 && line[nread - 1] == '\r') {
        line[nread - 1] = '\0';
        nread--;
    }
    return line;
}

static int dex_io_read_int(void) {
    int value = 0;
    if (scanf("%d", &value) != 1) {
        value = 0;
    }
    // Consume trailing newline
    int ch;
    while ((ch = getchar()) != '\n' && ch != EOF) {}
    return value;
}

static double dex_io_read_double(void) {
    double value = 0.0;
    if (scanf("%lf", &value) != 1) {
        value = 0.0;
    }
    // Consume trailing newline
    int ch;
    while ((ch = getchar()) != '\n' && ch != EOF) {}
    return value;
}

static int dex_io_read_bool(void) {
    char buf[16];
    if (scanf("%15s", buf) != 1) {
        return 0;
    }
    // Consume trailing newline
    int ch;
    while ((ch = getchar()) != '\n' && ch != EOF) {}
    // Case-insensitive comparison
    for (int i = 0; buf[i]; i++) {
        buf[i] = tolower((unsigned char)buf[i]);
    }
    return strcmp(buf, "true") == 0 ? 1 : 0;
}
