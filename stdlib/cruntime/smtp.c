// SMTP client.
//
// Enough of RFC 5321 to hand one message to one submission server and know
// whether it took it. Like the Redis module this speaks the protocol directly
// over a socket rather than linking a client library, so `dex build` keeps
// working on a machine with no mail headers installed. Unlike Redis it needs
// TLS, which comes from OpenSSL the same way the ws module's wss:// does — the
// same detection, the same context, the same SSL_read/SSL_write shims.
//
// One call opens a connection, sends one message and closes it. There is no
// pool: a password reset sends one mail every few minutes, and a pooled SMTP
// session that has gone stale behind a NAT costs more than a fresh connect.
//
// Three things here are not optional and are easy to get wrong:
//
//   * Every reply code is checked. "The connection succeeded" is not "the mail
//     was sent" — a server that refuses the recipient, or the password, or the
//     message, says so in a code, and this returns false for all of them.
//   * A multi-line reply is read to its end. `250-` is a continuation and
//     `250 ` is the last line; stopping at the first one leaves the rest in the
//     socket, and the next command reads someone else's answer.
//   * Lines end CRLF and a body line beginning with `.` is doubled. A bare LF
//     is a protocol violation some servers reject outright, and an un-stuffed
//     leading dot ends the message early — which looks like a truncated email
//     rather than an error anybody reports.

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <errno.h>
#include <time.h>
#include <unistd.h>
#include <fcntl.h>
#include <poll.h>
#include <sys/types.h>
#include <sys/socket.h>
#include <sys/time.h>
#include <netinet/in.h>
#include <netinet/tcp.h>
#include <netdb.h>

// A hung SMTP server must not hold a request thread forever: a reset email is
// sent from inside an HTTP handler, and a mail host that accepts the connection
// and then says nothing would otherwise pin that worker until the process dies.
#define DEX_SMTP_TIMEOUT_SECS 30

// Submission over implicit TLS. The port is the only thing that distinguishes
// it: 465 is encrypted from the first byte, everything else opens in the clear
// and upgrades with STARTTLS. Overridable so the branch can be exercised on a
// port a test can bind without root.
#ifndef DEX_SMTP_IMPLICIT_TLS_PORT
#define DEX_SMTP_IMPLICIT_TLS_PORT 465
#endif

// --- TLS (optional, requires OpenSSL) ---
//
// Detected exactly as ws.c detects it, and for the same reason: a header on the
// include path does not mean the library is on the link line, so the build says
// so explicitly. DEX_SSL_DISABLED wins over anything the header search finds.

#ifndef DEX_SSL_DISABLED
  #ifdef __has_include
    #if __has_include(<openssl/ssl.h>)
      #define DEX_SMTP_TLS 1
    #endif
  #endif
  #ifdef DEX_HAS_SSL
    #define DEX_SMTP_TLS 1
  #endif
#endif

#ifdef DEX_SMTP_TLS
#include <openssl/ssl.h>
#include <openssl/err.h>
#include <openssl/x509.h>
#include <pthread.h>

// The handle is kept as void* rather than SSL* throughout, so this file needs
// no typedef of its own. ws.c declares `typedef void SSL;` in its no-OpenSSL
// branch, and two modules in one translation unit must not both try.
static SSL_CTX* dex_smtp_ssl_ctx = NULL;
static pthread_mutex_t dex_smtp_ssl_lock = PTHREAD_MUTEX_INITIALIZER;

