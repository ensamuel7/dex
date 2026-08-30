package codegen

import (
	"fmt"
	"strings"

	"github.com/ensamuel7/dex/ast"
)

// genWebCall emits the http and ws module calls: route and callback
// registration, and the HTTP client. Reports whether it handled the call.
func (g *Generator) genWebCall(out *strings.Builder, e *ast.CallExpr) bool {

	if e.Module == "http" && e.Name == "route" {
		// The handler is an ordinary function value, so it may be a plain
		// function or a method carrying the struct that holds its dependencies.
		// Handlers are allowed to be written in several shapes — returning a
		// string or a number rather than a full response, and taking no request
		// at all — so anything that is not already the router's exact signature
		// is wrapped in an adapter closure.
		out.WriteString("dex_route(")
		g.genStringArg(out, e.Args[0])
		out.WriteString(", ")
		g.genStringArg(out, e.Args[1])
		out.WriteString(", ")
		g.genRouteHandler(out, e.Args[2])
		out.WriteString(")")
		return true
	}

	// http.response(statusCode, body, contentType) and
	// http.responseWith(..., headers) -> HttpResponse struct literal.
	// Retain borrowed string refs for ownership transfer to the worker thread.
	// response() leaves the trailing headers field zero-initialised, which the
	// runtime reads as "no extra headers".
	if e.Module == "http" && (e.Name == "response" || e.Name == "responseWith") {
		out.WriteString("(Dex_HttpResponse){")
		g.genExpr(out, e.Args[0])
		out.WriteString(", ")
		g.genOwnedStringArg(out, e.Args[1])
		out.WriteString(", ")
		g.genOwnedStringArg(out, e.Args[2])
		if e.Name == "responseWith" {
			out.WriteString(", ")
			g.genOwnedStringArg(out, e.Args[3])
		}
		out.WriteString("}")
		return true
	}

	// ws.handleMessage(handler) — register message callback
	// ws.handleMessage / handleConnect / handleDisconnect — register a callback.
	// The handler is an ordinary function value, so it may be a plain function or
	// a method bound to the struct holding its dependencies.
	if e.Module == "ws" && (e.Name == "handleMessage" || e.Name == "handleConnect" || e.Name == "handleDisconnect") {
		globals := map[string]string{
			"handleMessage":    "dex_ws_on_message",
			"handleConnect":    "dex_ws_on_connect",
			"handleDisconnect": "dex_ws_on_disconnect",
		}
		// Replacing a callback drops the one it replaced, so registering twice
		// does not strand the first handler and whatever it captured.
		global := globals[e.Name]
		tmp := g.nextTemp()
		out.WriteString(fmt.Sprintf("{ DexClosure* %s = %s; %s = ", tmp, global, global))
		g.genOwnedClosureArg(out, e.Args[0])
		out.WriteString(fmt.Sprintf("; dex_release(%s); }", tmp))
		return true
	}

	// ws.connect(url) -> Dex_Conn
	if e.Module == "ws" && e.Name == "connect" {
		out.WriteString("dex_ws_connect(")
		g.genStringArg(out, e.Args[0])
		out.WriteString(")")
		return true
	}

	// ws.send(conn, msg)
	if e.Module == "ws" && e.Name == "send" {
		out.WriteString("dex_ws_send(")
		g.genExpr(out, e.Args[0])
		out.WriteString(", ")
		g.genStringArg(out, e.Args[1])
		out.WriteString(")")
		return true
	}

	// ws.receive(conn) -> DexString*
	if e.Module == "ws" && e.Name == "receive" {
		out.WriteString("dex_ws_receive(")
		g.genExpr(out, e.Args[0])
		out.WriteString(")")
		return true
	}

	// ws.close(conn)
	if e.Module == "ws" && e.Name == "close" {
		out.WriteString("dex_ws_close(")
		g.genExpr(out, e.Args[0])
		out.WriteString(")")
		return true
	}

	// HTTP client functions — bridge string args and wrap string returns
	if e.Module == "http" {
		switch e.Name {
		case "get":
			if len(e.Args) == 2 {
				out.WriteString("dex_http_get_h(")
				g.genStringArg(out, e.Args[0])
				out.WriteString(", ")
				g.genStringArg(out, e.Args[1])
				out.WriteString(")")
			} else {
				out.WriteString("dex_http_get(")
				g.genStringArg(out, e.Args[0])
				out.WriteString(")")
			}
			return true
		case "post":
			if len(e.Args) == 3 {
				out.WriteString("dex_http_post_h(")
				g.genStringArg(out, e.Args[0])
				out.WriteString(", ")
				g.genStringArg(out, e.Args[1])
				out.WriteString(", ")
				g.genStringArg(out, e.Args[2])
				out.WriteString(")")
			} else {
				out.WriteString("dex_http_post(")
				g.genStringArg(out, e.Args[0])
				out.WriteString(", ")
				g.genStringArg(out, e.Args[1])
				out.WriteString(")")
			}
			return true
		case "put":
			if len(e.Args) == 3 {
				out.WriteString("dex_http_put_h(")
				g.genStringArg(out, e.Args[0])
				out.WriteString(", ")
				g.genStringArg(out, e.Args[1])
				out.WriteString(", ")
				g.genStringArg(out, e.Args[2])
				out.WriteString(")")
			} else {
				out.WriteString("dex_http_put(")
				g.genStringArg(out, e.Args[0])
				out.WriteString(", ")
				g.genStringArg(out, e.Args[1])
				out.WriteString(")")
			}
			return true
		case "patch":
			if len(e.Args) == 3 {
				out.WriteString("dex_http_patch_h(")
				g.genStringArg(out, e.Args[0])
				out.WriteString(", ")
				g.genStringArg(out, e.Args[1])
				out.WriteString(", ")
				g.genStringArg(out, e.Args[2])
				out.WriteString(")")
			} else {
				out.WriteString("dex_http_patch(")
				g.genStringArg(out, e.Args[0])
				out.WriteString(", ")
				g.genStringArg(out, e.Args[1])
				out.WriteString(")")
			}
			return true
		case "delete":
			if len(e.Args) == 2 {
				out.WriteString("dex_http_delete_h(")
				g.genStringArg(out, e.Args[0])
				out.WriteString(", ")
				g.genStringArg(out, e.Args[1])
				out.WriteString(")")
			} else {
				out.WriteString("dex_http_delete(")
				g.genStringArg(out, e.Args[0])
				out.WriteString(")")
			}
			return true
		case "request":
			out.WriteString("dex_http_request(")
			g.genStringArg(out, e.Args[0])
			out.WriteString(", ")
			g.genStringArg(out, e.Args[1])
			out.WriteString(", ")
			g.genStringArg(out, e.Args[2])
			out.WriteString(", ")
			g.genStringArg(out, e.Args[3])
			out.WriteString(")")
			return true
		case "header":
			out.WriteString("dex_string_from_cstr(dex_http_header(")
			g.genStringArg(out, e.Args[0])
			out.WriteString(", ")
			g.genStringArg(out, e.Args[1])
			out.WriteString(", ")
			g.genStringArg(out, e.Args[2])
			out.WriteString("))")
			return true
		case "formNew":
			out.WriteString("dex_string_from_cstr(dex_http_form_new())")
			return true
		case "formField":
			out.WriteString("dex_string_from_cstr(dex_http_form_field(")
			g.genStringArg(out, e.Args[0])
			out.WriteString(", ")
			g.genStringArg(out, e.Args[1])
			out.WriteString(", ")
			g.genStringArg(out, e.Args[2])
			out.WriteString("))")
			return true
		case "formFile":
			out.WriteString("dex_string_from_cstr(dex_http_form_file(")
			g.genStringArg(out, e.Args[0])
			out.WriteString(", ")
			g.genStringArg(out, e.Args[1])
			out.WriteString(", ")
			g.genStringArg(out, e.Args[2])
			out.WriteString("))")
			return true
		case "postForm":
			if len(e.Args) == 3 {
				out.WriteString("dex_http_post_form_h(")
				g.genStringArg(out, e.Args[0])
				out.WriteString(", ")
				g.genStringArg(out, e.Args[1])
				out.WriteString(", ")
				g.genStringArg(out, e.Args[2])
				out.WriteString(")")
			} else {
				out.WriteString("dex_http_post_form(")
				g.genStringArg(out, e.Args[0])
				out.WriteString(", ")
				g.genStringArg(out, e.Args[1])
				out.WriteString(")")
			}
			return true
		case "listen":
			if len(e.Args) == 2 {
				out.WriteString("dex_listen_multi(")
				g.genExpr(out, e.Args[0])
				out.WriteString(", ")
				g.genExpr(out, e.Args[1])
				out.WriteString(")")
			} else {
				out.WriteString("dex_listen(")
				g.genExpr(out, e.Args[0])
				out.WriteString(")")
			}
			return true
		}
	}

	return false
}
