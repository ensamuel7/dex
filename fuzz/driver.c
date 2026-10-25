#include <dirent.h>
#include <errno.h>
#include <signal.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <sys/time.h>
#include <time.h>
#include <unistd.h>

int LLVMFuzzerTestOneInput(const uint8_t* data, size_t size);

typedef struct {
    uint8_t* data;
    size_t size;
} Unit;

typedef struct {
    Unit* items;
    size_t len;
    size_t cap;
} UnitList;

static uint8_t* cov_seen;
static uint32_t cov_count;
static volatile uint32_t cov_new;
static uint32_t cov_total;

void __sanitizer_cov_trace_pc_guard_init(uint32_t* start, uint32_t* stop) {
    if (start == stop || *start) return;
    uint32_t old = cov_count;
    for (uint32_t* g = start; g < stop; g++) *g = ++cov_count;
    uint8_t* grown = (uint8_t*)realloc(cov_seen, (size_t)cov_count + 1);
    if (!grown) abort();
    memset(grown + old, 0, (size_t)(cov_count - old) + 1);
    cov_seen = grown;
}

void __sanitizer_cov_trace_pc_guard(uint32_t* guard) {
    uint32_t g = *guard;
    if (!g || cov_seen[g]) return;
    cov_seen[g] = 1;
    cov_new++;
    cov_total++;
}

void __sanitizer_set_death_callback(void (*cb)(void)) __attribute__((weak));

static const uint8_t* current_data;
static size_t current_size;
static const char* artifact_prefix = "./";
static int timeout_secs = 10;

static void save_artifact(const char* kind) {
    if (!current_data) return;
    char path[4096];
    uint64_t h = 1469598103934665603ULL;
    for (size_t i = 0; i < current_size; i++) h = (h ^ current_data[i]) * 1099511628211ULL;
    snprintf(path, sizeof(path), "%s%s-%016llx", artifact_prefix, kind, (unsigned long long)h);
    FILE* f = fopen(path, "wb");
    if (!f) return;
    fwrite(current_data, 1, current_size, f);
    fclose(f);
    fprintf(stderr, "==driver== %zu-byte input written to %s\n", current_size, path);
}

static void on_death(void) {
    save_artifact("crash");
}

static void on_signal(int sig) {
    if (sig == SIGALRM) {
        fprintf(stderr, "==driver== timeout after %d s\n", timeout_secs);
        save_artifact("timeout");
        _exit(70);
    }
    save_artifact("crash");
    signal(sig, SIG_DFL);
    raise(sig);
}

static void run_one(const uint8_t* data, size_t size) {
    uint8_t* copy = (uint8_t*)malloc(size ? size : 1);
    if (!copy) abort();
    memcpy(copy, data, size);
    current_data = copy;
    current_size = size;
    alarm((unsigned)timeout_secs);
    LLVMFuzzerTestOneInput(copy, size);
    alarm(0);
    current_data = NULL;
    free(copy);
}

static void list_push(UnitList* l, const uint8_t* data, size_t size) {
    if (l->len == l->cap) {
        l->cap = l->cap ? l->cap * 2 : 64;
        l->items = (Unit*)realloc(l->items, l->cap * sizeof(Unit));
        if (!l->items) abort();
    }
    uint8_t* d = (uint8_t*)malloc(size ? size : 1);
    if (!d) abort();
    memcpy(d, data, size);
    l->items[l->len].data = d;
    l->items[l->len].size = size;
    l->len++;
}

static int read_file(const char* path, uint8_t** out, size_t* out_size) {
    FILE* f = fopen(path, "rb");
    if (!f) return 0;
    size_t cap = 4096, n = 0;
    uint8_t* buf = (uint8_t*)malloc(cap);
    size_t r;
    while (buf && (r = fread(buf + n, 1, cap - n, f)) > 0) {
        n += r;
        if (n == cap) {
            cap *= 2;
            buf = (uint8_t*)realloc(buf, cap);
        }
    }
    fclose(f);
    if (!buf) return 0;
    *out = buf;
    *out_size = n;
    return 1;
}

static void load_path(UnitList* l, const char* path) {
    struct stat st;
    if (stat(path, &st) != 0) {
        fprintf(stderr, "==driver== cannot read %s: %s\n", path, strerror(errno));
        return;
    }
    if (S_ISDIR(st.st_mode)) {
        DIR* d = opendir(path);
        if (!d) return;
        struct dirent* e;
        while ((e = readdir(d))) {
            if (e->d_name[0] == '.') continue;
            char child[4096];
            snprintf(child, sizeof(child), "%s/%s", path, e->d_name);
            load_path(l, child);
        }
        closedir(d);
        return;
    }
    uint8_t* data;
    size_t size;
    if (read_file(path, &data, &size)) {
        list_push(l, data, size);
        free(data);
    }
}

