#include "runtime.h"
#include "../stdlib/cruntime/base64.h"
#include "../stdlib/cruntime/ws_handshake.h"
#include "../stdlib/cruntime/ws.c"

static const char* fuzz_protocols[] = {
    "",
    "chat.v1",
    "graphql-ws",
    "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
};

static const char* fuzz_headers[] = {"Origin", "Sec-WebSocket-Protocol", "Host", "X"};

int LLVMFuzzerTestOneInput(const uint8_t* data, size_t size) {
    if (size == 0) return 0;
    const char* proto = fuzz_protocols[data[0] % 4];
    const char* header = fuzz_headers[(data[0] >> 2) % 4];
    char* text = dex_fuzz_cstr(data + 1, size - 1);

    DexWsConn* conn = dex_ws_conn_new(NULL, 7);
    if (!conn) abort();
    char response[512];
    int rlen = dex_ws_handshake_reply(text, proto, conn->path, sizeof(conn->path), response, sizeof(response));
    if (rlen > 0) {
        if ((size_t)rlen >= sizeof(response) || response[rlen] != '\0') abort();
        if (strlen(conn->path) >= sizeof(conn->path)) abort();
        free(conn->read_buf);
        conn->read_buf = text;
        text = NULL;
        conn->state = WS_READING_FRAME;
        Dex_Conn handle = {7, 1, 0};
        DexString* v = dex_ws_header(handle, header);
        if (v->len != strlen(v->data)) abort();
        dex_release(v);
    }
    dex_ws_conn_free(conn);

    if (text) {
        char negotiated[128];
        (void)dex_ws_client_offers(text, proto, negotiated, sizeof(negotiated));
        (void)dex_ws_response_valid(text, "dGhlIHNhbXBsZSBub25jZQ==");
        free(text);
    }
    return 0;
}
