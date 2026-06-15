#include <stdio.h>

long fib(int n) {
    long a = 0, b = 1;
    for (int i = 0; i < n; i++) {
        long temp = a + b;
        a = b;
        b = temp;
    }
    return a;
}

int main() {
    long result = 0;
    for (int i = 0; i < 10000000; i++) {
        result = fib(50);
    }
    printf("%ld\n", result);
    return 0;
}
