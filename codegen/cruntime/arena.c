// DexLang Arena Allocator Runtime
// Bump-allocator for #[region] annotated variables.

#include <stdlib.h>
#include <string.h>

typedef struct DexArenaBlock {
    struct DexArenaBlock* next;
    size_t size;
    size_t used;
    char data[];
} DexArenaBlock;

typedef struct {
    DexArenaBlock* head;
    size_t block_size;
} DexArena;

static DexArenaBlock* dex_arena_block_new(size_t size) {
    DexArenaBlock* block = (DexArenaBlock*)malloc(sizeof(DexArenaBlock) + size);
    if (!block) { fprintf(stderr, "runtime error: out of memory\n"); exit(1); }
    block->next = NULL;
    block->size = size;
    block->used = 0;
    return block;
}

static DexArena* dex_arena_new(size_t block_size) {
    DexArena* arena = (DexArena*)malloc(sizeof(DexArena));
    if (!arena) { fprintf(stderr, "runtime error: out of memory\n"); exit(1); }
    arena->block_size = block_size;
    arena->head = dex_arena_block_new(block_size);
    return arena;
}

static void* dex_arena_alloc(DexArena* arena, size_t size) {
    // Align to 8 bytes
    size = (size + 7) & ~(size_t)7;
    DexArenaBlock* block = arena->head;
    if (block->used + size > block->size) {
        size_t new_size = arena->block_size;
        if (size > new_size) new_size = size;
        DexArenaBlock* new_block = dex_arena_block_new(new_size);
        new_block->next = arena->head;
        arena->head = new_block;
        block = new_block;
    }
    void* ptr = block->data + block->used;
    block->used += size;
    return ptr;
}

static DexString* dex_arena_string(DexArena* arena, const char* s, size_t len) {
    size_t total = sizeof(DexString) + len + 1;
    DexString* str = (DexString*)dex_arena_alloc(arena, total);
    // Set rc to INT_MAX sentinel — arena-managed strings are never individually freed
    atomic_store_explicit(&str->header.rc, __INT_MAX__, memory_order_relaxed);
    str->header.destroy = NULL;
    str->len = (int)len;
    str->data = (char*)str + sizeof(DexString);
    memcpy(str->data, s, len);
    str->data[len] = '\0';
    return str;
}

static void dex_arena_destroy(DexArena* arena) {
    DexArenaBlock* block = arena->head;
    while (block) {
        DexArenaBlock* next = block->next;
        free(block);
        block = next;
    }
    free(arena);
}
