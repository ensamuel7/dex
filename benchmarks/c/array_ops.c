#include <stdio.h>
#include <stdlib.h>

int main() {
    int cap = 8;
    int size = 0;
    int *arr = malloc(cap * sizeof(int));
    for (int i = 0; i < 1000000; i++) {
        if (size == cap) {
            cap *= 2;
            arr = realloc(arr, cap * sizeof(int));
        }
        arr[size++] = i;
    }
    long sum = 0;
    for (int i = 0; i < size; i++) {
        sum += arr[i];
    }
    printf("%ld\n", sum);
    free(arr);
    return 0;
}
