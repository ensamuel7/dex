// DexLang StringBuilder Runtime
// Growable buffer with amortized O(1) append, refcounted via DexObjHeader.

typedef struct {
    DexObjHeader hdr;
    char* buf;
    size_t len;
    size_t cap;
} DexStringBuilder;

static void dex_sb_destroy(void* ptr) {
    DexStringBuilder* sb = (DexStringBuilder*)ptr;
    free(sb->buf);
}

static DexStringBuilder* dex_sb_new(void) {
    DexStringBuilder* sb = (DexStringBuilder*)dex_obj_alloc(sizeof(DexStringBuilder), dex_sb_destroy);
    sb->cap = 64;
    sb->len = 0;
    sb->buf = (char*)dex_xmalloc(64);
    DEX_PF_BUF_SET(sb, 64);
    sb->buf[0] = '\0';
    return sb;
}

static void dex_sb_ensure(DexStringBuilder* sb, size_t extra) {
    if (sb->len + extra + 1 > sb->cap) {
        while (sb->len + extra + 1 > sb->cap) sb->cap *= 2;
        sb->buf = (char*)dex_xrealloc(sb->buf, sb->cap);
        DEX_PF_BUF_SET(sb, sb->cap);
    }
}

static void dex_sb_append_str(DexStringBuilder* sb, DexString* s) {
    dex_sb_ensure(sb, s->len);
    memcpy(sb->buf + sb->len, s->data, s->len);
    sb->len += s->len;
    sb->buf[sb->len] = '\0';
}

// snprintf costs far more than the digits do: parsing "%ld" and running the
// format machinery is most of the call, and a JSON response built here writes
// one integer per numeric field — forty of them for a page of twenty records.
// Writing the digits out directly is what every fast itoa does.
//
// The negation is written as -(v + 1) + 1 so that LONG_MIN, whose positive is
// not representable, still converts.
static int dex_sb_write_long(char* out, long v) {
    char digits[24];
    int i = 0;
    unsigned long u;
    int neg = 0;
    if (v < 0) {
        neg = 1;
        u = (unsigned long)(-(v + 1)) + 1UL;
    } else {
        u = (unsigned long)v;
    }
    do {
        digits[i++] = (char)('0' + (u % 10));
        u /= 10;
    } while (u);
    int n = 0;
    if (neg) out[n++] = '-';
    while (i > 0) out[n++] = digits[--i];
    return n;
}

static void dex_sb_append_long(DexStringBuilder* sb, long v) {
    dex_sb_ensure(sb, 24);
    sb->len += (size_t)dex_sb_write_long(sb->buf + sb->len, v);
    sb->buf[sb->len] = '\0';
}

static void dex_sb_append_int(DexStringBuilder* sb, int v) {
    dex_sb_append_long(sb, (long)v);
}

static void dex_sb_append_double(DexStringBuilder* sb, double v) {
    char tmp[64];
    int n = snprintf(tmp, sizeof(tmp), "%g", v);
    dex_sb_ensure(sb, n);
    memcpy(sb->buf + sb->len, tmp, n);
    sb->len += n;
    sb->buf[sb->len] = '\0';
}

static void dex_sb_append_bool(DexStringBuilder* sb, _Bool v) {
    const char* s = v ? "true" : "false";
    size_t n = v ? 4 : 5;
    dex_sb_ensure(sb, n);
    memcpy(sb->buf + sb->len, s, n);
    sb->len += n;
    sb->buf[sb->len] = '\0';
}

static void dex_sb_append_char(DexStringBuilder* sb, unsigned char v) {
    dex_sb_ensure(sb, 1);
    sb->buf[sb->len] = (char)v;
    sb->len += 1;
    sb->buf[sb->len] = '\0';
}

static void dex_sb_append_cstr(DexStringBuilder* sb, const char* s) {
    size_t n = strlen(s);
    dex_sb_ensure(sb, n);
    memcpy(sb->buf + sb->len, s, n);
    sb->len += n;
    sb->buf[sb->len] = '\0';
}

static DexString* dex_sb_toString(DexStringBuilder* sb) {
    return dex_string_new(sb->buf, sb->len);
}

static int dex_sb_len(DexStringBuilder* sb) {
    return (int)sb->len;
}

static void dex_sb_clear(DexStringBuilder* sb) {
    sb->len = 0;
    sb->buf[0] = '\0';
}
