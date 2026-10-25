// DexLang exception runtime — setjmp/longjmp-based exception handling

#include <setjmp.h>

/* _setjmp / _longjmp do not save or restore the signal mask. On macOS and the
   BSDs plain setjmp() is sigsetjmp(env, 1), so every try entry pays a
   sigprocmask syscall -- measured at ~180ns per entry, which is 45x what a
   table-driven implementation costs. Nothing in this runtime installs a signal
   handler or depends on the mask surviving an unwind, so saving it is pure
   waste. On glibc setjmp already omits the mask, so this is a no-op there.
   _setjmp/_longjmp are POSIX. MinGW spells those names for something else, so
   Windows keeps the plain pair -- where it costs nothing anyway. */
#if defined(_WIN32)
#define DEX_SETJMP(env)  setjmp(env)
#define DEX_LONGJMP(env) longjmp(env, 1)
#else
#define DEX_SETJMP(env)  _setjmp(env)
#define DEX_LONGJMP(env) _longjmp(env, 1)
#endif
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#define DEX_MAX_EXCEPTION_DEPTH 64
#define DEX_EXCEPTION_MESSAGE_MAX 512
#define DEX_EXCEPTION_PAYLOAD_MAX 256

typedef struct {
    jmp_buf env;
    const char* message;
    // The thrown struct's name, always a string literal in the generated code,
    // so it needs no owner across the unwind. This is what a typed catch
    // matches on: there is no inheritance to walk, only a name to compare.
    const char* type;
    int active;
} DexExceptionFrame;

static __thread DexExceptionFrame _dex_exc_stack[DEX_MAX_EXCEPTION_DEPTH];
static __thread int _dex_exc_top = -1;

// A thrown message built at runtime — `throw Exception("no " + name)` — is a
// DexString nothing can own across the throw: longjmp skips every release the
// unwound frames would have run, and the catch site only reads the text.
//
// So the text is copied here first, the thrower releases its string, and the
// longjmp happens afterwards. Staging is a separate call from the throw for
// exactly that reason: there is no way to release anything after a longjmp.
//
// Bounded, and truncation is preferable to the alternatives: an allocation here
// would need an owner across the unwind, which is the problem being solved.
static __thread char _dex_exc_msg[DEX_EXCEPTION_MESSAGE_MAX];

// The cleanup stack: what has to be released if an exception unwinds past it.
//
// A throw is a longjmp, so every frame between the throw and the catch is
// skipped — its heap locals are never released and the statement temporaries
// it was in the middle of are never freed. Measured at two objects per unwound
// frame per throw, which on an error path taken in a loop accumulates.
//
// So each heap value that a frame would release on its way out registers here
// as well, and each try records the height of this stack at its setjmp. When
// the longjmp lands, everything registered above that height belongs to a
// frame that no longer exists, and the runtime releases it.
//
// Entries hold the pointer rather than the address of the variable holding it:
// the unwind runs in the catching frame, whose own calls overwrite the stack
// memory the dead frames occupied, so reading a dead frame's slot would be a
// use-after-free.
//
// Holding the pointer means a value that gets replaced has to say so, since the
// write that replaces it also releases it. _dex_cleanup_push therefore hands
// back the slot it took, and _dex_cleanup_set points that slot at the new
// value — one per reassignable local, and one per written heap field of a
// struct local. A value written once needs no slot and takes none.
#define DEX_CLEANUP_MAX 4096
static __thread void* _dex_cleanup_stack[DEX_CLEANUP_MAX];
static __thread size_t _dex_cleanup_top = 0;

// Registering is only worth anything when something above can catch: with no
// exception frame live, a throw exits the process and nothing unwinds. That
// check is what keeps this off the cost of ordinary code — a load and a branch,
// no stores — and it is safe because a frame that can be unwound was entered
// with its catcher's frame already pushed.
#define DEX_CLEANUP_NO_SLOT ((size_t)-1)

static size_t _dex_cleanup_push(void* obj) {
    if (_dex_exc_top < 0) {
        return DEX_CLEANUP_NO_SLOT;
    }
    if (_dex_cleanup_top >= DEX_CLEANUP_MAX) {
        // Degrades to the old behaviour — the object leaks on an unwind —
        // rather than overrunning the stack.
        return DEX_CLEANUP_NO_SLOT;
    }
    // A null is registered as readily as anything else: an optional starts out
    // empty and is assigned later, and the slot has to exist by then.
    _dex_cleanup_stack[_dex_cleanup_top] = obj;
    return _dex_cleanup_top++;
}

// Points an existing entry at the value its variable now holds. Called after an
// assignment, which released what was there before.
static void _dex_cleanup_set(size_t slot, void* obj) {
    if (slot < _dex_cleanup_top) {
        _dex_cleanup_stack[slot] = obj;
    }
}

// Drops the entries a scope registered without releasing them: on the normal
// path the scope's own releases already ran. Monotone on purpose — a restore
// can only lower the height, never raise it, so two restores in either order
// cannot bring a stale entry back into range.
static void _dex_cleanup_drop_to(size_t mark) {
    if (_dex_cleanup_top > mark) {
        _dex_cleanup_top = mark;
    }
}

