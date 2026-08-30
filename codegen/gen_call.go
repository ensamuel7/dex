package codegen

import (
	"fmt"
	"strings"

	"github.com/ensamuel7/dex/ast"
	"github.com/ensamuel7/dex/stdlib"
)

func (g *Generator) genCallExpr(out *strings.Builder, e *ast.CallExpr) {
	// json.Value method calls: v.asInt(), v.has("k"), etc. A named receiver is
	// normalised into a receiver expression so both spellings take one path.
	if e.Recv != nil && g.typeOfExpr(e.Recv) == ast.TypeJsonValue {
		if g.genJsonValueMethodExpr(out, e.Recv, e.Name, e.Args, e.ResolvedType) {
			return
		}
	}
	if e.Recv == nil && e.Module != "" {
		// A plain variable, or a dotted field path such as msg.payload.
		recvType := g.varTypes[e.Module]
		if recvType != ast.TypeJsonValue && strings.Contains(e.Module, ".") {
			recvType = g.resolveFieldChainType(e.Module)
		}
		if recvType == ast.TypeJsonValue {
			recv := &ast.Ident{Pos: e.Pos, Name: e.Module}
			if g.genJsonValueMethodExpr(out, recv, e.Name, e.Args, e.ResolvedType) {
				return
			}
		}
	}

	// StringBuilder constructor
	if e.Name == "StringBuilder" && e.Module == "" && !e.IsConstructor {
		out.WriteString("dex_sb_new()")
		return
	}

	// Constructor call: emit struct literal from positional args
	if e.IsConstructor {
		structDef := ast.GetStructDef(e.StructType)
		out.WriteString(fmt.Sprintf("(Dex_%s){ ", structDef.Name))
		for i, cp := range structDef.ConstructorParams {
			if i > 0 {
				out.WriteString(", ")
			}
			out.WriteString(fmt.Sprintf(".%s = ", cp.Name))
			if ast.IsRefType(cp.Type) {
				argType := g.typeOfExpr(e.Args[i])
				if !ast.IsRefType(argType) {
					out.WriteString("&")
					g.genExpr(out, e.Args[i])
				} else if ident, ok := e.Args[i].(*ast.Ident); ok && isPrimitiveRef(argType) {
					out.WriteString(ident.Name)
				} else {
					g.genExpr(out, e.Args[i])
				}
			} else {
				g.genExpr(out, e.Args[i])
			}
		}
		out.WriteString(" }")
		return
	}

	// Method call: emit flattened function with instance as first arg
	if e.IsMethodCall {
		structDef := ast.GetStructDef(e.StructType)
		flatName := structDef.Name + "_" + e.Name
		// Check if struct belongs to a user module (needs module prefix)
		if modName, ok := g.structModules[structDef.Name]; ok {
			flatName = modName + "_" + flatName
		}
		out.WriteString(flatName)
		out.WriteString("(")
		// If instance is a ref type, dereference for value self param
		instanceType := g.resolveFieldChainType(e.Module)
		if ast.IsRefType(instanceType) {
			out.WriteString("*")
		}
		out.WriteString(e.Module) // the instance variable name
		for _, arg := range e.Args {
			out.WriteString(", ")
			// A method borrows its arguments just as a free function does, so an
			// argument built on the spot is released once the statement ends.
			g.genBorrowed(out, arg)
		}
		out.WriteString(")")
		return
	}

	// User module call: emit prefixed function name.
	// A local or global of the same name shadows the module — `chargers.push(x)`
	// on a variable called chargers is a method call, not a call into a module
	// that happens to share the name.
	_, shadowedByVar := g.varTypes[e.Module]
	if e.Module != "" && g.userModules[e.Module] && !shadowedByVar {
		out.WriteString(e.Module + "_" + e.Name)
		out.WriteString("(")
		modFn, hasModFn := g.funcs[e.Module+"_"+e.Name]
		for i, arg := range e.Args {
			if i > 0 {
				out.WriteString(", ")
			}
			if hasModFn && i < len(modFn.Params) && ast.IsRefType(modFn.Params[i].Type) {
				argType := g.typeOfExpr(arg)
				if !ast.IsRefType(argType) {
					out.WriteString("&")
					g.genExpr(out, arg)
				} else if ident, ok := arg.(*ast.Ident); ok && isPrimitiveRef(argType) {
					out.WriteString(ident.Name)
				} else {
					g.genExpr(out, arg)
				}
			} else {
				// The callee borrows its argument, exactly as for a call to a
				// function in the same file, so an argument that allocates is
				// released once the statement finishes rather than leaking.
				g.genBorrowed(out, arg)
			}
		}
		out.WriteString(")")
		return
	}

	// Special case: fmt.print / fmt.println — polymorphic print for any type
	if e.Module == "fmt" && (e.Name == "print" || e.Name == "println") {
		newline := e.Name == "println"
		argType := g.typeOfExpr(e.Args[0])
		// Auto-unwrap ref types for printing (e.g., &User → User)
		if ast.IsRefType(argType) {
			argType = ast.RefInnerType(argType)
		}
		if ast.IsArrayType(argType) {
			g.genPrintArray(out, e.Args[0], argType, newline)
			return
		}
		if ast.IsStructType(argType) {
			g.genPrintStruct(out, e.Args[0], argType, newline)
			return
		}
		nl := ""
		if newline {
			nl = "\\n"
		}
		var fmtStr string
		switch argType {
		case ast.TypeChar:
			fmtStr = "%c"
		case ast.TypeInt:
			fmtStr = "%d"
		case ast.TypeLong:
			fmtStr = "%ld"
		case ast.TypeDouble:
			fmtStr = "%f"
		case ast.TypeString:
			out.WriteString(fmt.Sprintf("printf(\"%%s%s\", ", nl))
			g.genBorrowed(out, e.Args[0])
			out.WriteString("->data)")
			return
		case ast.TypeBool:
			// Print bools as "true"/"false"
			out.WriteString(fmt.Sprintf("printf(\"%%s%s\", ", nl))
			out.WriteString("(")
			g.genExpr(out, e.Args[0])
			out.WriteString(") ? \"true\" : \"false\")")
			return
		default:
			fmtStr = "%d"
		}
		out.WriteString(fmt.Sprintf("printf(\"%s%s\", ", fmtStr, nl))
		g.genExpr(out, e.Args[0])
		out.WriteString(")")
		return
	}
	if g.genJsonCall(out, e) {
		return
	}

	// db.col(rows, col) — polymorphic: dispatch by resolved return type
	if e.Module == "db" && e.Name == "col" {
		var fn string
		switch e.ResolvedType {
		case ast.TypeString:
			fn = "dex_db_col_str"
		case ast.TypeBool:
			fn = "dex_db_col_bool"
		case ast.TypeDouble:
			fn = "dex_db_col_double"
		default:
			fn = "dex_db_col_int"
		}
		if e.ResolvedType == ast.TypeString {
			out.WriteString("dex_db_col_dexstr(")
			g.genExpr(out, e.Args[0])
			out.WriteString(", ")
			g.genExpr(out, e.Args[1])
			out.WriteString(")")
		} else {
			out.WriteString(fn + "(")
			g.genExpr(out, e.Args[0])
			out.WriteString(", ")
			g.genExpr(out, e.Args[1])
			out.WriteString(")")
		}
		return
	}

	if g.genWebCall(out, e) {
		return
	}

	// time.setTimeout / time.setInterval — resolve function name, emit C call
	if e.Module == "time" && (e.Name == "setTimeout" || e.Name == "setInterval") {
		var handlerName string
		switch h := e.Args[0].(type) {
		case *ast.Ident:
			handlerName = h.Name
		case *ast.CallExpr:
			handlerName = h.Name
			if h.Module != "" && g.userModules[h.Module] {
				handlerName = h.Module + "_" + h.Name
			}
		}
		if e.Name == "setTimeout" {
			out.WriteString("dex_time_set_timeout(")
		} else {
			out.WriteString("dex_time_set_interval(")
		}
		out.WriteString(handlerName)
		out.WriteString(", ")
		g.genExpr(out, e.Args[1])
		out.WriteString(")")
		return
	}

	// os.exec — returns ExecResult struct
	if e.Module == "os" && e.Name == "exec" {
		out.WriteString("dex_os_exec(")
		g.genStringArg(out, e.Args[0])
		out.WriteString(")")
		return
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
			return
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
			return
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
			return
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
			return
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
			return
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
			return
		case "header":
			out.WriteString("dex_string_from_cstr(dex_http_header(")
			g.genStringArg(out, e.Args[0])
			out.WriteString(", ")
			g.genStringArg(out, e.Args[1])
			out.WriteString(", ")
			g.genStringArg(out, e.Args[2])
			out.WriteString("))")
			return
		case "formNew":
			out.WriteString("dex_string_from_cstr(dex_http_form_new())")
			return
		case "formField":
			out.WriteString("dex_string_from_cstr(dex_http_form_field(")
			g.genStringArg(out, e.Args[0])
			out.WriteString(", ")
			g.genStringArg(out, e.Args[1])
			out.WriteString(", ")
			g.genStringArg(out, e.Args[2])
			out.WriteString("))")
			return
		case "formFile":
			out.WriteString("dex_string_from_cstr(dex_http_form_file(")
			g.genStringArg(out, e.Args[0])
			out.WriteString(", ")
			g.genStringArg(out, e.Args[1])
			out.WriteString(", ")
			g.genStringArg(out, e.Args[2])
			out.WriteString("))")
			return
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
			return
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
			return
		}
	}

	// Built-in: close(channel)
	if e.Module == "" && e.Name == "close" {
		out.WriteString("dex_chan_close(")
		g.genExpr(out, e.Args[0])
		out.WriteString(")")
		return
	}

	// Built-in: assert(condition)
	if e.Module == "" && e.Name == "assert" {
		out.WriteString("if (!(")
		g.genExpr(out, e.Args[0])
		out.WriteString(")) { fprintf(stderr, \"FAIL: assert failed\\n\"); exit(1); }")
		return
	}

	// Check if this is a mutex method call (direct variable or field chain)
	if e.Module != "" {
		isMutex := false
		if t, ok := g.varTypes[e.Module]; ok && t == ast.TypeMutex {
			isMutex = true
		}
		if !isMutex && strings.Contains(e.Module, ".") {
			chainType := g.resolveFieldChainType(e.Module)
			if chainType == ast.TypeMutex {
				isMutex = true
			}
		}
		if isMutex {
			// Resolve dotted path with correct accessor (-> for ref types, . for value types)
			cPath := g.resolveFieldChainC(e.Module)
			switch e.Name {
			case "lock":
				out.WriteString(fmt.Sprintf("pthread_mutex_lock(&%s)", cPath))
				return
			case "unlock":
				out.WriteString(fmt.Sprintf("pthread_mutex_unlock(&%s)", cPath))
				return
			}
		}
	}

	// Check if this is a StringBuilder method call
	if e.Module != "" && g.sbVars[e.Module] {
		switch e.Name {
		case "append":
			argType := g.typeOfExpr(e.Args[0])
			var fn string
			switch argType {
			case ast.TypeString:
				fn = "dex_sb_append_str"
			case ast.TypeInt:
				fn = "dex_sb_append_int"
			case ast.TypeLong:
				fn = "dex_sb_append_long"
			case ast.TypeDouble:
				fn = "dex_sb_append_double"
			case ast.TypeBool:
				fn = "dex_sb_append_bool"
			case ast.TypeChar:
				fn = "dex_sb_append_char"
			default:
				fn = "dex_sb_append_str"
			}
			// append copies the bytes out of its argument, so an allocating
			// argument is borrowed and released after the statement.
			out.WriteString(fmt.Sprintf("%s(%s, ", fn, e.Module))
			g.genBorrowed(out, e.Args[0])
			out.WriteString(")")
			return
		case "toString":
			out.WriteString(fmt.Sprintf("dex_sb_toString(%s)", e.Module))
			return
		case "len":
			out.WriteString(fmt.Sprintf("dex_sb_len(%s)", e.Module))
			return
		case "clear":
			out.WriteString(fmt.Sprintf("dex_sb_clear(%s)", e.Module))
			return
		}
	}

	// Check if this is a map method call (e.Module is a variable name or field chain)
	if e.Module != "" {
		mapType, isMap := g.mapVars[e.Module]
		// Field chain resolution: e.g., self.myMap → look up field type
		if !isMap && strings.Contains(e.Module, ".") {
			chainType := g.resolveFieldChainType(e.Module)
			if ast.IsMapType(chainType) {
				mapType = chainType
				isMap = true
			}
		}
		if isMap {
			// The emission below spells the receiver straight into the C, so a
			// field of a by-reference parameter needs its accessor corrected:
			// `c.args` on a `&Clause` is `c->args`. Done here rather than at
			// the top of the function because the type lookup above keys off
			// the source spelling, and rewriting it earlier loses the variable.
			if resolved := g.resolveFieldChainC(e.Module); resolved != e.Module {
				clone := *e
				clone.Module = resolved
				e = &clone
			}
			suffix := g.mapSuffix(mapType)
			// The map retains whatever it stores and only borrows lookup keys, so
			// every argument here is passed borrowed: an allocating key or value
			// is hoisted and released once the statement ends.
			switch e.Name {
			case "set":
				out.WriteString(fmt.Sprintf("dex_map_%s_set(%s, ", suffix, e.Module))
				g.genBorrowed(out, e.Args[0])
				out.WriteString(", ")
				g.genBorrowed(out, e.Args[1])
				out.WriteString(")")
				return
			case "get":
				out.WriteString(fmt.Sprintf("dex_map_%s_get(%s, ", suffix, e.Module))
				g.genBorrowed(out, e.Args[0])
				out.WriteString(")")
				return
			case "has":
				out.WriteString(fmt.Sprintf("dex_map_%s_has(%s, ", suffix, e.Module))
				g.genBorrowed(out, e.Args[0])
				out.WriteString(")")
				return
			case "remove":
				out.WriteString(fmt.Sprintf("dex_map_%s_remove(%s, ", suffix, e.Module))
				g.genBorrowed(out, e.Args[0])
				out.WriteString(")")
				return
			case "len":
				out.WriteString(fmt.Sprintf("dex_map_%s_len(%s)", suffix, e.Module))
				return
			case "clear":
				out.WriteString(fmt.Sprintf("dex_map_%s_clear(%s)", suffix, e.Module))
				return
			case "keys":
				out.WriteString(fmt.Sprintf("dex_map_%s_keys(%s)", suffix, e.Module))
				return
			case "values":
				out.WriteString(fmt.Sprintf("dex_map_%s_values(%s)", suffix, e.Module))
				return
			}
		}
	}

	// Check if this is an array method call (e.Module is a variable name or field chain)
	if e.Module != "" {
		arrType, ok := g.arrVars[e.Module]
		// Field chain resolution: e.g., charger.connectors → look up field type
		if !ok && strings.Contains(e.Module, ".") {
			chainType := g.resolveFieldChainType(e.Module)
			if ast.IsArrayType(chainType) {
				arrType = chainType
				ok = true
			}
		}
		if ok {
			// The emission below spells the receiver straight into the C, so a
			// field of a by-reference parameter needs its accessor corrected:
			// `c.args` on a `&Clause` is `c->args`. Done here rather than at
			// the top of the function because the type lookup above keys off
			// the source spelling, and rewriting it earlier loses the variable.
			if resolved := g.resolveFieldChainC(e.Module); resolved != e.Module {
				clone := *e
				clone.Module = resolved
				e = &clone
			}
			if ast.IsStructArrayType(arrType) {
				// Struct array methods
				elemType := ast.ElementType(arrType)
				elemCType := g.cType(elemType)
				switch e.Name {
				case "push":
					out.WriteString(fmt.Sprintf("{ %s _push_tmp = ", elemCType))
					g.genExpr(out, e.Args[0])
					out.WriteString("; ")
					// Retain heap fields before push — but only borrowed ones.
					// New allocations (string literals, function calls) already have
					// refcount=1 which transfers to the array via memcpy.
					def := ast.GetStructDef(elemType)
					if def != nil {
						if structLit, ok := e.Args[0].(*ast.StructLitExpr); ok {
							// Struct literal: check each field individually
							fieldValueMap := make(map[string]ast.Expr, len(structLit.FieldNames))
							for i, fn := range structLit.FieldNames {
								fieldValueMap[fn] = structLit.FieldValues[i]
							}
							for _, f := range def.Fields {
								if ast.IsHeapType(f.Type) {
									if valExpr, found := fieldValueMap[f.Name]; found {
										if !g.isNewAlloc(valExpr) {
											out.WriteString(fmt.Sprintf("dex_retain(_push_tmp.%s); ", f.Name))
										}
									}
								}
							}
						} else if !g.isNewAlloc(e.Args[0]) {
							// A borrowed struct — a variable, or a field of one —
							// is still owned by whoever it came from, so the array
							// takes its own reference to each heap field.
							for _, f := range def.Fields {
								if ast.IsHeapType(f.Type) {
									out.WriteString(fmt.Sprintf("dex_retain(_push_tmp.%s); ", f.Name))
								}
							}
						}
						// A struct the expression just produced — the result of a
						// call, say — already owns its fields, and that ownership
						// transfers to the array. Retaining here would leave every
						// field one reference above zero forever.
					}
					out.WriteString(fmt.Sprintf("dex_array_struct_push(%s, &_push_tmp); }", e.Module))
					return
				case "len":
					out.WriteString(fmt.Sprintf("%s->len", e.Module))
					return
				case "pop":
					out.WriteString(fmt.Sprintf("dex_array_struct_pop(%s)", e.Module))
					return
				case "remove":
					out.WriteString(fmt.Sprintf("dex_array_struct_remove(%s, ", e.Module))
					g.genExpr(out, e.Args[0])
					out.WriteString(")")
					return
				case "reverse":
					out.WriteString(fmt.Sprintf("dex_array_struct_reverse(%s)", e.Module))
					return
				}
			} else {
				switch e.Name {
				case "push":
					pushFn := g.arrayPushFunc(arrType)
					out.WriteString(fmt.Sprintf("%s(%s, ", pushFn, e.Module))
					g.genExpr(out, e.Args[0])
					out.WriteString(")")
					return
				case "len":
					out.WriteString(fmt.Sprintf("%s->len", e.Module))
					return
				case "pop":
					popFn := g.arrayPopFunc(arrType)
					out.WriteString(fmt.Sprintf("%s(%s)", popFn, e.Module))
					return
				case "remove":
					removeFn := g.arrayRemoveFunc(arrType)
					out.WriteString(fmt.Sprintf("%s(%s, ", removeFn, e.Module))
					g.genExpr(out, e.Args[0])
					out.WriteString(")")
					return
				case "contains":
					containsFn := g.arrayContainsFunc(arrType)
					out.WriteString(fmt.Sprintf("%s(%s, ", containsFn, e.Module))
					g.genExpr(out, e.Args[0])
					out.WriteString(")")
					return
				case "indexOf":
					indexOfFn := g.arrayIndexOfFunc(arrType)
					out.WriteString(fmt.Sprintf("%s(%s, ", indexOfFn, e.Module))
					g.genExpr(out, e.Args[0])
					out.WriteString(")")
					return
				case "reverse":
					reverseFn := g.arrayReverseFunc(arrType)
					out.WriteString(fmt.Sprintf("%s(%s)", reverseFn, e.Module))
					return
				case "sort":
					sortArg := e.Args[0].(*ast.StringLit).Value
					var sortFn string
					if sortArg == "asc" {
						sortFn = g.arraySortAscFunc(arrType)
					} else {
						sortFn = g.arraySortDescFunc(arrType)
					}
					out.WriteString(fmt.Sprintf("%s(%s)", sortFn, e.Module))
					return
				}
			}
		}
	}

	// String method calls: s.len(), s.contains(), etc. The receiver is a plain
	// variable, or a field holding a string — cmd.action.isEmpty() — in which case
	// it is rendered as the C that reads that field.
	if e.Module != "" {
		recv := e.Module
		isString := g.strVars[e.Module]
		if !isString && strings.Contains(e.Module, ".") && g.resolveFieldChainType(e.Module) == ast.TypeString {
			isString = true
			recv = g.fieldChainC(e.Module)
		}
		if isString {
			g.usesStringMethods = true
			switch e.Name {
			case "len":
				out.WriteString(fmt.Sprintf("dex_str_len(%s)", recv))
				return
			case "contains":
				g.genStrMethodWithArgs(out, "dex_str_contains", recv, e.Args[:1])
				return
			case "startsWith":
				g.genStrMethodWithArgs(out, "dex_str_startsWith", recv, e.Args[:1])
				return
			case "endsWith":
				g.genStrMethodWithArgs(out, "dex_str_endsWith", recv, e.Args[:1])
				return
			case "indexOf":
				g.genStrMethodWithArgs(out, "dex_str_indexOf", recv, e.Args[:1])
				return
			case "toLower":
				out.WriteString(fmt.Sprintf("dex_str_toLower(%s)", recv))
				return
			case "toUpper":
				out.WriteString(fmt.Sprintf("dex_str_toUpper(%s)", recv))
				return
			case "trim":
				out.WriteString(fmt.Sprintf("dex_str_trim(%s)", recv))
				return
			case "split":
				g.genStrMethodWithArgs(out, "dex_str_split", recv, e.Args[:1])
				return
			case "substring":
				out.WriteString(fmt.Sprintf("dex_str_substring(%s, ", recv))
				g.genExpr(out, e.Args[0])
				out.WriteString(", ")
				g.genExpr(out, e.Args[1])
				out.WriteString(")")
				return
			case "replace":
				g.genStrMethodWithArgs(out, "dex_str_replace", recv, e.Args[:2])
				return
			case "charAt":
				out.WriteString(fmt.Sprintf("dex_str_charAt(%s, ", recv))
				g.genExpr(out, e.Args[0])
				out.WriteString(")")
				return
			case "isAlphanumeric", "isAlpha", "isDigit", "isNumeric", "isWhitespace", "isEmpty",
				"containsUppercase", "containsLowercase", "containsDigit":
				out.WriteString(fmt.Sprintf("dex_str_%s(%s)", e.Name, recv))
				return
			}
		}
	}

	// Qualified call with CName — look up from stdlib
	if e.Module != "" {
		funcDef, ok := stdlib.LookupFunc(e.Module, e.Name)
		if ok && funcDef.CName != "" {
			// Check if function returns string — needs wrapping.
			// A RawString function has already built a DexString with the right
			// length, so wrapping it would strlen away the bytes it exists to
			// preserve.
			if funcDef.ReturnType == ast.TypeString && !funcDef.RawReturn {
				out.WriteString("dex_string_from_cstr(")
				out.WriteString(funcDef.CName)
				out.WriteString("(")
				for i, arg := range e.Args {
					if i > 0 {
						out.WriteString(", ")
					}
					g.genStdlibArg(out, arg, funcDef, i)
				}
				out.WriteString("))")
			} else {
				out.WriteString(funcDef.CName)
				out.WriteString("(")
				for i, arg := range e.Args {
					if i > 0 {
						out.WriteString(", ")
					}
					g.genStdlibArg(out, arg, funcDef, i)
				}
				out.WriteString(")")
			}
			return
		}
	}

	// Call through a function value: the receiver holds the code pointer and the
	// environment it was built with.
	if e.Module == "" {
		if t, isVar := g.varTypes[e.Name]; isVar && ast.IsFuncType(t) {
			g.genClosureCall(out, e.Name, t, e.Args)
			return
		}
	}

	// User-defined function call
	out.WriteString(e.Name)
	out.WriteString("(")
	fn, hasFn := g.funcs[e.Name]
	for i, arg := range e.Args {
		if i > 0 {
			out.WriteString(", ")
		}
		// Wrap argument for optional parameters if needed
		if hasFn && i < len(fn.Params) && ast.IsOptionalType(fn.Params[i].Type) {
			g.genOptionalArg(out, arg, fn.Params[i].Type)
		} else if hasFn && i < len(fn.Params) && ast.IsRefType(fn.Params[i].Type) {
			// Insert & when passing value to ref-typed param
			// but not if the arg is already a ref type
			argType := g.typeOfExpr(arg)
			if !ast.IsRefType(argType) {
				out.WriteString("&")
				g.genExpr(out, arg)
			} else if ident, ok := arg.(*ast.Ident); ok && isPrimitiveRef(argType) {
				// Primitive ref passed to ref param: emit raw pointer name
				out.WriteString(ident.Name)
			} else {
				g.genExpr(out, arg)
			}
		} else {
			// The callee borrows its argument, so an argument that allocates is
			// hoisted and released after the statement rather than leaking.
			g.genBorrowed(out, arg)
		}
	}
	out.WriteString(")")
}

