// An opaque C pointer, boxed so it can be refcounted.
//
// The pointer itself gets no header: it came from a C library and may not be
// one we allocated. The box is an ordinary Dex object, so it is pooled,
// released and swept exactly like every other, and the cleanup stack closes
// one when an exception unwinds past the frame holding it.
//
// The closing function lives in the box's destroy hook, so one struct serves
// every handle type. destroy releases the contents; the runtime frees the box
// (see dex_release).
typedef struct {
    DexObjHeader hdr;
    void* ptr;
} DexHandle;

// Boxes a pointer, or yields NULL when the C function failed. NULL is the
// absent value for a handle, so an `open` that returns one is naturally an
// optional rather than a box wrapping nothing.
static DexHandle* dex_handle_new(void* p, void (*close)(void*)) {
    if (!p) return NULL;
    DexHandle* h = (DexHandle*)dex_obj_alloc(sizeof(DexHandle), close);
    h->ptr = p;
    return h;
}

// Unboxes for a call into C. A NULL box yields a NULL pointer rather than
// faulting, so passing an absent handle reaches the C function as NULL — which
// is what that function already has to handle.
static DexHandle* dex_handle_require(DexHandle* h, const char* fn) {
    if (!h) {
        fprintf(stderr, "runtime error: %s returned NULL for a handle declared non-optional\n", fn);
        exit(1);
    }
    return h;
}

static void* dex_handle_ptr(DexHandle* h) {
    return h ? h->ptr : NULL;
}