static int hexval(int c) {
    if (c >= '0' && c <= '9') return c - '0';
    if (c >= 'a' && c <= 'f') return c - 'a' + 10;
    if (c >= 'A' && c <= 'F') return c - 'A' + 10;
    return -1;
}

static void load_dict(UnitList* l, const char* path) {
    uint8_t* data;
    size_t size;
    if (!read_file(path, &data, &size)) {
        fprintf(stderr, "==driver== cannot read dictionary %s\n", path);
        return;
    }
    size_t i = 0;
    while (i < size) {
        size_t eol = i;
        while (eol < size && data[eol] != '\n') eol++;
        size_t q = i;
        while (q < eol && data[q] != '"') q++;
        if (q < eol && data[i] != '#') {
            uint8_t tok[256];
            size_t tn = 0;
            size_t p = q + 1;
            while (p < eol && data[p] != '"' && tn < sizeof(tok)) {
                if (data[p] == '\\' && p + 1 < eol) {
                    if (data[p + 1] == 'x' && p + 3 < eol && hexval(data[p + 2]) >= 0 && hexval(data[p + 3]) >= 0) {
                        tok[tn++] = (uint8_t)(hexval(data[p + 2]) * 16 + hexval(data[p + 3]));
                        p += 4;
                        continue;
                    }
                    tok[tn++] = data[p + 1];
                    p += 2;
                    continue;
                }
                tok[tn++] = data[p++];
            }
            if (tn) list_push(l, tok, tn);
        }
        i = eol + 1;
    }
    free(data);
}

static uint64_t rng_state;

static uint64_t rnd(void) {
    rng_state ^= rng_state << 13;
    rng_state ^= rng_state >> 7;
    rng_state ^= rng_state << 17;
    return rng_state;
}

static size_t rnd_below(size_t n) {
    return n ? (size_t)(rnd() % n) : 0;
}

static const char* interesting[] = {
    "0", "-1", "2147483647", "2147483648", "-2147483649", "9223372036854775807",
    "9223372036854775808", "1e308", "1e999", "-0", "0.0000001", "\\u0000", "\\uD800",
    "\\uDBFF\\uDC00", "\r\n", "\r\n\r\n", "\x00", "\xff", "\x7f", "%n", "%s",
};

static size_t mutate(uint8_t* buf, size_t size, size_t max, const UnitList* corpus, const UnitList* dict) {
    int rounds = 1 + (int)rnd_below(6);
    for (int r = 0; r < rounds; r++) {
        switch (rnd_below(12)) {
        case 0:
            if (size) buf[rnd_below(size)] ^= (uint8_t)(1u << rnd_below(8));
            break;
        case 1:
            if (size) buf[rnd_below(size)] = (uint8_t)rnd();
            break;
        case 2:
            if (size) {
                size_t at = rnd_below(size);
                size_t n = 1 + rnd_below(size - at < 16 ? size - at : 16);
                memmove(buf + at, buf + at + n, size - at - n);
                size -= n;
            }
            break;
        case 3:
            if (size < max) {
                size_t n = 1 + rnd_below(max - size < 8 ? max - size : 8);
                size_t at = rnd_below(size + 1);
                memmove(buf + at + n, buf + at, size - at);
                for (size_t i = 0; i < n; i++) buf[at + i] = (uint8_t)rnd();
                size += n;
            }
            break;
        case 4:
            if (size && size < max) {
                size_t from = rnd_below(size);
                size_t n = 1 + rnd_below(size - from);
                if (n > max - size) n = max - size;
                size_t at = rnd_below(size + 1);
                uint8_t tmp[4096];
                if (n > sizeof(tmp)) n = sizeof(tmp);
                memcpy(tmp, buf + from, n);
                memmove(buf + at + n, buf + at, size - at);
                memcpy(buf + at, tmp, n);
                size += n;
            }
            break;
        case 5:
        case 6:
            if (dict->len) {
                const Unit* t = &dict->items[rnd_below(dict->len)];
                if (size + t->size <= max) {
                    size_t at = rnd_below(size + 1);
                    if (rnd_below(2) && at + t->size <= size) {
                        memcpy(buf + at, t->data, t->size);
                    } else {
                        memmove(buf + at + t->size, buf + at, size - at);
                        memcpy(buf + at, t->data, t->size);
                        size += t->size;
                    }
                }
            }
            break;
        case 7: {
            const char* s = interesting[rnd_below(sizeof(interesting) / sizeof(interesting[0]))];
            size_t n = strlen(s);
            if (!n) n = 1;
            if (size + n <= max) {
                size_t at = rnd_below(size + 1);
                memmove(buf + at + n, buf + at, size - at);
                memcpy(buf + at, s, n);
                size += n;
            }
            break;
        }
        case 8:
            if (corpus->len) {
                const Unit* o = &corpus->items[rnd_below(corpus->len)];
                if (o->size) {
                    size_t keep = rnd_below(size + 1);
                    size_t from = rnd_below(o->size);
                    size_t n = o->size - from;
                    if (keep + n > max) n = max - keep;
                    memcpy(buf + keep, o->data + from, n);
                    size = keep + n;
                }
            }
            break;
        case 9:
            if (size) {
                size_t at = rnd_below(size);
                size_t n = 1 + rnd_below(size - at);
                uint8_t c = buf[rnd_below(size)];
                memset(buf + at, c, n);
            }
            break;
        case 10:
            if (size && size < max) {
                size_t at = rnd_below(size);
                size_t reps = 1 + rnd_below(64);
                for (size_t i = 0; i < reps && size < max; i++) {
                    memmove(buf + at + 1, buf + at, size - at);
                    size++;
                }
            }
            break;
        case 11:
            if (size > 1) size = 1 + rnd_below(size);
            break;
        }
    }
    return size;
}

