package stdlib

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The handshake is C, so it is exercised by compiling cruntime/ws_handshake.h
// against a harness. Without this the accept-key derivation had no coverage at
// all: a Dex client talking to a Dex server agrees with itself no matter what
// the shared GUID is, which is how a wrong one went unnoticed.
const handshakeHarness = `
#include "ws_handshake.h"

static int failures = 0;

static void expect_str(const char* what, const char* got, const char* want) {
	if (strcmp(got, want) != 0) {
		printf("FAIL %s: got %s want %s\n", what, got, want);
		failures++;
	}
}

static void expect_int(const char* what, int got, int want) {
	if (got != want) {
		printf("FAIL %s: got %d want %d\n", what, got, want);
		failures++;
	}
}

int main(void) {
	/* RFC 6455 section 1.3 worked example. */
	const char* KEY = "dGhlIHNhbXBsZSBub25jZQ==";
	char accept[64];
	dex_ws_accept_key(KEY, accept, sizeof(accept));
	expect_str("rfc6455 accept key", accept, "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=");

	char neg[128];
	expect_int("offer matches", dex_ws_client_offers(
		"GET / HTTP/1.1\r\nSec-WebSocket-Protocol: ocpp1.6\r\n\r\n",
		"ocpp1.6", neg, sizeof(neg)), 1);
	expect_str("offer echoed verbatim", neg, "ocpp1.6");

	/* Echo the client's spelling: browsers compare case-sensitively. */
	dex_ws_client_offers("GET / HTTP/1.1\r\nSec-WebSocket-Protocol: OCPP1.6\r\n\r\n",
		"ocpp1.6", neg, sizeof(neg));
	expect_str("offer echoed with client casing", neg, "OCPP1.6");

	expect_int("picks from a list", dex_ws_client_offers(
		"GET / HTTP/1.1\r\nSec-WebSocket-Protocol: foo, ocpp1.6 , bar\r\n\r\n",
		"ocpp1.6", neg, sizeof(neg)), 1);

	expect_int("no header offers nothing", dex_ws_client_offers(
		"GET / HTTP/1.1\r\nHost: x\r\n\r\n", "ocpp1.6", neg, sizeof(neg)), 0);

	expect_int("non-matching offer declined", dex_ws_client_offers(
		"GET / HTTP/1.1\r\nSec-WebSocket-Protocol: ocpp2.0.1\r\n\r\n",
		"ocpp1.6", neg, sizeof(neg)), 0);

	expect_int("prefix is not a match", dex_ws_client_offers(
		"GET / HTTP/1.1\r\nSec-WebSocket-Protocol: ocpp1.6.1\r\n\r\n",
		"ocpp1.6", neg, sizeof(neg)), 0);

	char good[256];
	snprintf(good, sizeof(good),
		"HTTP/1.1 101 Switching Protocols\r\nSec-WebSocket-Accept: %s\r\n\r\n", accept);
	expect_int("valid server response", dex_ws_response_valid(good, KEY), 1);

	expect_int("wrong accept refused", dex_ws_response_valid(
		"HTTP/1.1 101 Switching Protocols\r\nSec-WebSocket-Accept: AAAA\r\n\r\n", KEY), 0);
	expect_int("missing accept refused", dex_ws_response_valid(
		"HTTP/1.1 101 Switching Protocols\r\n\r\n", KEY), 0);
	expect_int("non-101 refused", dex_ws_response_valid(
		"HTTP/1.1 400 Bad Request\r\n\r\n", KEY), 0);

	printf(failures == 0 ? "OK\n" : "FAILURES\n");
	return failures == 0 ? 0 : 1;
}
`

func TestWebSocketHandshake(t *testing.T) {
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("cc not available")
	}

	dir := t.TempDir()
	src := filepath.Join(dir, "harness.c")
	if err := os.WriteFile(src, []byte(handshakeHarness), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "harness")

	build := exec.Command(cc, "-I", "cruntime", "-Wall", "-Werror", "-o", bin, src)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("compiling handshake harness failed: %v\n%s", err, out)
	}

	out, err := exec.Command(bin).CombinedOutput()
	if err != nil || !strings.Contains(string(out), "OK") {
		t.Fatalf("handshake harness reported failures:\n%s", out)
	}
}
