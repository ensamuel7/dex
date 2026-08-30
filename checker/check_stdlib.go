package checker

import (
	"github.com/ensamuel7/dex/ast"
	"github.com/ensamuel7/dex/stdlib"
)

// checkStdlibCall type-checks the standard library calls whose signatures are
// polymorphic or context-dependent, and so cannot be expressed as a plain
// FuncDef. The second result reports whether the call was one of them.
func (c *Checker) checkStdlibCall(e *ast.CallExpr, mod *stdlib.Module) (ast.Type, bool, error) {
	// Special case: fmt.print/fmt.println — accepts any primitive type
	if e.Module == "fmt" && (e.Name == "print" || e.Name == "println") {
		if len(e.Args) != 1 {
			return 0, true, c.errAt(e.Pos, "fmt.%s() takes exactly 1 argument, got %d", e.Name, len(e.Args))
		}
		argType, err := c.checkExpr(e.Args[0])
		if err != nil {
			return 0, true, err
		}
		// Auto-unwrap ref types for printing (e.g., &User -> User)
		printType := argType
		if ast.IsRefType(printType) {
			printType = ast.RefInnerType(printType)
		}
		if printType != ast.TypeInt && printType != ast.TypeLong && printType != ast.TypeDouble &&
			printType != ast.TypeString && printType != ast.TypeBool && printType != ast.TypeChar &&
			!ast.IsEnumType(printType) && !ast.IsArrayType(printType) && !ast.IsStructType(printType) {
			return 0, true, c.errAt(e.Pos, "fmt.%s() argument must be a primitive type, got %s", e.Name, typeName(argType))
		}
		return ast.TypeVoid, true, nil
	}

	// Special case: json.encode(value) -> string
	if e.Module == "json" && e.Name == "encode" {
		if len(e.Args) != 1 {
			return 0, true, c.errAt(e.Pos, "json.encode() takes exactly 1 argument, got %d", len(e.Args))
		}
		// A literal passed straight to encode is JSON construction —
		// json.encode([2, id, action, payload]) needs no annotation to
		// say so, because there is nothing else it could mean.
		switch e.Args[0].(type) {
		case *ast.ObjectLitExpr, *ast.MapLitExpr, *ast.ArrayLitExpr:
			if err := c.markJsonValue(e.Args[0]); err != nil {
				return 0, true, err
			}
		}
		argType, err := c.checkExpr(e.Args[0])
		if err != nil {
			return 0, true, err
		}
		if argType == ast.TypeJsonValue {
			return ast.TypeString, true, nil
		}
		if !ast.IsArrayType(argType) && !ast.IsStructType(argType) && !ast.IsMapType(argType) {
			return 0, true, c.errAt(e.Pos, "json.encode() argument must be a json.Value, array, struct, or map, got %s", typeName(argType))
		}
		if ast.IsMapType(argType) && ast.MapKeyType(argType) != ast.TypeString {
			return 0, true, c.errAt(e.Pos, "json.encode() map key type must be string for JSON serialization, got %s", typeName(ast.MapKeyType(argType)))
		}
		return ast.TypeString, true, nil
	}

	// Special case: json.decode(jsonStr) -> struct (resolved from context)
	if e.Module == "json" && e.Name == "decode" {
		if len(e.Args) != 1 {
			return 0, true, c.errAt(e.Pos, "json.decode() takes exactly 1 argument, got %d", len(e.Args))
		}
		argType, err := c.checkExpr(e.Args[0])
		if err != nil {
			return 0, true, err
		}
		// Decoding accepts wire text or an already-parsed value, so a
		// nested payload can go straight into a struct without being
		// re-serialized by hand.
		if argType != ast.TypeString && argType != ast.TypeJsonValue {
			return 0, true, c.errAt(e.Pos, "json.decode() argument must be a string or json.Value, got %s", typeName(argType))
		}
		// The target may be a json.Value, a struct, or a struct array;
		// the latter two either plain (lenient: a bad payload decodes to
		// zero values) or optional (checked: a bad payload yields null).
		target := e.ResolvedType
		if ast.IsOptionalType(target) {
			target = ast.OptionalInnerType(target)
		}
		if target == ast.TypeJsonValue {
			return e.ResolvedType, true, nil
		}
		if e.ResolvedType == 0 || !(ast.IsStructType(target) || ast.IsStructArrayType(target)) {
			return 0, true, c.errAt(e.Pos, "json.decode() requires an explicit type annotation (e.g., let x: MyStruct = json.decode(...) or let v: json.Value = json.decode(...))")
		}
		return e.ResolvedType, true, nil
	}

	// Special case: json.set(obj, key, value) — polymorphic value type
	if e.Module == "json" && e.Name == "set" {
		if len(e.Args) != 3 {
			return 0, true, c.errAt(e.Pos, "json.set() takes exactly 3 arguments, got %d", len(e.Args))
		}
		objType, err := c.checkExpr(e.Args[0])
		if err != nil {
			return 0, true, err
		}
		if objType != ast.TypeString {
			return 0, true, c.errAt(e.Pos, "json.set() argument 1 must be string, got %s", typeName(objType))
		}
		keyType, err := c.checkExpr(e.Args[1])
		if err != nil {
			return 0, true, err
		}
		if keyType != ast.TypeString {
			return 0, true, c.errAt(e.Pos, "json.set() argument 2 must be string, got %s", typeName(keyType))
		}
		valType, err := c.checkExpr(e.Args[2])
		if err != nil {
			return 0, true, err
		}
		switch valType {
		case ast.TypeString, ast.TypeInt, ast.TypeBool, ast.TypeLong, ast.TypeDouble:
			// all valid
		default:
			if !ast.IsArrayType(valType) {
				return 0, true, c.errAt(e.Pos, "json.set() value must be a primitive type or array, got %s", typeName(valType))
			}
		}
		return ast.TypeString, true, nil
	}

	// Special case: db.col(rows, col) — return type resolved from context
	if e.Module == "db" && e.Name == "col" {
		if len(e.Args) != 2 {
			return 0, true, c.errAt(e.Pos, "db.col() takes exactly 2 arguments, got %d", len(e.Args))
		}
		for i, arg := range e.Args {
			argType, err := c.checkExpr(arg)
			if err != nil {
				return 0, true, err
			}
			if argType != ast.TypeInt {
				return 0, true, c.errAt(e.Pos, "db.col() argument %d must be int, got %s", i+1, typeName(argType))
			}
		}
		return e.ResolvedType, true, nil
	}

	// Special case: json.arrayPush(arr, value) — polymorphic value type
	if e.Module == "json" && e.Name == "arrayPush" {
		if len(e.Args) != 2 {
			return 0, true, c.errAt(e.Pos, "json.arrayPush() takes exactly 2 arguments, got %d", len(e.Args))
		}
		arrType, err := c.checkExpr(e.Args[0])
		if err != nil {
			return 0, true, err
		}
		if arrType != ast.TypeString {
			return 0, true, c.errAt(e.Pos, "json.arrayPush() argument 1 must be string, got %s", typeName(arrType))
		}
		valType, err := c.checkExpr(e.Args[1])
		if err != nil {
			return 0, true, err
		}
		switch valType {
		case ast.TypeString, ast.TypeInt, ast.TypeBool, ast.TypeLong, ast.TypeDouble:
			// all valid
		default:
			return 0, true, c.errAt(e.Pos, "json.arrayPush() value must be a primitive type, got %s", typeName(valType))
		}
		return ast.TypeString, true, nil
	}

	// Special case: json.setArray(obj, key, array) -> string
	if e.Module == "json" && e.Name == "setArray" {
		if len(e.Args) != 3 {
			return 0, true, c.errAt(e.Pos, "json.setArray() takes exactly 3 arguments, got %d", len(e.Args))
		}
		objType, err := c.checkExpr(e.Args[0])
		if err != nil {
			return 0, true, err
		}
		if objType != ast.TypeString {
			return 0, true, c.errAt(e.Pos, "json.setArray() argument 1 must be string, got %s", typeName(objType))
		}
		keyType, err := c.checkExpr(e.Args[1])
		if err != nil {
			return 0, true, err
		}
		if keyType != ast.TypeString {
			return 0, true, c.errAt(e.Pos, "json.setArray() argument 2 must be string, got %s", typeName(keyType))
		}
		arrType, err := c.checkExpr(e.Args[2])
		if err != nil {
			return 0, true, err
		}
		if !ast.IsArrayType(arrType) {
			return 0, true, c.errAt(e.Pos, "json.setArray() argument 3 must be an array type, got %s", typeName(arrType))
		}
		return ast.TypeString, true, nil
	}

	// Special case: http.route handler validation (before generic arg check)
	if e.Module == "http" && e.Name == "route" {
		if len(e.Args) != 3 {
			return 0, true, c.errAt(e.Pos, "http.route() takes exactly 3 arguments, got %d", len(e.Args))
		}
		// Check method (arg 0) and path (arg 1) are strings
		for i := 0; i < 2; i++ {
			argType, err := c.checkExpr(e.Args[i])
			if err != nil {
				return 0, true, err
			}
			if argType != ast.TypeString {
				return 0, true, c.errAt(e.Pos, "http.route() argument %d must be string, got %s", i+1, typeName(argType))
			}
		}
		// The handler is any function value: a plain function named
		// directly, or a method bound to the struct holding its
		// dependencies. A string literal still names a function, kept for
		// the older spelling.
		var params []ast.Type
		var retType ast.Type
		describe := "handler"

		if lit, isStr := e.Args[2].(*ast.StringLit); isStr {
			name := lit.Value
			sig, ok := c.funcs[name]
			if !ok {
				return 0, true, c.errAt(e.Pos, "http.route() handler '%s' is not a defined function", name)
			}
			params, retType, describe = sig.Params, sig.ReturnType, "'"+name+"'"
		} else {
			handlerType, err := c.checkExpr(e.Args[2])
			if err != nil {
				return 0, true, err
			}
			if !ast.IsFuncType(handlerType) {
				return 0, true, c.errAt(e.Pos, "http.route() handler (argument 3) must be a function value, got %s", typeName(handlerType))
			}
			params = ast.FuncTypeParams(handlerType)
			retType = ast.FuncTypeReturn(handlerType)
		}

		// A handler either takes the request or takes nothing.
		if len(params) == 1 {
			httpReqType, hasReq := ast.LookupStructType("HttpRequest")
			if !hasReq {
				return 0, true, c.errAt(e.Pos, "HttpRequest type not registered (internal error)")
			}
			if params[0] != httpReqType {
				return 0, true, c.errAt(e.Pos, "http.route() handler %s parameter must be http.HttpRequest, got %s", describe, typeName(params[0]))
			}
		} else if len(params) > 1 {
			return 0, true, c.errAt(e.Pos, "http.route() handler %s must take 0 or 1 parameter (http.HttpRequest)", describe)
		}
		if retType == ast.TypeVoid {
			return 0, true, c.errAt(e.Pos, "http.route() handler %s must have a return type", describe)
		}
		return ast.TypeVoid, true, nil
	}

	// Special case: http.response(statusCode, body, contentType) and
	// http.responseWith(statusCode, body, contentType, headers) -> HttpResponse.
	// Identical but for the trailing headers argument.
	if e.Module == "http" && (e.Name == "response" || e.Name == "responseWith") {
		httpRespType, ok := ast.LookupStructType("HttpResponse")
		if !ok {
			return 0, true, c.errAt(e.Pos, "HttpResponse type not registered (internal error)")
		}
		want := 3
		if e.Name == "responseWith" {
			want = 4
		}
		if len(e.Args) != want {
			return 0, true, c.errAt(e.Pos, "http.%s() takes exactly %d arguments, got %d", e.Name, want, len(e.Args))
		}
		arg0Type, err := c.checkExpr(e.Args[0])
		if err != nil {
			return 0, true, err
		}
		if arg0Type != ast.TypeInt {
			return 0, true, c.errAt(e.Pos, "http.%s() argument 1 must be int, got %s", e.Name, typeName(arg0Type))
		}
		for i := 1; i < want; i++ {
			argType, err := c.checkExpr(e.Args[i])
			if err != nil {
				return 0, true, err
			}
			if argType != ast.TypeString {
				return 0, true, c.errAt(e.Pos, "http.%s() argument %d must be string, got %s", e.Name, i+1, typeName(argType))
			}
		}
		e.ResolvedType = httpRespType
		return httpRespType, true, nil
	}

	// ws.handleMessage / handleConnect / handleDisconnect — the handler is
	// any function value of the right shape, so it may be a plain function
	// or a method bound to the struct holding its dependencies.
	if e.Module == "ws" && (e.Name == "handleMessage" || e.Name == "handleConnect" || e.Name == "handleDisconnect") {
		if len(e.Args) != 1 {
			return 0, true, c.errAt(e.Pos, "ws.%s() takes exactly 1 argument, got %d", e.Name, len(e.Args))
		}
		handlerType, err := c.checkExpr(e.Args[0])
		if err != nil {
			return 0, true, err
		}
		if !ast.IsFuncType(handlerType) {
			return 0, true, c.errAt(e.Pos, "ws.%s() argument must be a function value, got %s", e.Name, typeName(handlerType))
		}
		wsConnType, hasConn := ast.LookupStructType("Conn")
		if !hasConn {
			return 0, true, c.errAt(e.Pos, "ws.Conn type not registered (internal error)")
		}

		// handleDisconnect is handed only the connection; the other two
		// also receive the message, or the path that was connected to.
		want := []ast.Type{wsConnType, ast.TypeString}
		shape := "(ws.Conn, string)"
		if e.Name == "handleDisconnect" {
			want = []ast.Type{wsConnType}
			shape = "(ws.Conn)"
		}

		params := ast.FuncTypeParams(handlerType)
		if len(params) != len(want) {
			return 0, true, c.errAt(e.Pos, "ws.%s() handler must take %s, got %d parameters", e.Name, shape, len(params))
		}
		for i, expect := range want {
			if params[i] != expect {
				return 0, true, c.errAt(e.Pos, "ws.%s() handler parameter %d must be %s, got %s", e.Name, i+1, typeName(expect), typeName(params[i]))
			}
		}
		if ast.FuncTypeReturn(handlerType) != ast.TypeVoid {
			return 0, true, c.errAt(e.Pos, "ws.%s() handler must return void", e.Name)
		}
		return ast.TypeVoid, true, nil
	}

	// Special case: ws.connect(url) -> ws.Conn
	if e.Module == "ws" && e.Name == "connect" {
		wsConnType, ok := ast.LookupStructType("Conn")
		if !ok {
			return 0, true, c.errAt(e.Pos, "ws.Conn type not registered (internal error)")
		}
		if len(e.Args) != 1 {
			return 0, true, c.errAt(e.Pos, "ws.connect() takes exactly 1 argument, got %d", len(e.Args))
		}
		argType, err := c.checkExpr(e.Args[0])
		if err != nil {
			return 0, true, err
		}
		if argType != ast.TypeString {
			return 0, true, c.errAt(e.Pos, "ws.connect() argument must be string, got %s", typeName(argType))
		}
		e.ResolvedType = wsConnType
		return wsConnType, true, nil
	}

	// Special case: ws.send(conn, msg)
	if e.Module == "ws" && e.Name == "send" {
		if len(e.Args) != 2 {
			return 0, true, c.errAt(e.Pos, "ws.send() takes exactly 2 arguments, got %d", len(e.Args))
		}
		wsConnType, ok := ast.LookupStructType("Conn")
		if !ok {
			return 0, true, c.errAt(e.Pos, "ws.Conn type not registered (internal error)")
		}
		arg0Type, err := c.checkExpr(e.Args[0])
		if err != nil {
			return 0, true, err
		}
		if arg0Type != wsConnType {
			return 0, true, c.errAt(e.Pos, "ws.send() argument 1 must be ws.Conn, got %s", typeName(arg0Type))
		}
		arg1Type, err := c.checkExpr(e.Args[1])
		if err != nil {
			return 0, true, err
		}
		if arg1Type != ast.TypeString {
			return 0, true, c.errAt(e.Pos, "ws.send() argument 2 must be string, got %s", typeName(arg1Type))
		}
		return ast.TypeVoid, true, nil
	}

	// Special case: ws.receive(conn) -> string
	if e.Module == "ws" && e.Name == "receive" {
		if len(e.Args) != 1 {
			return 0, true, c.errAt(e.Pos, "ws.receive() takes exactly 1 argument, got %d", len(e.Args))
		}
		wsConnType, ok := ast.LookupStructType("Conn")
		if !ok {
			return 0, true, c.errAt(e.Pos, "ws.Conn type not registered (internal error)")
		}
		argType, err := c.checkExpr(e.Args[0])
		if err != nil {
			return 0, true, err
		}
		if argType != wsConnType {
			return 0, true, c.errAt(e.Pos, "ws.receive() argument must be ws.Conn, got %s", typeName(argType))
		}
		return ast.TypeString, true, nil
	}

	// Special case: ws.close(conn)
	if e.Module == "ws" && e.Name == "close" {
		if len(e.Args) != 1 {
			return 0, true, c.errAt(e.Pos, "ws.close() takes exactly 1 argument, got %d", len(e.Args))
		}
		wsConnType, ok := ast.LookupStructType("Conn")
		if !ok {
			return 0, true, c.errAt(e.Pos, "ws.Conn type not registered (internal error)")
		}
		argType, err := c.checkExpr(e.Args[0])
		if err != nil {
			return 0, true, err
		}
		if argType != wsConnType {
			return 0, true, c.errAt(e.Pos, "ws.close() argument must be ws.Conn, got %s", typeName(argType))
		}
		return ast.TypeVoid, true, nil
	}

	// Special case: time.setTimeout(fn, ms) and time.setInterval(fn, ms)
	if e.Module == "time" && (e.Name == "setTimeout" || e.Name == "setInterval") {
		if len(e.Args) != 2 {
			return 0, true, c.errAt(e.Pos, "time.%s() takes exactly 2 arguments, got %d", e.Name, len(e.Args))
		}
		// Arg 0: function name (Ident or CallExpr with 0 args)
		var handlerName string
		switch h := e.Args[0].(type) {
		case *ast.Ident:
			handlerName = h.Name
		case *ast.CallExpr:
			if len(h.Args) != 0 {
				return 0, true, c.errAt(e.Pos, "time.%s() callback must take no arguments", e.Name)
			}
			handlerName = h.Name
			if h.Module != "" && c.userModules[h.Module] {
				handlerName = h.Module + "_" + h.Name
			}
		default:
			return 0, true, c.errAt(e.Pos, "time.%s() argument 1 must be a function name", e.Name)
		}
		sig, ok := c.funcs[handlerName]
		if !ok {
			return 0, true, c.errAt(e.Pos, "time.%s() callback '%s' is not a defined function", e.Name, handlerName)
		}
		if len(sig.Params) != 0 {
			return 0, true, c.errAt(e.Pos, "time.%s() callback '%s' must take no parameters", e.Name, handlerName)
		}
		// Arg 1: milliseconds (int)
		msType, err := c.checkExpr(e.Args[1])
		if err != nil {
			return 0, true, err
		}
		if msType != ast.TypeInt {
			return 0, true, c.errAt(e.Pos, "time.%s() argument 2 must be int, got %s", e.Name, typeName(msType))
		}
		if e.Name == "setInterval" {
			return ast.TypeInt, true, nil
		}
		return ast.TypeVoid, true, nil
	}

	// os.exec returning ExecResult
	if e.Module == "os" && e.Name == "exec" {
		execResultType, ok := ast.LookupStructType("ExecResult")
		if !ok {
			return 0, true, c.errAt(e.Pos, "ExecResult type not registered (internal error)")
		}
		if len(e.Args) != 1 {
			return 0, true, c.errAt(e.Pos, "os.exec() takes exactly 1 argument, got %d", len(e.Args))
		}
		argType, err := c.checkExpr(e.Args[0])
		if err != nil {
			return 0, true, err
		}
		if argType != ast.TypeString {
			return 0, true, c.errAt(e.Pos, "os.exec() argument must be string, got %s", typeName(argType))
		}
		e.ResolvedType = execResultType
		return execResultType, true, nil
	}

	// HTTP client functions returning HttpResponse
	if e.Module == "http" && (e.Name == "get" || e.Name == "post" || e.Name == "put" || e.Name == "patch" || e.Name == "delete" || e.Name == "request" || e.Name == "postForm") {
		httpRespType, ok := ast.LookupStructType("HttpResponse")
		if !ok {
			return 0, true, c.errAt(e.Pos, "HttpResponse type not registered (internal error)")
		}
		switch e.Name {
		case "get":
			if len(e.Args) < 1 || len(e.Args) > 2 {
				return 0, true, c.errAt(e.Pos, "http.get() takes 1-2 arguments, got %d", len(e.Args))
			}
			for i, arg := range e.Args {
				argType, err := c.checkExpr(arg)
				if err != nil {
					return 0, true, err
				}
				if argType != ast.TypeString {
					return 0, true, c.errAt(e.Pos, "http.get() argument %d must be string, got %s", i+1, typeName(argType))
				}
			}
		case "post", "put", "patch":
			if len(e.Args) < 2 || len(e.Args) > 3 {
				return 0, true, c.errAt(e.Pos, "http.%s() takes 2-3 arguments, got %d", e.Name, len(e.Args))
			}
			for i, arg := range e.Args {
				argType, err := c.checkExpr(arg)
				if err != nil {
					return 0, true, err
				}
				if argType != ast.TypeString {
					return 0, true, c.errAt(e.Pos, "http.%s() argument %d must be string, got %s", e.Name, i+1, typeName(argType))
				}
			}
		case "delete":
			if len(e.Args) < 1 || len(e.Args) > 2 {
				return 0, true, c.errAt(e.Pos, "http.delete() takes 1-2 arguments, got %d", len(e.Args))
			}
			for i, arg := range e.Args {
				argType, err := c.checkExpr(arg)
				if err != nil {
					return 0, true, err
				}
				if argType != ast.TypeString {
					return 0, true, c.errAt(e.Pos, "http.delete() argument %d must be string, got %s", i+1, typeName(argType))
				}
			}
		case "request":
			if len(e.Args) != 4 {
				return 0, true, c.errAt(e.Pos, "http.request() takes exactly 4 arguments, got %d", len(e.Args))
			}
			for i, arg := range e.Args {
				argType, err := c.checkExpr(arg)
				if err != nil {
					return 0, true, err
				}
				if argType != ast.TypeString {
					return 0, true, c.errAt(e.Pos, "http.request() argument %d must be string, got %s", i+1, typeName(argType))
				}
			}
		case "postForm":
			if len(e.Args) < 2 || len(e.Args) > 3 {
				return 0, true, c.errAt(e.Pos, "http.postForm() takes 2-3 arguments, got %d", len(e.Args))
			}
			for i, arg := range e.Args {
				argType, err := c.checkExpr(arg)
				if err != nil {
					return 0, true, err
				}
				if argType != ast.TypeString {
					return 0, true, c.errAt(e.Pos, "http.postForm() argument %d must be string, got %s", i+1, typeName(argType))
				}
			}
		}
		e.ResolvedType = httpRespType
		return httpRespType, true, nil
	}

	// HTTP header builder function returning string
	if e.Module == "http" && e.Name == "header" {
		if len(e.Args) != 3 {
			return 0, true, c.errAt(e.Pos, "http.header() takes exactly 3 arguments, got %d", len(e.Args))
		}
		for i, arg := range e.Args {
			argType, err := c.checkExpr(arg)
			if err != nil {
				return 0, true, err
			}
			if argType != ast.TypeString {
				return 0, true, c.errAt(e.Pos, "http.header() argument %d must be string, got %s", i+1, typeName(argType))
			}
		}
		return ast.TypeString, true, nil
	}

	// Special case: http.listen(port) or http.listen(port, workers)
	if e.Module == "http" && e.Name == "listen" {
		if len(e.Args) < 1 || len(e.Args) > 2 {
			return 0, true, c.errAt(e.Pos, "http.listen() takes 1-2 arguments, got %d", len(e.Args))
		}
		for i, arg := range e.Args {
			argType, err := c.checkExpr(arg)
			if err != nil {
				return 0, true, err
			}
			if argType != ast.TypeInt {
				return 0, true, c.errAt(e.Pos, "http.listen() argument %d must be int, got %s", i+1, typeName(argType))
			}
		}
		return ast.TypeVoid, true, nil
	}

	// HTTP form builder functions returning string
	if e.Module == "http" && (e.Name == "formNew" || e.Name == "formField" || e.Name == "formFile") {
		switch e.Name {
		case "formNew":
			if len(e.Args) != 0 {
				return 0, true, c.errAt(e.Pos, "http.formNew() takes no arguments, got %d", len(e.Args))
			}
		case "formField":
			if len(e.Args) != 3 {
				return 0, true, c.errAt(e.Pos, "http.formField() takes exactly 3 arguments, got %d", len(e.Args))
			}
			for i, arg := range e.Args {
				argType, err := c.checkExpr(arg)
				if err != nil {
					return 0, true, err
				}
				if argType != ast.TypeString {
					return 0, true, c.errAt(e.Pos, "http.formField() argument %d must be string, got %s", i+1, typeName(argType))
				}
			}
		case "formFile":
			if len(e.Args) != 3 {
				return 0, true, c.errAt(e.Pos, "http.formFile() takes exactly 3 arguments, got %d", len(e.Args))
			}
			for i, arg := range e.Args {
				argType, err := c.checkExpr(arg)
				if err != nil {
					return 0, true, err
				}
				if argType != ast.TypeString {
					return 0, true, c.errAt(e.Pos, "http.formFile() argument %d must be string, got %s", i+1, typeName(argType))
				}
			}
		}
		return ast.TypeString, true, nil
	}

	return 0, false, nil
}
