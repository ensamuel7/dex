
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <ctype.h>
#include <unistd.h>
#include <termios.h>

static void dex_io_flush_line(void) {
    int ch;
    while ((ch = getchar()) != '\n' && ch != EOF) {}
}

static void dex_io_prompt(const char* msg) {
    printf("%s", msg);
    fflush(stdout);
}

// Interactive line reader with arrow key support (used when stdin is a terminal)
static char* dex_io_read_line_interactive(void) {
    struct termios orig, raw;
    tcgetattr(STDIN_FILENO, &orig);
    raw = orig;
    raw.c_lflag &= ~(ICANON | ECHO);
    raw.c_cc[VMIN] = 1;
    raw.c_cc[VTIME] = 0;
    tcsetattr(STDIN_FILENO, TCSAFLUSH, &raw);

    size_t cap = 256;
    size_t len = 0;
    size_t cursor = 0;
    char* buf = (char*)malloc(cap);
    buf[0] = '\0';

    while (1) {
        char c;
        if (read(STDIN_FILENO, &c, 1) != 1) break;

        if (c == '\n' || c == '\r') {
            write(STDOUT_FILENO, "\n", 1);
            break;
        }

        if (c == 127 || c == 8) { // Backspace
            if (cursor > 0) {
                memmove(buf + cursor - 1, buf + cursor, len - cursor);
                cursor--;
                len--;
                buf[len] = '\0';
                // Redraw: move back, write rest of line, clear trailing char, reposition
                write(STDOUT_FILENO, "\b", 1);
                write(STDOUT_FILENO, buf + cursor, len - cursor);
                write(STDOUT_FILENO, " \b", 2);
                for (size_t i = 0; i < len - cursor; i++)
                    write(STDOUT_FILENO, "\b", 1);
            }
            continue;
        }

        if (c == 27) { // Escape sequence
            char seq[3];
            if (read(STDIN_FILENO, &seq[0], 1) != 1) continue;
            if (seq[0] != '[') continue;
            if (read(STDIN_FILENO, &seq[1], 1) != 1) continue;

            if (seq[1] == 'D' && cursor > 0) { // Left arrow
                cursor--;
                write(STDOUT_FILENO, "\033[D", 3);
            } else if (seq[1] == 'C' && cursor < len) { // Right arrow
                cursor++;
                write(STDOUT_FILENO, "\033[C", 3);
            } else if (seq[1] == 'H') { // Home
                while (cursor > 0) {
                    cursor--;
                    write(STDOUT_FILENO, "\033[D", 3);
                }
            } else if (seq[1] == 'F') { // End
                while (cursor < len) {
                    cursor++;
                    write(STDOUT_FILENO, "\033[C", 3);
                }
            } else if (seq[1] == '3') { // Delete key (ESC [ 3 ~)
                char extra;
                if (read(STDIN_FILENO, &extra, 1) == 1 && extra == '~') {
                    if (cursor < len) {
                        memmove(buf + cursor, buf + cursor + 1, len - cursor - 1);
                        len--;
                        buf[len] = '\0';
                        write(STDOUT_FILENO, buf + cursor, len - cursor);
                        write(STDOUT_FILENO, " \b", 2);
                        for (size_t i = 0; i < len - cursor; i++)
                            write(STDOUT_FILENO, "\b", 1);
                    }
                }
            }
            continue;
        }

        if (c == 1) { // Ctrl+A — Home
            while (cursor > 0) {
                cursor--;
                write(STDOUT_FILENO, "\033[D", 3);
            }
            continue;
        }

        if (c == 5) { // Ctrl+E — End
            while (cursor < len) {
                cursor++;
                write(STDOUT_FILENO, "\033[C", 3);
            }
            continue;
        }

        if (c == 21) { // Ctrl+U — Kill line before cursor
            if (cursor > 0) {
                // Move to beginning visually
                for (size_t i = 0; i < cursor; i++)
                    write(STDOUT_FILENO, "\b", 1);
                // Shift remaining text to front
                memmove(buf, buf + cursor, len - cursor);
                len -= cursor;
                buf[len] = '\0';
                cursor = 0;
                // Rewrite remaining text and clear leftover chars
                write(STDOUT_FILENO, buf, len);
                write(STDOUT_FILENO, "\033[K", 3); // clear to end of line
                // Reposition cursor at beginning
                for (size_t i = 0; i < len; i++)
                    write(STDOUT_FILENO, "\b", 1);
            }
            continue;
        }

        if (c == 4) { // Ctrl+D — EOF on empty line
            if (len == 0) {
                free(buf);
                tcsetattr(STDIN_FILENO, TCSAFLUSH, &orig);
                return strdup("");
            }
            continue;
        }

        if (c == 3) { // Ctrl+C
            write(STDOUT_FILENO, "\n", 1);
            tcsetattr(STDIN_FILENO, TCSAFLUSH, &orig);
            free(buf);
            exit(130);
        }

        // Printable character — insert at cursor
        if ((unsigned char)c >= 32) {
            if (len + 1 >= cap) {
                cap *= 2;
                buf = (char*)realloc(buf, cap);
            }
            memmove(buf + cursor + 1, buf + cursor, len - cursor);
            buf[cursor] = c;
            cursor++;
            len++;
            buf[len] = '\0';
            // Write from cursor to end, then reposition
            write(STDOUT_FILENO, buf + cursor - 1, len - cursor + 1);
            for (size_t i = 0; i < len - cursor; i++)
                write(STDOUT_FILENO, "\b", 1);
        }
    }

    tcsetattr(STDIN_FILENO, TCSAFLUSH, &orig);
    return buf;
}

static char* dex_io_read_line(void) {
    fflush(stdout);

    // Use interactive line editor when stdin is a terminal
    if (isatty(STDIN_FILENO)) {
        return dex_io_read_line_interactive();
    }

    // Non-interactive: use basic getline
    char* line = NULL;
    size_t len = 0;
    ssize_t nread = getline(&line, &len, stdin);
    if (nread == -1) {
        free(line);
        return strdup("");
    }
    if (nread > 0 && line[nread - 1] == '\n') line[--nread] = '\0';
    if (nread > 0 && line[nread - 1] == '\r') line[--nread] = '\0';
    return line;
}

static int dex_io_read_int(void) {
    fflush(stdout);
    int value = 0;
    scanf("%d", &value);
    dex_io_flush_line();
    return value;
}

static double dex_io_read_double(void) {
    fflush(stdout);
    double value = 0.0;
    scanf("%lf", &value);
    dex_io_flush_line();
    return value;
}

static int dex_io_read_bool(void) {
    fflush(stdout);
    char buf[16];
    if (scanf("%15s", buf) != 1) {
        dex_io_flush_line();
        return 0;
    }
    dex_io_flush_line();
    for (int i = 0; buf[i]; i++) {
        buf[i] = tolower((unsigned char)buf[i]);
    }
    return strcmp(buf, "true") == 0;
}
