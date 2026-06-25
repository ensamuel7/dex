// Optional type wrappers for value types
typedef struct { _Bool has_value; int value; } DexOptInt;
typedef struct { _Bool has_value; _Bool value; } DexOptBool;
typedef struct { _Bool has_value; long value; } DexOptLong;
typedef struct { _Bool has_value; double value; } DexOptDouble;
typedef struct { _Bool has_value; unsigned char value; } DexOptChar;
