
#include <stdlib.h>
#include <stdio.h>
#include <string.h>
#include <sys/wait.h>

static char* dex_os_env(const char* name) {
    const char* val = getenv(name);
    if (val == NULL) {
        return strdup("");
    }
    return strdup(val);
}

static void dex_os_exit(int code) {
    exit(code);
}

// Read all output from a FILE* pipe into a malloc'd string.
static char* dex_os_read_pipe(FILE* fp) {
    char* buf = NULL;
    size_t len = 0;
    size_t cap = 0;
    char chunk[4096];
    size_t n;
    while ((n = fread(chunk, 1, sizeof(chunk), fp)) > 0) {
        if (len + n > cap) {
            cap = (len + n) * 2;
            buf = realloc(buf, cap + 1);
        }
        memcpy(buf + len, chunk, n);
        len += n;
    }
    if (buf != NULL) {
        buf[len] = '\0';
    }
    return buf;
}

static Dex_ExecResult dex_os_exec(const char* command) {
    Dex_ExecResult result;
    result.exitCode = -1;
    result.output = dex_string_from_lit("");
    result.error = dex_string_from_lit("");

    FILE* fp = popen(command, "r");
    if (fp == NULL) {
        return result;
    }

    char* out = dex_os_read_pipe(fp);
    int status = pclose(fp);
    if (WIFEXITED(status)) {
        result.exitCode = WEXITSTATUS(status);
    }
    if (out != NULL) {
        dex_release(result.output);
        result.output = dex_string_from_cstr(out);
    }

    // Capture stderr by re-running with redirected streams
    size_t cmd_len = strlen(command);
    char* stderr_cmd = malloc(cmd_len + 32);
    snprintf(stderr_cmd, cmd_len + 32, "%s 2>&1 1>/dev/null", command);
    FILE* efp = popen(stderr_cmd, "r");
    free(stderr_cmd);
    if (efp != NULL) {
        char* err = dex_os_read_pipe(efp);
        pclose(efp);
        if (err != NULL) {
            dex_release(result.error);
            result.error = dex_string_from_cstr(err);
        }
    }

    return result;
}
