package stdlib

// SpecialSignature returns the human-readable parameter string and return type
// for polymorphic stdlib functions (those with Params == nil), and for
// http.route, whose handler is typed string in its FuncDef but accepts any of
// the handler shapes checkHTTPHandler allows. This is the
// single source of truth used by the LSP, docgen, and any other tool that
// needs to display correct signatures for these special-cased functions.
//
// Returns ("", "", false) if the function is not special-cased, meaning the
// caller should fall back to the normal Params-based rendering.
func SpecialSignature(moduleName, funcName string, fd *FuncDef) (params, ret string, ok bool) {
	if moduleName == "http" && funcName == "route" {
		return "method: string, path: string, handler: fn", "void", true
	}
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
			return "value: T[]|struct|map[string, V]|struct?|T[]?", "string", true
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
		case "fieldName", "fieldKind", "fieldType":
			return "value: struct|&struct, index: int", "string", true
		case "fieldTag":
			return "value: struct|&struct, index: int, key: string", "string", true
		case "fieldTags":
			return "value: struct|&struct, key: string", "string[]", true
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
		// A setter's subject has to be writable, so a by-value struct parameter
		// is not one of the forms it accepts — see checkReflectSetterSubject.
		case "setInt":
			return "value: struct|&struct, index: int, to: int", "bool", true
		case "setLong":
			return "value: struct|&struct, index: int, to: long", "bool", true
		case "setDouble":
			return "value: struct|&struct, index: int, to: double", "bool", true
		case "setBool":
			return "value: struct|&struct, index: int, to: bool", "bool", true
		case "setString":
			return "value: struct|&struct, index: int, to: string", "bool", true
		}

	case "db":
		switch funcName {
		case "col":
			return "int, int", "int|long|string|double|bool", true
		case "fill":
			return "into: struct|&struct, result: int", "void", true
		}

	case "http":
		switch funcName {
		case "listen":
			return "port: int, [workers: int]", "void", true
		case "listenOn":
			return "host: string, port: int, [workers: int]", "void", true
		case "notFound":
			return "handler: fn", "void", true
		case "afterBatch":
			return "hook: fn(): bool", "void", true
		case "lanes":
			return "count: int, serve: fn(lane: int, payloads: string[]): void", "void", true
		case "response":
			return "statusCode: int, body: string, contentType: string", "HttpResponse", true
		case "responseWith":
			return "statusCode: int, body: string, contentType: string, headers: string", "HttpResponse", true
		case "get", "delete":
			return "url: string, [headers: string]", "HttpResponse", true
		case "post", "put", "patch":
			return "url: string, body: string, [headers: string]", "HttpResponse", true
		case "request":
			return "method: string, url: string, body: string, headers: string", "HttpResponse", true
		case "requestWith":
			return "method: string, url: string, body: string, headers: string, options: HttpOptions", "HttpResponse", true
		case "stream":
			return "method: string, url: string, body: string, headers: string, options: HttpOptions, onChunk: fn(string): bool", "HttpResponse", true
		case "streamEvents":
			return "method: string, url: string, body: string, headers: string, options: HttpOptions, onEvent: fn(string): bool", "HttpResponse", true
		case "header":
			return "headers: string, key: string, value: string", "string", true
		case "formNew":
			return "", "string", true
		case "formField":
			return "form: string, name: string, value: string", "string", true
		case "formFile":
			return "form: string, name: string, path: string", "string", true
		case "postForm":
			return "url: string, form: string, [headers: string]", "HttpResponse", true
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