func (g *Generator) genOptionalArg(out *strings.Builder, arg ast.Expr, paramType ast.Type) {
	inner := ast.OptionalInnerType(paramType)
	_, isNull := arg.(*ast.NullLit)
	argType := g.typeOfExpr(arg)

	// If the argument is already the optional type, emit directly
	if argType == paramType {
		g.genExpr(out, arg)
		return
	}

	if ast.IsValueType(inner) {
		ctyp := g.cType(paramType)
		if isNull {
			out.WriteString(fmt.Sprintf("(%s){0}", ctyp))
		} else {
			out.WriteString(fmt.Sprintf("(%s){1, ", ctyp))
			g.genExpr(out, arg)
			out.WriteString("}")
		}
	} else {
		// Heap/struct optional: NULL for null, value otherwise
		if isNull {
			out.WriteString("NULL")
		} else {
			g.genExpr(out, arg)
		}
	}
}

// genOwnedStringArg generates a string argument that transfers ownership (+1).
// If the expression is already a new allocation (+1 from call/literal/concat),
// it is emitted directly. Otherwise (borrowed ref like variable, field, index),
// it emits a retain to create an owned copy.
func (g *Generator) genOwnedStringArg(out *strings.Builder, arg ast.Expr) {
	if g.isNewAlloc(arg) {
		g.genExpr(out, arg)
	} else {
		tmp := g.nextTemp()
		out.WriteString(fmt.Sprintf("({ DexString* %s = ", tmp))
		g.genExpr(out, arg)
		out.WriteString(fmt.Sprintf("; dex_retain(%s); %s; })", tmp, tmp))
	}
}

