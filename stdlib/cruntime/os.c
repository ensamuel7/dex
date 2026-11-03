
#include <stdlib.h>
#include <stdio.h>
#include <string.h>
#include <unistd.h>
#include <poll.h>
#include <sys/wait.h>
#include <signal.h>
#include <fcntl.h>
#include <time.h>
#include <errno.h>
#include <pthread.h>

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
        return dex_xstrdup("");
    }
    return dex_xstrdup(val);
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
            char* new_buf = dex_xrealloc(buf, cap + 1);
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
static long dex_os_now_ms(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (long)ts.tv_sec * 1000 + ts.tv_nsec / 1000000;
}

/* Runs argv[0] with argv: through /bin/sh for os.exec and os.execWith,
 * looked up on PATH and run directly for os.run and os.runWith, where no
 * shell ever parses the arguments.
 *
 * append_to NULL or empty: stdout and stderr are collected into the result,
 * as os.exec always has. Otherwise both are appended to that file as they
 * arrive, in the order the command wrote them, and the result's output and
 * error stay empty: a build log is read while the build runs, and holding a
 * long one in memory twice is what this exists to avoid.
 *
 * timeout_ms 0: no deadline. Otherwise the command runs in a process group
 * of its own, and at the deadline the whole group gets SIGTERM, then SIGKILL
 * two seconds later: `sh -c "docker build ..."` must take docker with it, not
 * leave it running under a dead shell. A timed-out run reports exit code 124,
 * the code timeout(1) uses, and timedOut.
 *
 * input, when not empty, is written to the command's stdin, which is then
 * closed; otherwise stdin is inherited, as it always has been for os.exec. dir, when not empty, is the directory
 * the command runs in. A command that cannot be started -- not found, or the
 * directory missing -- exits 127, as a shell reports it. */
