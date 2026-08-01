// DexLang OS runtime — environment, process control, command execution

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

static Dex_ExecResult dex_os_exec(const char* command) {
    Dex_ExecResult result;
    result.exitCode = -1;
    result.output = dex_string_from_lit("");
    result.error = dex_string_from_lit("");

    // Capture stdout: run command with popen
    FILE* fp = popen(command, "r");
    if (fp == NULL) {
        return result;
    }

    // Read stdout
    char* stdout_buf = NULL;
    size_t stdout_len = 0;
    size_t stdout_cap = 0;
    char chunk[4096];
    size_t n;
    while ((n = fread(chunk, 1, sizeof(chunk), fp)) > 0) {
        if (stdout_len + n > stdout_cap) {
            stdout_cap = (stdout_len + n) * 2;
            stdout_buf = realloc(stdout_buf, stdout_cap + 1);
        }
        memcpy(stdout_buf + stdout_len, chunk, n);
        stdout_len += n;
    }

    int status = pclose(fp);
    if (WIFEXITED(status)) {
        result.exitCode = WEXITSTATUS(status);
    }

    if (stdout_buf != NULL) {
        stdout_buf[stdout_len] = '\0';
        dex_release(result.output);
        // dex_string_from_cstr takes ownership and frees stdout_buf
        result.output = dex_string_from_cstr(stdout_buf);
    }

    // Capture stderr separately by redirecting
    size_t cmd_len = strlen(command);
    char* stderr_cmd = malloc(cmd_len + 32);
    snprintf(stderr_cmd, cmd_len + 32, "%s 2>&1 1>/dev/null", command);
    FILE* efp = popen(stderr_cmd, "r");
    free(stderr_cmd);
    if (efp != NULL) {
        char* stderr_buf = NULL;
        size_t stderr_len = 0;
        size_t stderr_cap = 0;
        while ((n = fread(chunk, 1, sizeof(chunk), efp)) > 0) {
            if (stderr_len + n > stderr_cap) {
                stderr_cap = (stderr_len + n) * 2;
                stderr_buf = realloc(stderr_buf, stderr_cap + 1);
            }
            memcpy(stderr_buf + stderr_len, chunk, n);
            stderr_len += n;
        }
        pclose(efp);
        if (stderr_buf != NULL) {
            stderr_buf[stderr_len] = '\0';
            dex_release(result.error);
            // dex_string_from_cstr takes ownership and frees stderr_buf
            result.error = dex_string_from_cstr(stderr_buf);
        }
    }

    return result;
}