static SSL_CTX* dex_smtp_get_ctx(void) {
    pthread_mutex_lock(&dex_smtp_ssl_lock);
    if (!dex_smtp_ssl_ctx) {
        SSL_library_init();
        SSL_load_error_strings();
        OpenSSL_add_all_algorithms();
        dex_smtp_ssl_ctx = SSL_CTX_new(TLS_client_method());
        if (dex_smtp_ssl_ctx) {
            // Loaded so the chain is actually built and SSL_get_verify_result
            // has something to say. The handshake is not failed on a bad
            // certificate — that matches the ws module, and mail hosts on
            // shared hosting very often present a certificate for the hosting
            // provider rather than for the name you dialled — but it is
            // reported, because the alternative is sending a password into a
            // connection nobody ever looked at.
            SSL_CTX_set_default_verify_paths(dex_smtp_ssl_ctx);
        }
    }
    SSL_CTX* ctx = dex_smtp_ssl_ctx;
    pthread_mutex_unlock(&dex_smtp_ssl_lock);
    return ctx;
}
#endif

// --- The connection ---

typedef struct {
    int fd;              // -1 when closed
    void* ssl;           // SSL*, or NULL for a plaintext connection
    char buf[4096];      // read buffer: replies arrive in whatever chunks they like
    size_t len;          // bytes held
    size_t pos;          // bytes consumed
    char last[512];      // the final line of the last reply, for the log
} DexSmtpSock;

static int dex_smtp_write(DexSmtpSock* s, const char* buf, size_t len) {
    size_t sent = 0;
    while (sent < len) {
        int n;
#ifdef DEX_SMTP_TLS
        if (s->ssl) {
            n = SSL_write((SSL*)s->ssl, buf + sent, (int)(len - sent));
        } else
#endif
        {
            // MSG_NOSIGNAL where it exists, SO_NOSIGPIPE on the socket where it
            // does not: a write to a server that hung up must return an error,
            // not kill the process with SIGPIPE.
#ifdef MSG_NOSIGNAL
            n = (int)send(s->fd, buf + sent, len - sent, MSG_NOSIGNAL);
#else
            n = (int)send(s->fd, buf + sent, len - sent, 0);
#endif
        }
        if (n <= 0) {
            if (n < 0 && errno == EINTR) continue;
            return -1;
        }
        sent += (size_t)n;
    }
    return 0;
}

static int dex_smtp_fill(DexSmtpSock* s) {
    if (s->pos == s->len) {
        s->pos = 0;
        s->len = 0;
    } else if (s->len == sizeof(s->buf)) {
        if (s->pos == 0) return -1;   // one reply line longer than the buffer
        memmove(s->buf, s->buf + s->pos, s->len - s->pos);
        s->len -= s->pos;
        s->pos = 0;
    }
    int n;
#ifdef DEX_SMTP_TLS
    if (s->ssl) {
        n = SSL_read((SSL*)s->ssl, s->buf + s->len, (int)(sizeof(s->buf) - s->len));
    } else
#endif
    {
        n = (int)recv(s->fd, s->buf + s->len, sizeof(s->buf) - s->len, 0);
    }
    if (n <= 0) return -1;
    s->len += (size_t)n;
    return n;
}

// One CRLF-terminated line, with the terminator stripped. A lone LF is accepted
// on the way in — being strict about what a server sends buys nothing — but
// never sent.
static int dex_smtp_read_line(DexSmtpSock* s, char* out, size_t out_size) {
    size_t scan = s->pos;
    for (;;) {
        while (scan < s->len) {
            if (s->buf[scan] == '\n') {
                size_t end = scan;
                if (end > s->pos && s->buf[end - 1] == '\r') end--;
                size_t n = end - s->pos;
                if (n >= out_size) n = out_size - 1;
                memcpy(out, s->buf + s->pos, n);
                out[n] = '\0';
                s->pos = scan + 1;
                return (int)n;
            }
            scan++;
        }
        size_t offset = scan - s->pos;   // fill() may slide the buffer down
        if (dex_smtp_fill(s) < 0) return -1;
        scan = s->pos + offset;
    }
}