// genStringArg generates a string argument for a stdlib function, extracting ->data
func (g *Generator) genStringArg(out *strings.Builder, arg ast.Expr) {
	argType := g.typeOfExpr(arg)
	if argType == ast.TypeString {
		if strLit, ok := arg.(*ast.StringLit); ok {
			// String literal — just use C string directly
			out.WriteString(fmt.Sprintf("%q", strLit.Value))
		} else {
			g.genExpr(out, arg)
			out.WriteString("->data")
		}
	} else {
		g.genExpr(out, arg)
	}
}

// genStrMethodWithArgs generates a string method call, wrapping any string literal
// arguments in a statement-expression that releases the temp after the call.
func (g *Generator) genStrMethodWithArgs(out *strings.Builder, cFunc, receiver string, args []ast.Expr) {
	// Check if any arg is a string literal (produces +1 ref that would leak)
	hasLitArg := false
	for _, arg := range args {
		if _, ok := arg.(*ast.StringLit); ok {
			hasLitArg = true
			break
		}
	}
	if !hasLitArg {
		// No string literal temps — emit directly
		out.WriteString(fmt.Sprintf("%s(%s", cFunc, receiver))
		for _, arg := range args {
			out.WriteString(", ")
			g.genExpr(out, arg)
		}
		out.WriteString(")")
		return
	}
	// Wrap in statement-expression to release string literal temps
	out.WriteString("({ ")
	var tmpNames []string
	for _, arg := range args {
		tmp := g.nextTemp()
		tmpNames = append(tmpNames, tmp)
		if _, ok := arg.(*ast.StringLit); ok {
			out.WriteString(fmt.Sprintf("DexString* %s = ", tmp))
		} else {
			// Non-literal args: capture the type from the expression
			out.WriteString(fmt.Sprintf("DexString* %s = ", tmp))
		}
		g.genExpr(out, arg)
		out.WriteString("; ")
	}
	resTmp := g.nextTemp()
	out.WriteString(fmt.Sprintf("__auto_type %s = %s(%s", resTmp, cFunc, receiver))
	for _, tmp := range tmpNames {
		out.WriteString(fmt.Sprintf(", %s", tmp))
	}
	out.WriteString("); ")
	// Release only the string literal temps
	for i, arg := range args {
		if _, ok := arg.(*ast.StringLit); ok {
			out.WriteString(fmt.Sprintf("dex_release(%s); ", tmpNames[i]))
		}
	}
	out.WriteString(fmt.Sprintf("%s; })", resTmp))
}

// genStdlibArg generates an argument for a stdlib function with CName,
// bridging DexString* to const char* when the stdlib expects it.
func (g *Generator) genStdlibArg(out *strings.Builder, arg ast.Expr, funcDef *stdlib.FuncDef, idx int) {
	argType := g.typeOfExpr(arg)
	if argType == ast.TypeString && funcDef.IsRawParam(idx) {
		// Handed over whole, so the callee can read ->len. Borrowed, because the
		// callee only reads it: an argument that allocates is released once the
		// statement finishes, exactly as in the const char* case below.
		g.genBorrowed(out, arg)
		return
	}
	if argType == ast.TypeString {
		// Stdlib functions with CName expect const char*
		if strLit, ok := arg.(*ast.StringLit); ok {
			out.WriteString(fmt.Sprintf("%q", strLit.Value))
		} else {
			// Only the bytes are read, and the DexString wrapping them is not
			// handed over, so an argument that allocates — a concatenated SQL
			// statement, say — is released once the statement finishes.
			g.genBorrowed(out, arg)
			out.WriteString("->data")
		}
	} else {
		g.genBorrowed(out, arg)
	}
}
