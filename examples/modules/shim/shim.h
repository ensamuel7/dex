#ifndef SHIM_H
#define SHIM_H

static inline int shimc_half(int n) {
    return n % 2 == 0 ? n / 2 : -1;
}

#endif