// Reads a complete reply and returns its three-digit code, or -1.
//
// `250-SIZE` is a continuation and `250 SIZE` is the last line, so the loop
// runs until a line whose fourth character is not '-'. `capture`, when given,
// collects the text after the code from every line — that is how the EHLO
// capability list gets back to the caller.
static int dex_smtp_read_reply(DexSmtpSock* s, char* capture, size_t cap_size) {
    char line[1024];
    int code = -1;
    size_t used = 0;

    if (capture && cap_size) capture[0] = '\0';
    for (;;) {
        if (dex_smtp_read_line(s, line, sizeof(line)) < 0) return -1;
        if (line[0] < '0' || line[0] > '9' ||
            line[1] < '0' || line[1] > '9' ||
            line[2] < '0' || line[2] > '9') {
            return -1;
        }
        int this_code = (line[0] - '0') * 100 + (line[1] - '0') * 10 + (line[2] - '0');
        // A reply whose lines disagree about their own code is not one reply.
        if (code < 0) code = this_code;
        else if (this_code != code) return -1;

        snprintf(s->last, sizeof(s->last), "%s", line);

        if (capture && cap_size) {
            const char* text = line + 3;
            if (*text) text++;   // step over the '-' or ' '
            size_t n = strlen(text);
            if (used + n + 2 < cap_size) {
                memcpy(capture + used, text, n);
                used += n;
                capture[used++] = '\n';
                capture[used] = '\0';
            }
        }
        if (line[3] != '-') return code;   // '\0' on a bare "250" ends it too
    }
}

static int dex_smtp_cmd(DexSmtpSock* s, const char* text, char* capture, size_t cap_size) {
    if (dex_smtp_write(s, text, strlen(text)) < 0) return -1;
    return dex_smtp_read_reply(s, capture, cap_size);
}

// --- Connecting ---

static int dex_smtp_dial(const char* host, int port) {
    char portstr[16];
    snprintf(portstr, sizeof(portstr), "%d", port);

    struct addrinfo hints, *res = NULL;
    memset(&hints, 0, sizeof(hints));
    hints.ai_family = AF_UNSPEC;
    hints.ai_socktype = SOCK_STREAM;
    if (getaddrinfo(host, portstr, &hints, &res) != 0) return -1;

    int fd = -1;
    for (struct addrinfo* a = res; a; a = a->ai_next) {
        fd = socket(a->ai_family, a->ai_socktype, a->ai_protocol);
        if (fd < 0) continue;

        // Non-blocking for the connect alone, so a host that blackholes SYNs
        // costs the timeout rather than the kernel's several minutes.
        int flags = fcntl(fd, F_GETFL, 0);
        fcntl(fd, F_SETFL, flags | O_NONBLOCK);

        int rc = connect(fd, a->ai_addr, a->ai_addrlen);
        if (rc != 0) {
            if (errno != EINPROGRESS) { close(fd); fd = -1; continue; }
            struct pollfd pfd;
            pfd.fd = fd;
            pfd.events = POLLOUT;
            pfd.revents = 0;
            if (poll(&pfd, 1, DEX_SMTP_TIMEOUT_SECS * 1000) != 1) {
                close(fd); fd = -1; continue;
            }
            int err = 0;
            socklen_t errlen = sizeof(err);
            if (getsockopt(fd, SOL_SOCKET, SO_ERROR, &err, &errlen) != 0 || err != 0) {
                close(fd); fd = -1; continue;
            }
        }
        fcntl(fd, F_SETFL, flags);
        break;
    }
    freeaddrinfo(res);
    if (fd < 0) return -1;

    struct timeval tv;
    tv.tv_sec = DEX_SMTP_TIMEOUT_SECS;
    tv.tv_usec = 0;
    setsockopt(fd, SOL_SOCKET, SO_RCVTIMEO, &tv, sizeof(tv));
    setsockopt(fd, SOL_SOCKET, SO_SNDTIMEO, &tv, sizeof(tv));

    int one = 1;
    setsockopt(fd, IPPROTO_TCP, TCP_NODELAY, &one, sizeof(one));
#ifdef SO_NOSIGPIPE
    setsockopt(fd, SOL_SOCKET, SO_NOSIGPIPE, &one, sizeof(one));
#endif
    return fd;
}

