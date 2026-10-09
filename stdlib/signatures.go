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
		case "encode":
			return "value: T[]|struct|map[string, V]", "string", true
		case "decode":
			return "json: string", "T | T? | T[] | T[]?  (struct target; T? and T[]? are checked)", true
		case "setArray":
			return "json: string, key: string, arr: T[]", "string", true
		case "arrayPush":
			return "arr: string, value: int|long|double|string|bool", "string", true
		}

	case "reflect":
		// Every function here is polymorphic in its subject — any struct, by
		// value or by reference — so the signature is written out rather than
		// derived from Params.
		switch funcName {
		case "typeName":
			return "value: struct|&struct", "string", true
		case "fieldCount":
			return "value: struct|&struct", "int", true
		case "fields":
			return "value: struct|&struct", "string[]", true
		case "fieldName", "fieldKind":
			return "value: struct|&struct, index: int", "string", true
		case "indexOf":
			return "value: struct|&struct, name: string", "int", true
		case "has":
			return "value: struct|&struct, name: string", "bool", true
		case "asInt":
			return "value: struct|&struct, index: int", "int", true
		case "asLong":
			return "value: struct|&struct, index: int", "long", true
		case "asDouble":
			return "value: struct|&struct, index: int", "double", true
		case "asBool":
			return "value: struct|&struct, index: int", "bool", true
		case "asString", "toString":
			return "value: struct|&struct, index: int", "string", true
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
