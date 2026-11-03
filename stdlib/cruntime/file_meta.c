
#include <sys/stat.h>
#include <sys/file.h>
#include <fcntl.h>
#include <dirent.h>
#include <errno.h>

/* Ownership, permissions, links, recursive removal, atomic writes and locks:
 * what a program managing a host's files needs beyond reading and writing them.
 *
 * Modes cross as octal strings ("0644", "755"), because Dex has no octal
 * literal and 420 for 0644 is a mistake waiting in every call site. Anything
 * that is not 1-4 octal digits is refused rather than guessed at.
 *
 * Nothing here follows a symbolic link it is pointed at: chown is lchown, and
 * mode, uid, gid, isDir and isLink describe the link itself. A program running
 * as root that chowns a path inside a user's directory must not be steered
 * through a link the user planted to a file it never meant to touch. */

static int dex_file_parse_mode(const char* s, mode_t* out) {
    if (!s) return 0;
    size_t n = strlen(s);
    if (n == 0 || n > 4) return 0;
    mode_t m = 0;
    for (size_t i = 0; i < n; i++) {
        if (s[i] < '0' || s[i] > '7') return 0;
        m = (mode_t)(m * 8 + (mode_t)(s[i] - '0'));
    }
    *out = m;
    return 1;
}

_Bool dex_file_chmod(const char* path, const char* mode) {
    mode_t m;
    if (!dex_file_parse_mode(mode, &m)) return 0;
    return chmod(path, m) == 0;
}

_Bool dex_file_chown(const char* path, int uid, int gid) {
    return lchown(path, (uid_t)uid, (gid_t)gid) == 0;
}

char* dex_file_mode(const char* path) {
    struct stat st;
    if (lstat(path, &st) != 0) return dex_xstrdup("");
    char buf[8];
    snprintf(buf, sizeof(buf), "%04o", (unsigned)(st.st_mode & 07777));
    return dex_xstrdup(buf);
}

int dex_file_uid(const char* path) {
    struct stat st;
    if (lstat(path, &st) != 0) return -1;
    return (int)st.st_uid;
}

int dex_file_gid(const char* path) {
    struct stat st;
    if (lstat(path, &st) != 0) return -1;
    return (int)st.st_gid;
}

_Bool dex_file_is_dir(const char* path) {
    struct stat st;
    return lstat(path, &st) == 0 && S_ISDIR(st.st_mode);
}

_Bool dex_file_is_link(const char* path) {
    struct stat st;
    return lstat(path, &st) == 0 && S_ISLNK(st.st_mode);
}

_Bool dex_file_symlink(const char* target, const char* link) {
    return symlink(target, link) == 0;
}

char* dex_file_readlink(const char* path) {
    char buf[4096];
    ssize_t n = readlink(path, buf, sizeof(buf) - 1);
    if (n < 0) return dex_xstrdup("");
    buf[n] = '\0';
    return dex_xstrdup(buf);
}

/* Empties the directory open as fd, depth first, by names relative to it:
 * openat with O_NOFOLLOW never steps through a link, and a link is unlinked
 * as the entry it is. nftw would do the walk, but glibc declares it only
 * under _XOPEN_SOURCE, which the generated C does not define. */
static int dex_file_empty_dir(int fd, int depth) {
    if (depth > 256) { close(fd); return 0; }
    DIR* d = fdopendir(fd);
    if (!d) { close(fd); return 0; }
    int ok = 1;
    struct dirent* e;
    while ((e = readdir(d)) != NULL) {
        const char* n = e->d_name;
        if (strcmp(n, ".") == 0 || strcmp(n, "..") == 0) continue;
        struct stat st;
        if (fstatat(dirfd(d), n, &st, AT_SYMLINK_NOFOLLOW) != 0) { ok = 0; continue; }
        if (S_ISDIR(st.st_mode)) {
            int sub = openat(dirfd(d), n, O_RDONLY | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC);
            if (sub < 0 || !dex_file_empty_dir(sub, depth + 1)) { ok = 0; continue; }
            if (unlinkat(dirfd(d), n, AT_REMOVEDIR) != 0) ok = 0;
        } else if (unlinkat(dirfd(d), n, 0) != 0) {
            ok = 0;
        }
    }
    closedir(d);
    return ok;
}

/* A file, a link or a whole tree, never following a link inside it. "" and
 * "/" are refused outright -- a path built from an empty variable must not
 * empty the machine. */
_Bool dex_file_remove_all(const char* path) {
    if (!path || path[0] == '\0' || strcmp(path, "/") == 0) return 0;
    struct stat st;
    if (lstat(path, &st) != 0) return errno == ENOENT;
    if (!S_ISDIR(st.st_mode)) return unlink(path) == 0;
    int fd = open(path, O_RDONLY | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC);
    if (fd < 0 || !dex_file_empty_dir(fd, 0)) return 0;
    return rmdir(path) == 0;
}

/* Written beside the destination and renamed over it, so a reader sees the old
 * contents or the new, never half of either -- the way a config a daemon
 * reloads must be written. The mode is set before the rename, so the file is
 * never visible with the wrong permissions. Binary-safe. */
_Bool dex_file_write_atomic(const char* path, DexString* content, const char* mode) {
    mode_t m;
    if (!dex_file_parse_mode(mode, &m)) return 0;
    size_t plen = strlen(path);
    char* tmp = (char*)dex_xmalloc(plen + 12);
    if (!tmp) return 0;
    memcpy(tmp, path, plen);
    memcpy(tmp + plen, ".tmpXXXXXX", 11);
    int fd = mkstemp(tmp);
    if (fd < 0) { free(tmp); return 0; }

    int ok = 1;
    size_t len = content ? content->len : 0;
    size_t off = 0;
    while (ok && off < len) {
        ssize_t w = write(fd, content->data + off, len - off);
        if (w < 0) { if (errno != EINTR) ok = 0; continue; }
        off += (size_t)w;
    }
    if (ok && fchmod(fd, m) != 0) ok = 0;
    if (ok && fsync(fd) != 0) ok = 0;
    if (close(fd) != 0) ok = 0;
    if (ok && rename(tmp, path) != 0) ok = 0;
    if (!ok) unlink(tmp);
    free(tmp);
    return ok;
}

/* An exclusive advisory lock on path, created if missing, held until unlock.
 * Blocks until it is free. Returns the descriptor that holds it, or -1. */
int dex_file_lock(const char* path) {
    int fd = open(path, O_RDWR | O_CREAT | O_CLOEXEC, 0600);
    if (fd < 0) return -1;
    while (flock(fd, LOCK_EX) != 0) {
        if (errno != EINTR) { close(fd); return -1; }
    }
    return fd;
}

_Bool dex_file_unlock(int fd) {
    if (fd < 0) return 0;
    flock(fd, LOCK_UN);
    return close(fd) == 0;
}