static double now_secs(void) {
    struct timeval tv;
    gettimeofday(&tv, NULL);
    return (double)tv.tv_sec + (double)tv.tv_usec / 1e6;
}

static void usage(const char* argv0) {
    fprintf(stderr,
        "usage: %s [-runs=N] [-max_total_time=S] [-max_len=N] [-seed=N] [-timeout=S]\n"
        "          [-dict=FILE] [-artifact_prefix=DIR/] [corpus dir or file ...]\n",
        argv0);
}

int main(int argc, char** argv) {
    long runs = -1;
    double max_time = 0;
    size_t max_len = 4096;
    uint64_t seed = 0;
    UnitList corpus = {0};
    UnitList dict = {0};

    for (int i = 1; i < argc; i++) {
        const char* a = argv[i];
        if (strncmp(a, "-runs=", 6) == 0) runs = atol(a + 6);
        else if (strncmp(a, "-max_total_time=", 16) == 0) max_time = atof(a + 16);
        else if (strncmp(a, "-max_len=", 9) == 0) max_len = (size_t)atol(a + 9);
        else if (strncmp(a, "-seed=", 6) == 0) seed = (uint64_t)strtoull(a + 6, NULL, 10);
        else if (strncmp(a, "-timeout=", 9) == 0) timeout_secs = atoi(a + 9);
        else if (strncmp(a, "-dict=", 6) == 0) load_dict(&dict, a + 6);
        else if (strncmp(a, "-artifact_prefix=", 17) == 0) artifact_prefix = a + 17;
        else if (a[0] == '-') { usage(argv[0]); return 2; }
        else load_path(&corpus, a);
    }
    if (max_len < 1) max_len = 1;
    if (!seed) seed = (uint64_t)time(NULL) ^ ((uint64_t)getpid() << 32);
    rng_state = seed | 1;

    if (__sanitizer_set_death_callback) __sanitizer_set_death_callback(on_death);
    signal(SIGALRM, on_signal);
    signal(SIGABRT, on_signal);

    size_t seeds = corpus.len;
    for (size_t i = 0; i < seeds; i++) {
        cov_new = 0;
        run_one(corpus.items[i].data, corpus.items[i].size);
    }
    fprintf(stderr, "==driver== seed=%llu ran %zu corpus inputs, %u edges of %u\n",
            (unsigned long long)seed, seeds, cov_total, cov_count);

    if (runs == 0 || (runs < 0 && max_time <= 0 && seeds > 0)) return 0;
    if (runs < 0 && max_time <= 0) runs = 100000;
    if (corpus.len == 0) list_push(&corpus, (const uint8_t*)"", 0);

    uint8_t* buf = (uint8_t*)malloc(max_len);
    if (!buf) abort();
    double start = now_secs();
    double last_report = start;
    long done = 0;
    while (runs < 0 || done < runs) {
        if (max_time > 0 && (done & 63) == 0 && now_secs() - start >= max_time) break;
        const Unit* base = &corpus.items[rnd_below(corpus.len)];
        size_t size = base->size < max_len ? base->size : max_len;
        memcpy(buf, base->data, size);
        size = mutate(buf, size, max_len, &corpus, &dict);
        cov_new = 0;
        run_one(buf, size);
        if (cov_new) list_push(&corpus, buf, size);
        done++;
        double t = now_secs();
        if (t - last_report >= 10) {
            fprintf(stderr, "==driver== #%ld  %.0f exec/s  corpus %zu  edges %u/%u\n",
                    done, (double)done / (t - start), corpus.len, cov_total, cov_count);
            last_report = t;
        }
    }
    double t = now_secs();
    fprintf(stderr, "==driver== done: %ld runs in %.0f s, corpus %zu, edges %u/%u\n",
            done, t - start, corpus.len, cov_total, cov_count);
    free(buf);
    return 0;
}
