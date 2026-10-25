#include <signal.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <sys/resource.h>

#if defined(__has_feature)
#if __has_feature(address_sanitizer) || __has_feature(thread_sanitizer) || __has_feature(memory_sanitizer)
#define DEX_NO_STACK_GUARD 1
#endif
#endif
#if defined(__SANITIZE_ADDRESS__) || defined(__SANITIZE_THREAD__)
#define DEX_NO_STACK_GUARD 1
#endif

#ifndef DEX_NO_STACK_GUARD
static _Thread_local char* _dex_stack_hi;
static _Thread_local size_t _dex_stack_size;

static void dex_fault_write(const char* s) {
    ssize_t r = write(2, s, strlen(s));
    (void)r;
}

static void dex_fault_handler(int sig, siginfo_t* info, void* ctx) {
    (void)ctx;
    char* addr = (char*)info->si_addr;
    char* hi = _dex_stack_hi;
    size_t slack = 1 << 20;
    if (hi && addr <= hi && (size_t)(hi - addr) <= _dex_stack_size + slack) {
        dex_fault_write("runtime error: stack overflow (recursion too deep)\n");
    } else if (sig == SIGBUS) {
        dex_fault_write("runtime error: bus error\n");
    } else {
        dex_fault_write("runtime error: segmentation fault\n");
    }
    signal(sig, SIG_DFL);
}

#define DEX_ALTSTACK_SIZE (64 * 1024)

static void dex_guard_thread(char* hi, size_t size, char* alt) {
    _dex_stack_hi = hi;
    _dex_stack_size = size;
    stack_t ss;
    ss.ss_sp = alt;
    ss.ss_size = DEX_ALTSTACK_SIZE;
    ss.ss_flags = 0;
    sigaltstack(&ss, NULL);
}

static char _dex_main_altstack[DEX_ALTSTACK_SIZE];

static void dex_install_fault_handler(char* hi) {
    struct rlimit rl;
    size_t size = 8 << 20;
    if (getrlimit(RLIMIT_STACK, &rl) == 0 && rl.rlim_cur != RLIM_INFINITY) {
        size = (size_t)rl.rlim_cur;
    }
    dex_guard_thread(hi, size, _dex_main_altstack);
    struct sigaction sa;
    memset(&sa, 0, sizeof(sa));
    sa.sa_sigaction = dex_fault_handler;
    sa.sa_flags = SA_SIGINFO | SA_ONSTACK;
    sigemptyset(&sa.sa_mask);
    sigaction(SIGSEGV, &sa, NULL);
    sigaction(SIGBUS, &sa, NULL);
}
#else
#define DEX_ALTSTACK_SIZE 1
static void dex_guard_thread(char* hi, size_t size, char* alt) { (void)hi; (void)size; (void)alt; }
static void dex_install_fault_handler(char* hi) { (void)hi; }
#endif

#include <limits.h>

static long dex_parse_long(const char* s) {
    if (!s) return 0;
    return strtol(s, NULL, 10);
}

static int dex_parse_int(const char* s) {
    long v = dex_parse_long(s);
    if (v > INT_MAX) return INT_MAX;
    if (v < INT_MIN) return INT_MIN;
    return (int)v;
}

static double dex_parse_double(const char* s) {
    if (!s) return 0.0;
    return strtod(s, NULL);
}

static void dex_out_of_memory(void) {
    const char* msg = "runtime error: out of memory\n";
    ssize_t r = write(2, msg, strlen(msg));
    (void)r;
    exit(1);
}

static void* dex_xmalloc(size_t n) {
    void* p = malloc(n);
    if (!p && n) dex_out_of_memory();
    return p;
}

static void* dex_xcalloc(size_t count, size_t n) {
    void* p = calloc(count, n);
    if (!p && count && n) dex_out_of_memory();
    return p;
}

static void* dex_xrealloc(void* old, size_t n) {
    void* p = realloc(old, n);
    if (!p && n) dex_out_of_memory();
    return p;
}

static char* dex_xstrdup(const char* s) {
    char* p = strdup(s);
    if (!p) dex_out_of_memory();
    return p;
}
