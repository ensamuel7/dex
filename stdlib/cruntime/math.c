
#include <math.h>

// Constants
double dex_math_pi() {
    return 3.14159265358979323846;
}

double dex_math_e() {
    return 2.71828182845904523536;
}

// Trigonometric
double dex_math_sin(double x) {
    return sin(x);
}

double dex_math_cos(double x) {
    return cos(x);
}

double dex_math_tan(double x) {
    return tan(x);
}

double dex_math_asin(double x) {
    return asin(x);
}

double dex_math_acos(double x) {
    return acos(x);
}

double dex_math_atan(double x) {
    return atan(x);
}

// Power / root
double dex_math_sqrt(double x) {
    return sqrt(x);
}

double dex_math_pow(double base, double exp) {
    return pow(base, exp);
}

double dex_math_exp(double x) {
    return exp(x);
}

// Rounding
double dex_math_floor(double x) {
    return floor(x);
}

double dex_math_ceil(double x) {
    return ceil(x);
}

double dex_math_round(double x) {
    return round(x);
}

// Other
double dex_math_abs(double x) {
    return fabs(x);
}

double dex_math_log(double x) {
    return log(x);
}

double dex_math_log2(double x) {
    return log2(x);
}

double dex_math_log10(double x) {
    return log10(x);
}

// Two-arg
double dex_math_min(double a, double b) {
    return (a < b) ? a : b;
}

double dex_math_max(double a, double b) {
    return (a > b) ? a : b;
}
