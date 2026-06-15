#include <stdio.h>

typedef struct {
    int x, y, z;
} Vec3;

Vec3 add_vecs(Vec3 a, Vec3 b) {
    Vec3 result = { a.x + b.x, a.y + b.y, a.z + b.z };
    return result;
}

int main() {
    Vec3 acc = { 0, 0, 0 };
    for (int i = 0; i < 10000000; i++) {
        Vec3 v = { 1, 2, 3 };
        acc = add_vecs(acc, v);
    }
    printf("%d\n%d\n%d\n", acc.x, acc.y, acc.z);
    return 0;
}
