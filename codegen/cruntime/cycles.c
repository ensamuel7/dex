// DexLang Cycle Detection Runtime (Debug)
// Fixed-size tracking table for #[debug(cycles)] annotated objects.

#include <stdio.h>

#define DEX_CYCLE_TABLE_SIZE 256

typedef struct {
    void* obj;
    const char* name;
    void* parent;
} DexCycleEntry;

static DexCycleEntry dex_cycle_table[DEX_CYCLE_TABLE_SIZE];
static int dex_cycle_count = 0;

static void dex_cycle_track(void* obj, const char* name) {
    if (dex_cycle_count < DEX_CYCLE_TABLE_SIZE) {
        dex_cycle_table[dex_cycle_count].obj = obj;
        dex_cycle_table[dex_cycle_count].name = name;
        dex_cycle_table[dex_cycle_count].parent = NULL;
        dex_cycle_count++;
    }
}

static void dex_cycle_check_assign(void* parent, void* child) {
    if (!parent || !child) return;

    // Record the parent relationship
    for (int i = 0; i < dex_cycle_count; i++) {
        if (dex_cycle_table[i].obj == child) {
            dex_cycle_table[i].parent = parent;
            break;
        }
    }

    // Walk parent chain from parent to check if we reach child (cycle)
    void* current = parent;
    int depth = 0;
    while (current && depth < DEX_CYCLE_TABLE_SIZE) {
        if (current == child) {
            // Find names for reporting
            const char* parent_name = "unknown";
            const char* child_name = "unknown";
            for (int i = 0; i < dex_cycle_count; i++) {
                if (dex_cycle_table[i].obj == parent) parent_name = dex_cycle_table[i].name;
                if (dex_cycle_table[i].obj == child) child_name = dex_cycle_table[i].name;
            }
            fprintf(stderr, "warning: reference cycle detected: %s -> %s\n", parent_name, child_name);
            return;
        }
        // Find parent of current
        void* next = NULL;
        for (int i = 0; i < dex_cycle_count; i++) {
            if (dex_cycle_table[i].obj == current) {
                next = dex_cycle_table[i].parent;
                break;
            }
        }
        current = next;
        depth++;
    }
}