static int dex_smtp_start_tls(DexSmtpSock* s, const char* host) {
#ifdef DEX_SMTP_TLS
    SSL_CTX* ctx = dex_smtp_get_ctx();
    if (!ctx) {
        fprintf(stderr, "[smtp] could not create a TLS context\n");
        return -1;
    }
    SSL* ssl = SSL_new(ctx);
    if (!ssl) {
        fprintf(stderr, "[smtp] could not create a TLS connection\n");
        return -1;
    }
    SSL_set_fd(ssl, s->fd);
    SSL_set_tlsext_host_name(ssl, host);   // SNI: shared hosting needs it
    if (SSL_connect(ssl) <= 0) {
        fprintf(stderr, "[smtp] TLS handshake with %s failed\n", host);
        SSL_free(ssl);
        return -1;
    }
    long verdict = SSL_get_verify_result(ssl);
    if (verdict != X509_V_OK) {
        fprintf(stderr, "[smtp] warning: %s presented a certificate that does not verify (%s)\n",
                host, X509_verify_cert_error_string(verdict));
    }
    s->ssl = ssl;
    return 0;
#else
    (void)s; (void)host;
    fprintf(stderr, "[smtp] TLS needs OpenSSL and this build has none — "
                    "install it and rebuild, or use a host that does not require TLS\n");
    return -1;
#endif
}

static void dex_smtp_close(DexSmtpSock* s) {
#ifdef DEX_SMTP_TLS
    if (s->ssl) {
        SSL_shutdown((SSL*)s->ssl);
        SSL_free((SSL*)s->ssl);
    }
#endif
    s->ssl = NULL;
    if (s->fd >= 0) close(s->fd);
    s->fd = -1;
}

// --- Addresses, headers, capabilities ---

static int dex_smtp_ci_eq(const char* a, const char* b, size_t n) {
    for (size_t i = 0; i < n; i++) {
        unsigned char ca = (unsigned char)a[i], cb = (unsigned char)b[i];
        if (ca >= 'A' && ca <= 'Z') ca = (unsigned char)(ca - 'A' + 'a');
        if (cb >= 'A' && cb <= 'Z') cb = (unsigned char)(cb - 'A' + 'a');
        if (ca != cb) return 0;
    }
    return 1;
}

// True when the EHLO capability list has a line beginning with `keyword`.
// Line-anchored rather than a plain substring search, so a server naming a
// mechanism "XSTARTTLSFOO" cannot be read as offering STARTTLS.
static int dex_smtp_offers(const char* caps, const char* keyword) {
    size_t klen = strlen(keyword);
    for (const char* line = caps; line && *line; ) {
        if (dex_smtp_ci_eq(line, keyword, klen)) {
            char after = line[klen];
            if (after == '\0' || after == '\n' || after == ' ' || after == '\t' || after == '=') return 1;
        }
        const char* nl = strchr(line, '\n');
        if (!nl) break;
        line = nl + 1;
    }
    return 0;
}

// True when the AUTH line names this mechanism. `AUTH LOGIN PLAIN XOAUTH2` is
// one line of space-separated tokens, and a token must match whole: PLAIN must
// not be found inside XOAUTH2-PLAIN-ish names some servers invent.
static int dex_smtp_auth_offers(const char* caps, const char* mechanism) {
    size_t mlen = strlen(mechanism);
    for (const char* line = caps; line && *line; ) {
        if (dex_smtp_ci_eq(line, "AUTH", 4) && (line[4] == ' ' || line[4] == '=')) {
            const char* end = strchr(line, '\n');
            if (!end) end = line + strlen(line);
            const char* p = line + 4;
            while (p < end) {
                while (p < end && (*p == ' ' || *p == '\t' || *p == '=')) p++;
                const char* tok = p;
                while (p < end && *p != ' ' && *p != '\t') p++;
                if ((size_t)(p - tok) == mlen && dex_smtp_ci_eq(tok, mechanism, mlen)) return 1;
            }
        }
        const char* nl = strchr(line, '\n');
        if (!nl) break;
        line = nl + 1;
    }
    return 0;
}

