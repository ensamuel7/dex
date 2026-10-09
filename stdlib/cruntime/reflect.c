// Reflection runtime.
//
// Deliberately almost empty. The metadata codegen emits for a reflected struct
// is two arrays of string literals plus a set of accessor functions built from
// a switch on the field index, so reading a field through reflection compiles
// to the same load a field access compiles to — there is no offset arithmetic
// and no reinterpreting of bytes for this file to perform.
//
// What is left is the one operation that genuinely needs a loop: turning a
// field name chosen at runtime into an index. When the name is a literal the
// compiler folds it and this never runs.

// Resolve a field name to its index, or -1 if the struct has no such field.
static int dex_refl_index_of(const char* name, int count, const char* const* names) {
    if (!name) return -1;
    for (int i = 0; i < count; i++) {
        const char* a = names[i];
        const char* b = name;
        while (*a && *a == *b) { a++; b++; }
        if (*a == '\0' && *b == '\0') return i;
    }
    return -1;
}
