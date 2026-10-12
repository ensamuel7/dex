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

// Extract one key's value from a field tag.
//
// The format is the one Go uses: space-separated `key:"value"` pairs, so a
// single tag can carry several namespaces — `db:"i.total_minor" json:"total"`.
// Values are taken verbatim; a tag is raw text, so a value cannot itself
// contain a double quote and none of the usual escaping applies.
//
// Writes the value into out (always NUL-terminated) and returns its length, or
// 0 when the key is absent — which is indistinguishable from an empty value,
// and deliberately so: a caller that cares treats both as "nothing specified".
static int dex_refl_tag(const char* tag, const char* key, char* out, int outCap) {
    if (outCap > 0) out[0] = '\0';
    if (!tag || !key || outCap <= 0) return 0;
    int keyLen = 0;
    while (key[keyLen]) keyLen++;
    if (keyLen == 0) return 0;

    int i = 0;
    while (tag[i]) {
        while (tag[i] == ' ' || tag[i] == '\t') i++;
        if (!tag[i]) break;
        // Key runs up to the colon.
        int keyStart = i;
        while (tag[i] && tag[i] != ':' && tag[i] != ' ') i++;
        int thisKeyLen = i - keyStart;
        if (tag[i] != ':' || tag[i + 1] != '"') {
            // Not a well-formed pair; skip to the next space rather than
            // guessing what was meant.
            while (tag[i] && tag[i] != ' ') i++;
            continue;
        }
        i += 2; // consume :"
        int valStart = i;
        while (tag[i] && tag[i] != '"') i++;
        int valLen = i - valStart;
        if (tag[i] == '"') i++;

        int match = thisKeyLen == keyLen;
        if (match) {
            for (int k = 0; k < keyLen; k++) {
                if (tag[keyStart + k] != key[k]) { match = 0; break; }
            }
        }
        if (match) {
            int n = valLen < outCap - 1 ? valLen : outCap - 1;
            for (int k = 0; k < n; k++) out[k] = tag[valStart + k];
            out[n] = '\0';
            return n;
        }
    }
    return 0;
}