// Releases everything registered above a try's recorded height. Called on the
// catch path only: on the normal path each value is released by the scope that
// owns it, and the height is restored without releasing anything.
static void _dex_cleanup_unwind_to(size_t mark) {
    while (_dex_cleanup_top > mark) {
        void* obj = _dex_cleanup_stack[--_dex_cleanup_top];
        if (obj) {
            dex_release(obj);
        }
    }
}

// Entering a try. The frame stack is a fixed array, so this is where running
// out of it is caught: a bounded exit naming the limit, rather than writing
// past the end of a thread-local array and corrupting whatever follows it.
static void _dex_exc_push_frame(void) {
    if (_dex_exc_top + 1 >= DEX_MAX_EXCEPTION_DEPTH) {
        fprintf(stderr, "Exception frames exhausted: more than %d nested try blocks\n",
                DEX_MAX_EXCEPTION_DEPTH);
        exit(1);
    }
    _dex_exc_top++;
}

static void _dex_exc_stage(const char* msg) {
    int n = 0;
    if (msg) {
        while (msg[n] && n + 1 < DEX_EXCEPTION_MESSAGE_MAX) {
            _dex_exc_msg[n] = msg[n];
            n++;
        }
    }
    _dex_exc_msg[n] = '\0';
}

// An exception's remaining fields ride across the unwind as bytes. Only fields
// that need no releasing are allowed on a throwable struct, so a flat copy is
// enough — and `message` is excluded from that promise: the copy carries a
// pointer the unwind frees, which the catch overwrites with a fresh string
// before anything can read it.
static __thread char _dex_exc_payload[DEX_EXCEPTION_PAYLOAD_MAX];
static __thread size_t _dex_exc_payload_size = 0;
static __thread const char* _dex_exc_payload_type = 0;

static void _dex_exc_stage_payload(const void* src, size_t n) {
    if (n > DEX_EXCEPTION_PAYLOAD_MAX) {
        // The generated code asserts against this at compile time; zero here
        // rather than overrun if that assert is ever removed.
        _dex_exc_payload_size = 0;
        return;
    }
    memcpy(_dex_exc_payload, src, n);
    _dex_exc_payload_size = n;
}

// A clause matches its own type, and `Exception` matches everything: it is the
// catch-all, which is what every catch was before types existed.
static int _dex_exc_matches(const char* thrown, const char* clause) {
    if (!clause || strcmp(clause, "Exception") == 0) {
        return 1;
    }
    return thrown && strcmp(thrown, clause) == 0;
}

// Fills a caught exception's fields. The staged bytes belong to the thrown
// type, so they are only copied into a handler of that same type; a catch-all
// handling some other type sees zeroes rather than another struct's layout.
static void _dex_exc_restore(const char* clause, void* dst, size_t n) {
    if (_dex_exc_payload_size == n && _dex_exc_payload_type && clause &&
        strcmp(_dex_exc_payload_type, clause) == 0) {
        memcpy(dst, _dex_exc_payload, n);
        return;
    }
    memset(dst, 0, n);
}

static void _dex_exc_fail(const char* type, const char* msg) {
    fprintf(stderr, "Unhandled %s: %s\n", type ? type : "exception", msg ? msg : "");
    exit(1);
}

static void _dex_throw_staged(const char* type) {
    _dex_exc_payload_type = type;
    if (_dex_exc_top < 0) {
        _dex_exc_fail(type, _dex_exc_msg);
    }
    _dex_exc_stack[_dex_exc_top].message = _dex_exc_msg;
    _dex_exc_stack[_dex_exc_top].type = type;
    _dex_exc_stack[_dex_exc_top].active = 1;
    DEX_LONGJMP(_dex_exc_stack[_dex_exc_top].env);
}

// A literal message needs no staging: it is static storage and outlives the
// unwind on its own. Nothing else is carried, so the payload is cleared.
static void _dex_throw(const char* type, const char* msg) {
    _dex_exc_payload_size = 0;
    _dex_exc_payload_type = type;
    if (_dex_exc_top < 0) {
        _dex_exc_fail(type, msg);
    }
    _dex_exc_stack[_dex_exc_top].message = msg;
    _dex_exc_stack[_dex_exc_top].type = type;
    _dex_exc_stack[_dex_exc_top].active = 1;
    DEX_LONGJMP(_dex_exc_stack[_dex_exc_top].env);
}

// Re-throwing keeps what was staged: the exception is the same one, so its
// payload has to survive being passed outward. A try whose clauses all decline
// it, or one with only a finally, ends here.
static void _dex_rethrow(const char* type, const char* msg) {
    if (_dex_exc_top < 0) {
        _dex_exc_fail(type, msg);
    }
    _dex_exc_stack[_dex_exc_top].message = msg;
    _dex_exc_stack[_dex_exc_top].type = type;
    _dex_exc_stack[_dex_exc_top].active = 1;
    DEX_LONGJMP(_dex_exc_stack[_dex_exc_top].env);
}

#ifdef DEX_MEM_CHECK
static void _dex_mc_throw_injected(const char* msg) {
    _dex_exc_stage(msg);
    _dex_throw_staged("Exception");
}

static int _dex_mc_in_try(void) {
    return _dex_exc_top >= 0;
}

__attribute__((constructor)) static void _dex_mc_inject_init(void) {
    dex_mc_inject_throw = _dex_mc_throw_injected;
    dex_mc_inject_armed = _dex_mc_in_try;
}
#endif
