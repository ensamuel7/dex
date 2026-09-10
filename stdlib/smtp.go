package stdlib

import (
	_ "embed"

	"github.com/ensamuel7/dex/ast"
)

//go:embed cruntime/smtp.c
var smtpClientRuntime string

// base64.h is prepended rather than #included: the runtime is pasted into the
// generated C, which is compiled with no include path back to here. Its own
// guard makes the paste idempotent, so a program importing both `smtp` and `ws`
// — which prepends the same file — gets one copy of the encoder.
var smtpRuntime = base64Runtime + smtpClientRuntime

func init() {
	Register(&Module{
		Name: "smtp",
		Funcs: map[string]FuncDef{
			"send": {
				Params: []ast.Type{
					ast.TypeString, ast.TypeInt, ast.TypeString, ast.TypeString,
					ast.TypeString, ast.TypeString, ast.TypeString, ast.TypeString,
				},
				ParamNames: []string{"host", "port", "username", "password", "from", "to", "subject", "body"},
				ReturnType: ast.TypeBool,
				CName:      "dex_smtp_send",
				Doc:        "Send one plain-text message and wait to be told it was accepted. Port 465 is TLS from the first byte, anything else upgrades with STARTTLS. Pass \"\" for username to skip authentication; with one set, an unencrypted connection is refused rather than sending the password in the clear. Returns false — and says why on stderr — for every refusal.",
			},
			"sendHtml": {
				Params: []ast.Type{
					ast.TypeString, ast.TypeInt, ast.TypeString, ast.TypeString,
					ast.TypeString, ast.TypeString, ast.TypeString, ast.TypeString,
					ast.TypeString,
				},
				ParamNames: []string{"host", "port", "username", "password", "from", "to", "subject", "body", "html"},
				ReturnType: ast.TypeBool,
				CName:      "dex_smtp_send_html",
				Doc:        "Send one message in both plain text and HTML, as multipart/alternative, and wait to be told it was accepted. `body` is the text a client that will not render HTML shows; `html` is what one that will shows instead, base64'd so a long line cannot breach the 998-octet limit. An empty `html` sends exactly what send() does. Same TLS and authentication rules as send().",
			},
		},
		// TLS comes from OpenSSL, found the way the ws module finds it: without
		// it a build still compiles, and a call that needs TLS fails with a
		// message saying so rather than silently sending in the clear.
		CRuntime: smtpRuntime,
		CFlags:   append([]string{"-pthread"}, detectSslFlags()...),
	})
}