static Dex_ExecResult dex_os_spawn(char* const* argv, int timeout_ms, const char* append_to,
                                   const DexString* input, const char* dir) {
    Dex_ExecResult result;
    result.exitCode = -1;
    result.timedOut = 0;
    result.output = dex_string_from_lit("");
    result.error = dex_string_from_lit("");

    int sink = -1;
    if (append_to && append_to[0] != '\0') {
        sink = open(append_to, O_WRONLY | O_CREAT | O_APPEND | O_CLOEXEC, 0644);
        if (sink < 0) {
            dex_release(result.error);
            result.error = dex_string_from_cstr(dex_xstrdup("could not open the file to append output to"));
            return result;
        }
    }

    int stdout_pipe[2];
    int stderr_pipe[2];

    if (pipe(stdout_pipe) < 0) {
        if (sink >= 0) close(sink);
        return result;
    }
    if (pipe(stderr_pipe) < 0) {
        close(stdout_pipe[0]);
        close(stdout_pipe[1]);
        if (sink >= 0) close(sink);
        return result;
    }

    int stdin_pipe[2] = {-1, -1};
    if (input && input->len == 0) input = NULL;
    if (input && pipe(stdin_pipe) < 0) {
        close(stdout_pipe[0]); close(stdout_pipe[1]);
        close(stderr_pipe[0]); close(stderr_pipe[1]);
        if (sink >= 0) close(sink);
        return result;
    }

    pid_t pid = fork();
    if (pid < 0) {
        close(stdout_pipe[0]); close(stdout_pipe[1]);
        close(stderr_pipe[0]); close(stderr_pipe[1]);
        if (stdin_pipe[0] >= 0) { close(stdin_pipe[0]); close(stdin_pipe[1]); }
        if (sink >= 0) close(sink);
        return result;
    }

    if (pid == 0) {
        // Child: its own process group when there is a deadline, so the group
        // can be stopped as one. Without one it stays in the caller's, so a
        // Ctrl-C at the terminal still reaches it.
        if (timeout_ms > 0) setpgid(0, 0);
        // An HTTP server ignores SIGPIPE, and an ignored signal stays ignored
        // across exec; `producer | head` in the command needs the default.
        signal(SIGPIPE, SIG_DFL);
        close(stdout_pipe[0]);
        close(stderr_pipe[0]);
        dup2(stdout_pipe[1], STDOUT_FILENO);
        // Going to a file, stderr shares stdout's pipe: one pipe keeps the
        // order the command wrote in, where two would be read in whatever
        // order poll happened to see them.
        dup2(sink >= 0 ? stdout_pipe[1] : stderr_pipe[1], STDERR_FILENO);
        close(stdout_pipe[1]);
        close(stderr_pipe[1]);
        if (stdin_pipe[0] >= 0) {
            close(stdin_pipe[1]);
            dup2(stdin_pipe[0], STDIN_FILENO);
            close(stdin_pipe[0]);
        }
        if (dir && dir[0] != '\0' && chdir(dir) != 0) _exit(127);
        execvp(argv[0], argv);
        _exit(127);
    }
    if (timeout_ms > 0) setpgid(pid, pid);  // both sides, so neither races the other

    // Parent: close write ends, read from both pipes using poll
    close(stdout_pipe[1]);
    close(stderr_pipe[1]);

    // stdin is fed from the same poll loop as the output is drained: writing
    // it all first deadlocks a command that fills its stdout pipe before it
    // has read all of its input.
    size_t in_off = 0;
    size_t in_len = input ? input->len : 0;
    if (stdin_pipe[0] >= 0) {
        close(stdin_pipe[0]);
        fcntl(stdin_pipe[1], F_SETFL, fcntl(stdin_pipe[1], F_GETFL) | O_NONBLOCK);
    }
    // A command that exits without reading its input must not kill the
    // caller with SIGPIPE on the next write; the write fails with EPIPE.
    // Blocked on this thread alone: swapping the process-wide handler would
    // race another thread running a command at the same moment.
    sigset_t pipe_set, old_mask;
    int masked = 0;
    if (stdin_pipe[1] >= 0) {
        sigemptyset(&pipe_set);
        sigaddset(&pipe_set, SIGPIPE);
        masked = pthread_sigmask(SIG_BLOCK, &pipe_set, &old_mask) == 0;
    }

    char* out_buf = NULL; size_t out_len = 0; size_t out_cap = 0;
    char* err_buf = NULL; size_t err_len = 0; size_t err_cap = 0;

    struct pollfd fds[3];
    fds[0].fd = stdout_pipe[0];
    fds[0].events = POLLIN;
    fds[1].fd = stderr_pipe[0];
    fds[1].events = POLLIN;
    fds[2].fd = stdin_pipe[1];
    fds[2].events = POLLOUT;

    long deadline = timeout_ms > 0 ? dex_os_now_ms() + timeout_ms : 0;
    long killed_at = 0;
    int active = 2;
    while (active > 0) {
        int wait_ms = -1;
        if (deadline) {
            long now = dex_os_now_ms();
            if (!result.timedOut && now >= deadline) {
                result.timedOut = 1;
                kill(-pid, SIGTERM);
                killed_at = now;
            }
            if (result.timedOut && killed_at && now - killed_at >= 2000) {
                kill(-pid, SIGKILL);
                killed_at = 0;
            }
            wait_ms = result.timedOut ? 200 : (int)(deadline - now);
        }
        int ready = poll(fds, fds[2].fd >= 0 ? 3 : 2, wait_ms);
        if (ready < 0) break;
        if (ready == 0) continue;

        if (fds[2].fd >= 0 && (fds[2].revents & (POLLOUT | POLLERR | POLLHUP))) {
            ssize_t w = write(fds[2].fd, input->data + in_off, in_len - in_off);
            if (w > 0) in_off += (size_t)w;
            if ((w < 0 && errno != EAGAIN && errno != EINTR) || in_off >= in_len) {
                close(fds[2].fd);
                fds[2].fd = -1;
            }
        }

        for (int i = 0; i < 2; i++) {
            if (fds[i].fd < 0) continue;
            if (fds[i].revents & (POLLIN | POLLHUP)) {
                char chunk[4096];
                ssize_t n = read(fds[i].fd, chunk, sizeof(chunk));
                if (n > 0 && sink >= 0) {
                    ssize_t off = 0;
                    while (off < n) {
                        ssize_t w = write(sink, chunk + off, (size_t)(n - off));
                        if (w <= 0) break;
                        off += w;
                    }
                } else if (n > 0) {
                    char** buf = (i == 0) ? &out_buf : &err_buf;
                    size_t* len = (i == 0) ? &out_len : &err_len;
                    size_t* cap = (i == 0) ? &out_cap : &err_cap;
                    if (*len + (size_t)n > *cap) {
                        *cap = (*len + (size_t)n) * 2;
                        char* new_buf = dex_xrealloc(*buf, *cap + 1);
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

    // The pipes are closed, but the shell may outlive them; a timed-out one
    // gets the rest of its grace period and then SIGKILL.
    int status = 0;
    if (result.timedOut) {
        for (int tries = 0; tries < 20 && waitpid(pid, &status, WNOHANG) == 0; tries++) {
            usleep(100000);
        }
        kill(-pid, SIGKILL);
    }
    waitpid(pid, &status, 0);
    if (fds[2].fd >= 0) close(fds[2].fd);
    if (masked) {
        // A SIGPIPE raised by our own write is pending on this thread; it is
        // consumed here so unblocking does not deliver it.
        sigset_t pending;
        int sig;
        if (sigpending(&pending) == 0 && sigismember(&pending, SIGPIPE) && !sigismember(&old_mask, SIGPIPE)) {
            sigwait(&pipe_set, &sig);
        }
        pthread_sigmask(SIG_SETMASK, &old_mask, NULL);
    }
    if (sink >= 0) close(sink);

    if (out_buf) out_buf[out_len] = '\0';
    if (err_buf) err_buf[err_len] = '\0';

    if (result.timedOut) {
        result.exitCode = 124;
    } else if (WIFEXITED(status)) {
        result.exitCode = WEXITSTATUS(status);
    }

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

static Dex_ExecResult dex_os_shell(const char* command, int timeout_ms, const char* append_to,
                                   const DexString* input, const char* dir) {
    char* argv[] = {"/bin/sh", "-c", (char*)command, NULL};
    return dex_os_spawn(argv, timeout_ms, append_to, input, dir);
}

static Dex_ExecResult dex_os_exec(const char* command) {
    return dex_os_shell(command, 0, NULL, NULL, NULL);
}

static Dex_ExecResult dex_os_exec_with(const char* command, Dex_ExecOptions opts) {
    return dex_os_shell(command, opts.timeoutMs, opts.appendTo ? opts.appendTo->data : NULL,
                        opts.input, opts.dir ? opts.dir->data : NULL);
}

/* os.run and os.runWith: each element of args is one argument, exactly as
 * given -- no word splitting, no globbing, no expansion -- so a value that
 * came from a request cannot become a second command. args[0] is looked up
 * on PATH. An empty array runs nothing and reports exit code 127. */
#ifdef DEX_HAVE_ARRAYS
static Dex_ExecResult dex_os_run_argv(DexArrayString* args, Dex_ExecOptions opts) {
    int n = args ? (int)args->len : 0;
    if (n == 0) {
        Dex_ExecResult r;
        r.exitCode = 127;
        r.timedOut = 0;
        r.output = dex_string_from_lit("");
        r.error = dex_string_from_lit("os.run: no command given");
        return r;
    }
    char** argv = (char**)dex_xmalloc(sizeof(char*) * (size_t)(n + 1));
    for (int i = 0; i < n; i++) {
        DexString* a = args->data[i];
        argv[i] = a ? a->data : "";
    }
    argv[n] = NULL;
    Dex_ExecResult r = dex_os_spawn(argv, opts.timeoutMs, opts.appendTo ? opts.appendTo->data : NULL,
                                    opts.input, opts.dir ? opts.dir->data : NULL);
    free(argv);
    return r;
}

static Dex_ExecResult dex_os_run_plain(DexArrayString* args) {
    Dex_ExecOptions opts;
    memset(&opts, 0, sizeof(opts));
    return dex_os_run_argv(args, opts);
}
#endif
