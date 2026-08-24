package stdlib

// SpecialSignature returns the human-readable parameter string and return type
// for polymorphic stdlib functions (those with Params == nil). This is the
// single source of truth used by the LSP, docgen, and any other tool that
// needs to display correct signatures for these special-cased functions.
//
// Returns ("", "", false) if the function is not special-cased, meaning the
// caller should fall back to the normal Params-based rendering.
func SpecialSignature(moduleName, funcName string, fd *FuncDef) (params, ret string, ok bool) {
	if fd.Params != nil {
		return "", "", false
	}

	switch moduleName {
	case "fmt":
		switch funcName {
		case "print", "println":
			return "int|long|double|string|bool", "void", true
		}

	case "json":
		switch funcName {
		case "set":
			return "string, string, int|long|double|string|bool", "string", true
		case "stringify":
			return "value: T[]|struct|map[string, V]", "string", true
		case "objectify":
			return "json: string", "T (struct)", true
		case "arrayPush":
			return "arr: string, value: int|long|double|string|bool", "string", true
		}

	case "db":
		switch funcName {
		case "col":
			return "int, int", "int|string|double|bool", true
		}

	case "http":
		switch funcName {
		case "listen":
			return "port: int, [workers: int]", "void", true
		case "response":
			return "statusCode: int, body: string, contentType: string", "HttpResponse", true
		case "get":
			return "string, [string]", "HttpResponse", true
		case "post", "put", "patch":
			return "string, string, [string]", "HttpResponse", true
		case "delete":
			return "string, [string]", "HttpResponse", true
		case "request":
			return "string, string, string, string", "HttpResponse", true
		case "header":
			return "string, string, string", "string", true
		case "formNew":
			return "", "string", true
		case "formField", "formFile":
			return "string, string, string", "string", true
		case "postForm":
			return "string, string, [string]", "HttpResponse", true
		}

	case "time":
		switch funcName {
		case "setTimeout":
			return "fn: fn(): void, ms: int", "void", true
		case "setInterval":
			return "fn: fn(): void, ms: int", "int", true
		}

	case "ws":
		switch funcName {
		case "handleMessage":
			return "handler: fn(ws.Conn, string): void", "void", true
		case "connect":
			return "url: string", "Conn", true
		case "send":
			return "conn: Conn, msg: string", "void", true
		case "receive":
			return "conn: Conn", "string", true
		case "close":
			return "conn: Conn", "void", true
		case "handleConnect":
			return "handler: fn(ws.Conn, string): void", "void", true
		case "handleDisconnect":
			return "handler: fn(ws.Conn): void", "void", true
		}

	case "os":
		switch funcName {
		case "exec":
			return "command: string", "ExecResult", true
		}
	}

	return "", "", false
}