// The bare address out of either "you@example.com" or "Name <you@example.com>".
// The envelope takes only the address; the display name belongs in the header.
static void dex_smtp_addr(const char* value, char* out, size_t out_size) {
    const char* start = value;
    const char* end = value + strlen(value);
    const char* open = strchr(value, '<');
    if (open) {
        start = open + 1;
        const char* close_bracket = strchr(start, '>');
        if (close_bracket) end = close_bracket;
    }
    while (start < end && (*start == ' ' || *start == '\t')) start++;
    while (end > start && (end[-1] == ' ' || end[-1] == '\t')) end--;

    size_t j = 0;
    for (const char* p = start; p < end && j + 1 < out_size; p++) {
        unsigned char c = (unsigned char)*p;
        // CR, LF and a stray '>' would let a caller's address text write its own
        // SMTP command. There is no legal address containing them.
        if (c == '\r' || c == '\n' || c == '<' || c == '>' || c < 0x20) continue;
        out[j++] = (char)c;
    }
    out[j] = '\0';
}

// A header value with the line breaks taken out. A subject carrying a CRLF
// would otherwise inject a header of the caller's choosing — a Bcc, say —
// into a message the caller only got to name the subject of.
static void dex_smtp_header_value(const char* in, char* out, size_t out_size) {
    size_t j = 0;
    for (size_t i = 0; in[i] && j + 1 < out_size; i++) {
        unsigned char c = (unsigned char)in[i];
        if (c == '\r' || c == '\n') continue;
        out[j++] = (char)c;
    }
    out[j] = '\0';
}

static int dex_smtp_is_ascii(const char* s) {
    for (size_t i = 0; s[i]; i++) {
        if ((unsigned char)s[i] >= 0x80) return 0;
    }
    return 1;
}

// RFC 2047 encoded-words for a Subject that is not plain ASCII. Emitted in
// chunks because an encoded-word may not exceed 75 characters, and the chunk
// boundary is only ever taken between UTF-8 sequences — splitting one produces
// two words that each decode to mojibake.
static char* dex_smtp_encode_subject(const char* subject) {
    size_t n = strlen(subject);
    // Each 30-byte chunk becomes at most 40 base64 characters inside a 12-byte
    // wrapper, plus 3 for the folding "\r\n ". Rounded up generously.
    size_t cap = (n / 30 + 2) * 64 + 16;
    char* out = (char*)malloc(cap);
    if (!out) return NULL;

    size_t used = 0;
    size_t i = 0;
    while (i < n) {
        size_t take = (n - i > 30) ? 30 : (n - i);
        // Back off to the start of the UTF-8 sequence we would have split.
        while (take > 0 && i + take < n && ((unsigned char)subject[i + take] & 0xC0) == 0x80) take--;
        if (take == 0) take = (n - i > 30) ? 30 : (n - i);   // not UTF-8; chunk anyway

        char encoded[64];
        dex_base64_encode((const unsigned char*)subject + i, take, encoded);
        int wrote = snprintf(out + used, cap - used, "%s=?UTF-8?B?%s?=",
                             used ? "\r\n " : "", encoded);
        if (wrote < 0 || (size_t)wrote >= cap - used) { free(out); return NULL; }
        used += (size_t)wrote;
        i += take;
    }
    if (used == 0) out[0] = '\0';
    return out;
}

static void dex_smtp_date(char* out, size_t out_size) {
    // Spelled out rather than left to strftime, whose %a and %b follow the
    // locale: a process that has called setlocale would otherwise put French
    // day names in a header RFC 5322 says are these twenty-one letters.
    static const char* const days[] = {"Sun","Mon","Tue","Wed","Thu","Fri","Sat"};
    static const char* const months[] = {"Jan","Feb","Mar","Apr","May","Jun",
                                         "Jul","Aug","Sep","Oct","Nov","Dec"};
    time_t now = time(NULL);
    struct tm utc;
    gmtime_r(&now, &utc);
    snprintf(out, out_size, "%s, %02d %s %04d %02d:%02d:%02d +0000",
             days[utc.tm_wday], utc.tm_mday, months[utc.tm_mon], utc.tm_year + 1900,
             utc.tm_hour, utc.tm_min, utc.tm_sec);
}

