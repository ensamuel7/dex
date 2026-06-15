#include <stdio.h>
#include <stdlib.h>
#include <string.h>

int main() {
    int count = 100000;
    char *s = malloc(1);
    s[0] = '\0';
    for (int i = 0; i < count; i++) {
        size_t la = strlen(s), lb = 1;
        char *new_s = malloc(la + lb + 1);
        memcpy(new_s, s, la);
        new_s[la] = 'a';
        new_s[la + 1] = '\0';
        free(s);
        s = new_s;
    }
    printf("%d\n", count);
    free(s);
    return 0;
}
