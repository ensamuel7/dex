
#include <stdlib.h>
#include <stdio.h>
#include <string.h>
#include <unistd.h>
#include <poll.h>
#include <sys/wait.h>

/* main stores its argv here when a program calls os.args(). Codegen only
 * gives main the (argc, argv) signature for a program that asks, so these stay
 * zero otherwise and os.args() hands back an empty array rather than reading
 * a pointer nobody set. */
static int _dex_argc = 0;
static char** _dex_argv = 0;

/* Argument zero is the path the program was invoked by, not an argument, so it
 * is skipped -- `args()` is what the caller typed.
 *
 * Guarded because this is the only thing in the os runtime that needs the
 * array runtime, and that one is emitted only for a program that uses arrays.
 * Importing os without calling args() must not drag it in. */
#ifdef DEX_HAVE_ARRAYS
static DexArrayString* dex_os_args(void) {
    DexArrayString* a = dex_array_string_new();
    for (int i = 1; i < _dex_argc; i++) {
        DexString* s = dex_string_new(_dex_argv[i], strlen(_dex_argv[i]));
        dex_array_string_push(a, s);
        dex_release(s);
    }
    return a;
}
#endif

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

// Read all output from a file descriptor into a malloc'd buffer.
// Returns the buffer and sets *out_len to the number of bytes read.
static char* dex_os_read_fd(int fd, size_t* out_len) {
    char* buf = NULL;
    size_t len = 0;
    size_t cap = 0;
    char chunk[4096];
    ssize_t n;
    while ((n = read(fd, chunk, sizeof(chunk))) > 0) {
        if (len + (size_t)n > cap) {
            cap = (len + (size_t)n) * 2;
            char* new_buf = realloc(buf, cap + 1);
            if (!new_buf) {
                if (buf) buf[len] = '\0';
                *out_len = len;
                return buf;
            }
            buf = new_buf;
        }
        memcpy(buf + len, chunk, (size_t)n);
        len += (size_t)n;
    }
    if (buf != NULL) {
        buf[len] = '\0';
    }
    *out_len = len;
    return buf;
}

// Execute a command using fork+exec with separate pipes for stdout and stderr.
// This captures both streams in a single execution, avoiding the security and
// correctness issues of running the command twice.
static Dex_ExecResult dex_os_exec(const char* command) {
    Dex_ExecResult result;
    result.exitCode = -1;
    result.output = dex_string_from_lit("");
    result.error = dex_string_from_lit("");

    int stdout_pipe[2];
    int stderr_pipe[2];

    if (pipe(stdout_pipe) < 0) {
        return result;
    }
    if (pipe(stderr_pipe) < 0) {
        close(stdout_pipe[0]);
        close(stdout_pipe[1]);
        return result;
    }

    pid_t pid = fork();
    if (pid < 0) {
        close(stdout_pipe[0]); close(stdout_pipe[1]);
        close(stderr_pipe[0]); close(stderr_pipe[1]);
        return result;
    }

    if (pid == 0) {
        // Child: redirect stdout/stderr to pipes, then exec via shell
        close(stdout_pipe[0]);
        close(stderr_pipe[0]);
        dup2(stdout_pipe[1], STDOUT_FILENO);
        dup2(stderr_pipe[1], STDERR_FILENO);
        close(stdout_pipe[1]);
        close(stderr_pipe[1]);
        execl("/bin/sh", "sh", "-c", command, (char*)NULL);
        _exit(127);
    }

    // Parent: close write ends, read from both pipes using poll
    close(stdout_pipe[1]);
    close(stderr_pipe[1]);

    char* out_buf = NULL; size_t out_len = 0; size_t out_cap = 0;
    char* err_buf = NULL; size_t err_len = 0; size_t err_cap = 0;

    struct pollfd fds[2];
    fds[0].fd = stdout_pipe[0];
    fds[0].events = POLLIN;
    fds[1].fd = stderr_pipe[0];
    fds[1].events = POLLIN;

    int active = 2;
    while (active > 0) {
        if (poll(fds, 2, -1) < 0) break;

        for (int i = 0; i < 2; i++) {
            if (fds[i].fd < 0) continue;
            if (fds[i].revents & (POLLIN | POLLHUP)) {
                char chunk[4096];
                ssize_t n = read(fds[i].fd, chunk, sizeof(chunk));
                if (n > 0) {
                    char** buf = (i == 0) ? &out_buf : &err_buf;
                    size_t* len = (i == 0) ? &out_len : &err_len;
                    size_t* cap = (i == 0) ? &out_cap : &err_cap;
                    if (*len + (size_t)n > *cap) {
                        *cap = (*len + (size_t)n) * 2;
                        char* new_buf = realloc(*buf, *cap + 1);
                        if (new_buf) *buf = new_buf;
                    }
                    if (*buf) {
                        memcpy(*buf + *len, chunk, (size_t)n);
                        *len += (size_t)n;
                    }
                } else {
                    close(fds[i].fd);
                    fds[i].fd = -1;
                    active--;
                }
            }
        }
    }

    // Null-terminate buffers
    if (out_buf) out_buf[out_len] = '\0';
    if (err_buf) err_buf[err_len] = '\0';

    // Wait for child
    int status;
    waitpid(pid, &status, 0);
    if (WIFEXITED(status)) {
        result.exitCode = WEXITSTATUS(status);
    }

    // Convert to DexString
    if (out_buf) {
        dex_release(result.output);
        result.output = dex_string_from_cstr(out_buf);
    }
    if (err_buf) {
        dex_release(result.error);
        result.error = dex_string_from_cstr(err_buf);
    }

    return result;
}