// --- The message ---

// Headers, a blank line, the dot-stuffed body and the terminating ".".
// Returned malloc'd; the caller frees it on every path.
static char* dex_smtp_build_message(const char* from, const char* to, const char* subject,
                                    const char* body, const char* domain) {
    char date[64];
    dex_smtp_date(date, sizeof(date));

    char message_id[320];
    snprintf(message_id, sizeof(message_id), "%llx.%x.%x@%s",
             (unsigned long long)time(NULL), (unsigned)getpid(), (unsigned)rand(), domain);

    char* subject_header = NULL;
    if (dex_smtp_is_ascii(subject)) {
        size_t n = strlen(subject);
        subject_header = (char*)malloc(n + 1);
        if (!subject_header) return NULL;
        memcpy(subject_header, subject, n + 1);
    } else {
        subject_header = dex_smtp_encode_subject(subject);
        if (!subject_header) return NULL;
    }

    size_t body_len = strlen(body);
    // Worst case each body byte becomes two: a leading '.' is doubled, an LF
    // becomes CRLF. Plus the headers and the terminator.
    size_t cap = body_len * 2 + strlen(from) + strlen(to) + strlen(subject_header) + 512;
    char* out = (char*)malloc(cap);
    if (!out) { free(subject_header); return NULL; }

    int header_len = snprintf(out, cap,
        "From: %s\r\n"
        "To: %s\r\n"
        "Subject: %s\r\n"
        "Date: %s\r\n"
        "Message-ID: <%s>\r\n"
        "MIME-Version: 1.0\r\n"
        "Content-Type: text/plain; charset=UTF-8\r\n"
        "\r\n",
        from, to, subject_header, date, message_id);
    free(subject_header);
    if (header_len < 0 || (size_t)header_len >= cap) { free(out); return NULL; }

    char* p = out + header_len;
    int at_line_start = 1;
    for (size_t i = 0; i < body_len; i++) {
        char c = body[i];
        if (c == '\r') {
            if (i + 1 < body_len && body[i + 1] == '\n') i++;
            *p++ = '\r'; *p++ = '\n';
            at_line_start = 1;
            continue;
        }
        if (c == '\n') {
            *p++ = '\r'; *p++ = '\n';
            at_line_start = 1;
            continue;
        }
        // Transparency, RFC 5321 §4.5.2: a line the sender wrote as "." must
        // reach the server as ".." or it ends the message where it stands.
        if (at_line_start && c == '.') *p++ = '.';
        *p++ = c;
        at_line_start = 0;
    }
    if (!at_line_start) { *p++ = '\r'; *p++ = '\n'; }
    *p++ = '.'; *p++ = '\r'; *p++ = '\n';
    *p = '\0';
    return out;
}

// --- The conversation ---

