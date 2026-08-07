package stdlib

import (
	_ "embed"

	"github.com/ensamuel7/dex/ast"
)

//go:embed cruntime/ws.c
var wsRuntime string

func init() {
	Register(&Module{
		Name: "ws",
		Funcs: map[string]FuncDef{
			"handleMessage": {
				Params:     nil,
				ParamNames: []string{"handler"},
				ReturnType: ast.TypeVoid,
				CName:      "",
				Doc:        "Register a message handler fn(conn: ws.Conn, msg: string): void.",
			},
			"listen": {
				Params:     []ast.Type{ast.TypeInt},
				ParamNames: []string{"port"},
				ReturnType: ast.TypeVoid,
				CName:      "dex_ws_listen",
				Doc:        "Start WebSocket server on the specified port.",
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
				Doc:        "Register a connect handler fn(conn: ws.Conn, path: string): void.",
			},
			"handleDisconnect": {
				Params:     nil,
				ParamNames: []string{"handler"},
				ReturnType: ast.TypeVoid,
				CName:      "",
				Doc:        "Register a disconnect handler fn(conn: ws.Conn): void.",
			},
			"setProtocol": {
				Params:     []ast.Type{ast.TypeString},
				ParamNames: []string{"protocol"},
				ReturnType: ast.TypeVoid,
				CName:      "dex_ws_set_protocol",
				Doc:        "Set the WebSocket subprotocol for server and client handshakes.",
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
		CFlags:   []string{"-pthread"},
		CRuntime: wsRuntime,
	})
}
