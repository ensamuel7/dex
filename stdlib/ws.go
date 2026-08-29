package stdlib

import (
	_ "embed"
	"os/exec"
	"strings"

	"github.com/ensamuel7/dex/ast"
)

//go:embed cruntime/ws_handshake.h
var wsHandshakeRuntime string

//go:embed cruntime/ws.c
var wsServerRuntime string

// The handshake unit is prepended rather than #included: the runtime is pasted
// into the generated C, which is compiled with no include path back to here.
var wsRuntime = wsHandshakeRuntime + wsServerRuntime

// wss:// needs OpenSSL on the link line, not just its headers on the include
// path. When pkg-config cannot find it, TLS is switched off explicitly so a
// stray include path from another module cannot half-enable it.
func detectSslFlags() []string {
	out, err := exec.Command("pkg-config", "--cflags", "openssl").Output()
	if err != nil {
		return []string{"-DDEX_SSL_DISABLED"}
	}
	flags := []string{"-DDEX_HAS_SSL"}
	flags = append(flags, strings.Fields(strings.TrimSpace(string(out)))...)
	if libs, err := exec.Command("pkg-config", "--libs", "openssl").Output(); err == nil {
		flags = append(flags, strings.Fields(strings.TrimSpace(string(libs)))...)
	} else {
		flags = append(flags, "-lssl", "-lcrypto")
	}
	return flags
}

func init() {
	Register(&Module{
		Name: "ws",
		Funcs: map[string]FuncDef{
			"handleMessage": {
				Params:     nil,
				ParamNames: []string{"handler"},
				ReturnType: ast.TypeVoid,
				CName:      "",
				Doc:        "Register a message handler fn(conn: ws.Conn, msg: string): void. Applies to the ws.listen() started on this same thread.",
			},
			"listen": {
				Params:     []ast.Type{ast.TypeInt},
				ParamNames: []string{"port"},
				ReturnType: ast.TypeVoid,
				CName:      "dex_ws_listen",
				Doc:        "Start a WebSocket server on the given port and block serving it. Takes the handlers and subprotocol registered on the current thread and gives this server its own copy, so several servers can run at once — spawn each on its own thread.",
			},
			"connect": {
				Params:     nil,
				ParamNames: []string{"url"},
				ReturnType: ast.TypeVoid, // actual return type resolved by checker
				CName:      "",
				Doc:        "Connect to a WebSocket server. Returns a ws.Conn.",
			},
			"send": {
				Params:     nil,
				ParamNames: []string{"conn", "msg"},
				ReturnType: ast.TypeVoid,
				CName:      "",
				Doc:        "Send a text message on a WebSocket connection.",
			},
			"receive": {
				Params:     nil,
				ParamNames: []string{"conn"},
				ReturnType: ast.TypeVoid, // actual return type resolved by checker
				CName:      "",
				Doc:        "Receive a text message from a WebSocket connection (blocking).",
			},
			"close": {
				Params:     nil,
				ParamNames: []string{"conn"},
				ReturnType: ast.TypeVoid,
				CName:      "",
				Doc:        "Close a WebSocket connection.",
			},
			"handleConnect": {
				Params:     nil,
				ParamNames: []string{"handler"},
				ReturnType: ast.TypeVoid,
				CName:      "",
				Doc:        "Register a connect handler fn(conn: ws.Conn, path: string): void. Applies to the ws.listen() started on this same thread.",
			},
			"handleDisconnect": {
				Params:     nil,
				ParamNames: []string{"handler"},
				ReturnType: ast.TypeVoid,
				CName:      "",
				Doc:        "Register a disconnect handler fn(conn: ws.Conn): void. Applies to the ws.listen() started on this same thread.",
			},
			"setProtocol": {
				Params:     []ast.Type{ast.TypeString},
				ParamNames: []string{"protocol"},
				ReturnType: ast.TypeVoid,
				CName:      "dex_ws_set_protocol",
				Doc:        "Set the WebSocket subprotocol for handshakes made from this thread — the ws.listen() server started on it, and any ws.connect() calls it makes. Each server keeps its own value, so two listeners can negotiate different subprotocols.",
			},
		},
		Types: []ast.StructDef{
			{
				Name: "Conn",
				Doc:  "WebSocket connection handle.",
				Fields: []ast.StructField{
					{Name: "fd", Type: ast.TypeInt, Doc: "Socket file descriptor."},
					{Name: "isServer", Type: ast.TypeInt, Doc: "1 if server-side, 0 if client-side."},
					{Name: "ssl", Type: ast.TypeLong, Doc: "SSL pointer (cast to long), 0 if plain."},
				},
			},
		},
		CFlags:   append([]string{"-pthread"}, detectSslFlags()...),
		CRuntime: wsRuntime,
	})
}
