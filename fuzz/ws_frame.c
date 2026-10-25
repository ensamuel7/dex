#include "runtime.h"
#include "../stdlib/cruntime/base64.h"
#include "../stdlib/cruntime/ws_handshake.h"
#include "../stdlib/cruntime/ws.c"

static int fuzz_wfd = -1;
static int fuzz_rfd = -1;

static void fuzz_setup(void) {
    if (fuzz_rfd >= 0) return;
    char path[] = "/tmp/dex-fuzz-ws-XXXXXX";
    fuzz_wfd = mkstemp(path);
    if (fuzz_wfd < 0) abort();
    fuzz_rfd = open(path, O_RDONLY);
    if (fuzz_rfd < 0) abort();
    unlink(path);
}

static void fuzz_load(const uint8_t* data, size_t size) {
    if (ftruncate(fuzz_wfd, 0) != 0) abort();
    if (size && pwrite(fuzz_wfd, data, size, 0) != (ssize_t)size) abort();
    if (lseek(fuzz_rfd, 0, SEEK_SET) != 0) abort();
}

static void fuzz_server(const uint8_t* data, size_t size) {
    fuzz_load(data, size);
    DexWsConn* conn = dex_ws_conn_new(NULL, fuzz_rfd);
    if (!conn) abort();
    conn->state = WS_READING_FRAME;
    dex_ws_reset_frame_parser(conn);
    for (int frames = 0; frames < 100000; frames++) {
        int r = dex_ws_parse_frame_incremental(conn);
        if (r < 0) break;
        if (r == 0) continue;
        if (conn->frame_payload_read != conn->frame_payload_len) abort();
        if (conn->frame_opcode == 0x8) break;
        if (conn->frame_opcode == 0x1) {
            DexString* msg = dex_string_from_cstr(conn->frame_payload);
            conn->frame_payload = NULL;
            dex_release(msg);
        }
        dex_ws_reset_frame_parser(conn);
    }
    dex_ws_conn_free(conn);
}

static void fuzz_client(const uint8_t* data, size_t size) {
    fuzz_load(data, size);
    static char buf[65536];
    for (int frames = 0; frames < 100000; frames++) {
        int opcode = 0;
        int n = dex_ws_frame_recv(fuzz_rfd, NULL, buf, sizeof(buf), &opcode);
        if (n < 0) break;
        if ((size_t)n >= sizeof(buf) || buf[n] != '\0') abort();
    }
}

int LLVMFuzzerTestOneInput(const uint8_t* data, size_t size) {
    fuzz_setup();
    fuzz_server(data, size);
    fuzz_client(data, size);
    return 0;
}