_Bool dex_smtp_send(const char* host, int port,
                    const char* username, const char* password,
                    const char* from, const char* to,
                    const char* subject, const char* body) {
    if (!host || !*host) {
        fprintf(stderr, "[smtp] no host given\n");
        return 0;
    }
    if (!username) username = "";
    if (!password) password = "";
    if (!from) from = "";
    if (!to) to = "";
    if (!subject) subject = "";
    if (!body) body = "";
    if (port <= 0) port = 587;

    char from_addr[320], to_addr[320];
    dex_smtp_addr(from, from_addr, sizeof(from_addr));
    dex_smtp_addr(to, to_addr, sizeof(to_addr));
    if (!from_addr[0] || !to_addr[0]) {
        fprintf(stderr, "[smtp] need both a sender and a recipient address\n");
        return 0;
    }

    // The domain the envelope belongs to: what EHLO announces and what the
    // Message-ID is scoped by. Taken from the sender rather than from the local
    // hostname, which on a container is a hex string no receiver has heard of.
    char domain[256];
    const char* at = strchr(from_addr, '@');
    snprintf(domain, sizeof(domain), "%s", (at && at[1]) ? at + 1 : "localhost");

    char from_header[512], to_header[512], subject_header[998];
    dex_smtp_header_value(from, from_header, sizeof(from_header));
    dex_smtp_header_value(to, to_header, sizeof(to_header));
    dex_smtp_header_value(subject, subject_header, sizeof(subject_header));

    DexSmtpSock s;
    s.fd = -1;
    s.ssl = NULL;
    s.len = 0;
    s.pos = 0;
    s.last[0] = '\0';

    _Bool ok = 0;
    char caps[4096];
    char ehlo[320];
    char cmd[1024];
    char* message = NULL;
    char* auth = NULL;
    int code = -1;

    s.fd = dex_smtp_dial(host, port);
    if (s.fd < 0) {
        fprintf(stderr, "[smtp] cannot reach %s:%d\n", host, port);
        return 0;
    }

    // Port 465 is TLS from the first byte — the greeting itself is encrypted.
    // Everything else starts in the clear and is upgraded with STARTTLS below.
    if (port == DEX_SMTP_IMPLICIT_TLS_PORT) {
        if (dex_smtp_start_tls(&s, host) < 0) goto done;
    }

    code = dex_smtp_read_reply(&s, NULL, 0);
    if (code != 220) {
        fprintf(stderr, "[smtp] %s:%d did not greet: %d %s\n", host, port, code, s.last);
        goto done;
    }

    snprintf(ehlo, sizeof(ehlo), "EHLO %s\r\n", domain);
    code = dex_smtp_cmd(&s, ehlo, caps, sizeof(caps));
    if (code != 250) {
        fprintf(stderr, "[smtp] EHLO refused: %d %s\n", code, s.last);
        goto done;
    }

    if (!s.ssl && dex_smtp_offers(caps, "STARTTLS")) {
        code = dex_smtp_cmd(&s, "STARTTLS\r\n", NULL, 0);
        if (code != 220) {
            fprintf(stderr, "[smtp] STARTTLS refused: %d %s\n", code, s.last);
            goto done;
        }
        // Nothing may be buffered here. Bytes sent before the handshake but
        // read after it would be an injection the TLS session then vouches for
        // (CVE-2011-0411), so a server that pipelined into the upgrade is
        // hung up on rather than trusted.
        if (s.pos != s.len) {
            fprintf(stderr, "[smtp] %s sent data before the TLS handshake; refusing\n", host);
            goto done;
        }
        if (dex_smtp_start_tls(&s, host) < 0) goto done;

        // The capability list from before the handshake cannot be trusted and
        // is generally different anyway — AUTH usually appears only now.
        code = dex_smtp_cmd(&s, ehlo, caps, sizeof(caps));
        if (code != 250) {
            fprintf(stderr, "[smtp] EHLO after STARTTLS refused: %d %s\n", code, s.last);
            goto done;
        }
    }

    if (username[0]) {
        if (!s.ssl) {
            fprintf(stderr, "[smtp] %s:%d offered no TLS; refusing to send the password "
                            "in the clear. Use port 465, or a host that offers STARTTLS.\n",
                    host, port);
            goto done;
        }
        // LOGIN unless the server advertises PLAIN and not LOGIN. Both carry
        // the same secret in the same base64; which one is on offer is the only
        // thing that decides it.
        int use_plain = !dex_smtp_auth_offers(caps, "LOGIN") && dex_smtp_auth_offers(caps, "PLAIN");

        if (use_plain) {
            size_t ulen = strlen(username), plen = strlen(password);
            size_t raw_len = 1 + ulen + 1 + plen;
            unsigned char* raw = (unsigned char*)malloc(raw_len);
            if (!raw) goto done;
            raw[0] = 0;
            memcpy(raw + 1, username, ulen);
            raw[1 + ulen] = 0;
            memcpy(raw + 2 + ulen, password, plen);

            auth = (char*)malloc(4 * ((raw_len + 2) / 3) + 32);
            if (!auth) { free(raw); goto done; }
            memcpy(auth, "AUTH PLAIN ", 11);
            dex_base64_encode(raw, raw_len, auth + 11);
            free(raw);
            strcat(auth, "\r\n");

            code = dex_smtp_cmd(&s, auth, NULL, 0);
            free(auth);
            auth = NULL;
            if (code != 235) {
                fprintf(stderr, "[smtp] AUTH PLAIN refused: %d %s\n", code, s.last);
                goto done;
            }
        } else {
            code = dex_smtp_cmd(&s, "AUTH LOGIN\r\n", NULL, 0);
            if (code != 334) {
                fprintf(stderr, "[smtp] AUTH LOGIN refused: %d %s\n", code, s.last);
                goto done;
            }
            for (int step = 0; step < 2; step++) {
                const char* secret = step == 0 ? username : password;
                size_t slen = strlen(secret);
                auth = (char*)malloc(4 * ((slen + 2) / 3) + 8);
                if (!auth) goto done;
                dex_base64_encode((const unsigned char*)secret, slen, auth);
                strcat(auth, "\r\n");
                code = dex_smtp_cmd(&s, auth, NULL, 0);
                free(auth);
                auth = NULL;
                // 334 asks for the next field, 235 accepts the pair.
                if (code != (step == 0 ? 334 : 235)) {
                    fprintf(stderr, "[smtp] %s rejected: %d %s\n",
                            step == 0 ? "username" : "password", code, s.last);
                    goto done;
                }
            }
        }
    }

    snprintf(cmd, sizeof(cmd), "MAIL FROM:<%s>\r\n", from_addr);
    code = dex_smtp_cmd(&s, cmd, NULL, 0);
    if (code != 250) {
        fprintf(stderr, "[smtp] sender %s refused: %d %s\n", from_addr, code, s.last);
        goto done;
    }

    snprintf(cmd, sizeof(cmd), "RCPT TO:<%s>\r\n", to_addr);
    code = dex_smtp_cmd(&s, cmd, NULL, 0);
    // 251 is "not local, will forward" — an acceptance, not a refusal.
    if (code != 250 && code != 251) {
        fprintf(stderr, "[smtp] recipient %s refused: %d %s\n", to_addr, code, s.last);
        goto done;
    }

    code = dex_smtp_cmd(&s, "DATA\r\n", NULL, 0);
    if (code != 354) {
        fprintf(stderr, "[smtp] DATA refused: %d %s\n", code, s.last);
        goto done;
    }

    message = dex_smtp_build_message(from_header, to_header, subject_header, body, domain);
    if (!message) {
        fprintf(stderr, "[smtp] out of memory building the message\n");
        goto done;
    }
    if (dex_smtp_write(&s, message, strlen(message)) < 0) {
        fprintf(stderr, "[smtp] connection dropped while sending the message\n");
        goto done;
    }

    // The reply to the terminating "." is the only thing that means accepted.
    code = dex_smtp_read_reply(&s, NULL, 0);
    if (code != 250) {
        fprintf(stderr, "[smtp] message refused: %d %s\n", code, s.last);
        goto done;
    }
    ok = 1;

done:
    free(message);
    free(auth);
    if (s.fd >= 0) {
        // Sent whatever happened: a server told the session is over releases it
        // now instead of waiting out its idle timeout. Its reply does not
        // matter and is not waited for beyond the socket timeout.
        dex_smtp_cmd(&s, "QUIT\r\n", NULL, 0);
        dex_smtp_close(&s);
    }
    return ok;
}
