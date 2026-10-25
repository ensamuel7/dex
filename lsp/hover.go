package lsp

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ensamuel7/dex/ast"
	"github.com/ensamuel7/dex/checker"
	"github.com/ensamuel7/dex/lexer"
	"github.com/ensamuel7/dex/parser"
	"github.com/ensamuel7/dex/resolve"
	"github.com/ensamuel7/dex/stdlib"
	"github.com/ensamuel7/dex/token"
)

func (s *Server) hoverAt(uri string, text string, pos Position) string {
	// Tokenize to find what's at the cursor
	lex := lexer.New(text)
	tokens, err := lex.Tokenize()
	if err != nil {
		return ""
	}

	tok := tokenAtPosition(tokens, pos)
	if tok == nil {
		return ""
	}

	switch tok.Kind {
	case token.TokenFn, token.TokenFunction:
		return "**keyword** `fn` / `function`\n\nDeclares a function."
	case token.TokenLet:
		return "**keyword** `let`\n\nDeclares a variable with a type annotation."
	case token.TokenConst:
		return "**keyword** `const`\n\nDeclares an immutable variable. Cannot be reassigned after initialization."
	case token.TokenPublic:
		return "**keyword** `public`\n\nMarks a function or struct field as publicly accessible (default)."
	case token.TokenPrivate:
		return "**keyword** `private`\n\nMarks a function or struct field as private. Access restricted to the defining module."
	case token.TokenReturn:
		return "**keyword** `return`\n\nReturns a value from the current function."
	case token.TokenIf:
		return "**keyword** `if`\n\nConditional branch. Condition must be `bool`."
	case token.TokenElse:
		return "**keyword** `else`\n\nAlternate branch for `if`."
	case token.TokenWhile:
		return "**keyword** `while`\n\nLoop while condition is `bool` true."
	case token.TokenImport:
		return "**keyword** `import`\n\nImports a module."
	case token.TokenIntKw:
		return "**type** `int`\n\nSigned integer (C `int`)."
	case token.TokenBool:
		return "**type** `bool`\n\nBoolean value (`true` or `false`)."
	case token.TokenStringKw:
		return "**type** `string`\n\nUTF-8 string."
	case token.TokenLong:
		return "**type** `long`\n\nSigned long integer (C `long`)."
	case token.TokenDouble:
		return "**type** `double`\n\nDouble-precision floating point (C `double`)."
	case token.TokenCharKw:
		return "**type** `char`\n\nSingle character (C `unsigned char`)."
	case token.TokenChar:
		return fmt.Sprintf("**char literal** `char`\n\n`'%s'`", tok.Value)
	case token.TokenTry:
		return "**keyword** `try`\n\nStarts a try-catch-finally block for exception handling."
	case token.TokenCatch:
		return "**keyword** `catch`\n\nHandles one exception type thrown in a `try` block. Syntax: `catch (e: ParseError) { ... }`\n\nA `try` may have a clause per type; the first whose type matches runs. `catch (e: Exception)` is the catch-all, so a clause after it can never run."
	case token.TokenFinally:
		return "**keyword** `finally`\n\nBlock that always executes after `try` or `catch`, used for cleanup. An exception no clause matched carries on outward once it has run."
	case token.TokenThrow:
		return "**keyword** `throw`\n\nThrows an exception. Syntax: `throw Exception(\"message\")`\n\nAny struct carrying a `message: string` and no field needing release can be thrown: `throw ParseError(\"bad digit\", 12)`."
	case token.TokenThrows:
		return "**keyword** `throws`\n\nDeclares that an exception may escape this function, after the return type: `fn load(path: string): int throws`\n\nNaming types says which: `throws ParseError, IOError`. A bare `throws` promises nothing specific, so only a catch-all can be sure of handling it.\n\nChecked by `dex lint`, not by the compiler — it reports an exception that escapes a function not declaring it, a type the list leaves out, and a type the function cannot throw."
	case token.TokenEnum:
		return "**keyword** `enum`\n\nDeclares an enumerated type. Variants are integer constants.\n\nSyntax: `enum Name { Variant1 Variant2 ... }`"
	case token.TokenMap:
		return "**keyword** `map`\n\nHash map type. Syntax: `map[keyType, valueType]`\n\nMethods: `set`, `get`, `has`, `remove`, `len`, `keys`, `values`"
	case token.TokenSwitch:
		return "**keyword** `switch`\n\nSwitch statement for multi-way branching. Syntax: `switch (expr) { case val: { ... } default: { ... } }`"
	case token.TokenCase:
		return "**keyword** `case`\n\nA case branch in a `switch` statement. Supports multiple comma-separated values."
	case token.TokenDefault:
		return "**keyword** `default`\n\nDefault branch in a `switch` statement."
	case token.TokenFor:
		return "**keyword** `for`\n\nC-style for loop. Syntax: `for (init; cond; step) { ... }`"
	case token.TokenForeach:
		return "**keyword** `foreach`\n\nIterate over array elements. Syntax: `foreach arr as item { ... }`"
	case token.TokenAs:
		return "**keyword** `as`\n\nBinds loop variable in `foreach`. Syntax: `foreach arr as item { ... }`"
	case token.TokenBreak:
		return "**keyword** `break`\n\nExit the nearest enclosing loop immediately."
	case token.TokenContinue:
		return "**keyword** `continue`\n\nSkip to the next iteration of the nearest enclosing loop."
	case token.TokenStruct:
		return "**keyword** `struct`\n\nDeclares a struct type. Syntax: `struct Name { field: type ... }`"
	case token.TokenMatch:
		return "**keyword** `match`\n\nPattern matching expression. Syntax: `match (expr) { pattern => result, ... }`"
	case token.TokenDefer:
		return "**keyword** `defer`\n\nRegisters a call to run when the function is left — on a `return`, on falling off the end, and when an exception unwinds past the frame.\n\nThe call is registered where the statement runs, so one in a branch not taken registers nothing and one in a loop registers per iteration. Registered calls run at the exit, most recent first, and capture what they need where the `defer` is written."
	case token.TokenInterface:
		return "**keyword** `interface`\n\nDeclares an interface type. Syntax: `interface Name { method(params): ret ... }`"
	case token.TokenSpawn:
		return "**keyword** `spawn`\n\nSpawn a concurrent task. Syntax: `spawn functionName(args)`"
	case token.TokenChan:
		return "**keyword** `chan`\n\nChannel type for concurrent communication. Syntax: `chan<type>`"
	case token.TokenNull:
		return "**keyword** `null`\n\nNull value for weak references and optional types."
	case token.TokenVoid:
		return "**type** `void`\n\nReturn type for functions that do not return a value."
	case token.TokenMutex:
		return "**keyword** `mutex`\n\nMutual exclusion lock for thread synchronization."
	case token.TokenWeak:
		return "**keyword** `weak`\n\nDeclares a weak reference that does not prevent garbage collection."
	case token.TokenTrue, token.TokenFalse:
		return "**constant** `bool`\n\nBoolean literal."
	case token.TokenIdent:
		return s.hoverIdent(uri, text, tokens, tok)
	case token.TokenString:
		return fmt.Sprintf("**string literal**\n\n`\"%s\"`", tok.Value)
	case token.TokenInt:
		return fmt.Sprintf("**integer literal** `int`\n\n`%s`", tok.Value)
	case token.TokenFloat:
		return fmt.Sprintf("**float literal** `double`\n\n`%s`", tok.Value)
	}

	return ""
}

func (s *Server) hoverIdent(uri string, text string, tokens []token.Token, tok *token.Token) string {
	// Parse the file for function/variable info
	filePath := uriToPath(uri)
	sourceDir := filepath.Dir(filePath)
	p := parser.New(tokens)
	seedParserModuleTypes(p, tokens, sourceDir)
	program, errs := p.Parse()
	if len(errs) > 0 {
		// Still attempt hover with partial parse results
		if program == nil {
			return ""
		}
	}

	// Resolve user modules for cross-module hover info
	resolve.FlattenStructMethods(program)
	_ = resolve.ResolveUserModules(program, sourceDir)
	ch := checker.New()
	ch.SetGenerics(program.GenericFuncs, resolve.Instantiator{Parser: p, Aliases: program.ModuleAliases})
	ch.Check(program)

	name := tok.Value

	// Check if this identifier is after a dot (field access: object.field)
	idx := tokenIndex(tokens, tok)
	if idx >= 2 && tokens[idx-1].Kind == token.TokenDot {
		objTok := tokens[idx-2]
		if objTok.Kind == token.TokenIdent {
			if result := s.hoverFieldAccess(program, objTok.Value, name); result != "" {
				return result
			}
			if t, ok := findVariableTypeID(program, objTok.Value); ok {
				if m, ok := builtinMethodOf(t, name); ok {
					return fmt.Sprintf("```dex\n(method) %s.%s\n```\n\n%s", typeName(t), m.sig, m.doc)
				}
			}
			if result := hoverUserModuleFunc(program, objTok.Value, name); result != "" {
				return result
			}
		}
	}

	// Check if it's a user-defined function name
	for _, fn := range program.Functions {
		if fn.Name == name {
			return formatFunctionHover(&fn)
		}
	}

	// Check if it's a struct name — the program's own, or a built-in such as
	// Exception.
	if def := structDeclByName(program, name); def != nil {
		return formatStructHover(def)
	}

	// Check if it's a user-defined enum name
	for i := range program.Enums {
		if program.Enums[i].Name == name {
			return formatEnumHover(&program.Enums[i])
		}
	}

	// Check if it's a module name (by path or alias)
	for _, imp := range program.Imports {
		modName := importModuleName(imp)
		if modName == name {
			return formatModuleHover(imp.Path)
		}
	}

	// Check if it's a user module name
	for _, userMod := range program.UserModules {
		if userMod == name {
			return fmt.Sprintf("**module** `%s`\n\nUser module.", name)
		}
	}

	// Check if it's a stdlib function name (after a dot)
	for _, imp := range program.Imports {
		modName := importModuleName(imp)
		mod := stdlib.Lookup(imp.Path)
		if mod == nil {
			continue
		}
		if fdef, ok := mod.Funcs[name]; ok {
			return formatStdlibFuncHover(modName, name, &fdef)
		}
	}

	// Check if it's a stdlib struct type name (e.g. HttpResponse)
	for _, imp := range program.Imports {
		mod := stdlib.Lookup(imp.Path)
		if mod == nil {
			continue
		}
		for i := range mod.Types {
			if mod.Types[i].Name == name {
				return formatStructHover(&mod.Types[i])
			}
		}
	}

	// Try to find as a local variable by walking function bodies
	if t, ok := findVariableTypeID(program, name); ok {
		if isArray(t) {
			return formatArrayHover(program, name, t)
		}
		// A string has too many methods to list on every hover; hovering one of
		// them shows its signature instead.
		if ast.IsMapType(t) || t == ast.TypeStringBuilder {
			return formatReceiverHover(name, t)
		}
		return fmt.Sprintf("**variable** `%s`\n\nType: `%s`", name, typeName(t))
	}

	return ""
}

func findVariableType(program *ast.Program, name string) string {
	if t, ok := findVariableTypeID(program, name); ok {
		return typeName(t)
	}
	return ""
}

// hoverUserModuleFunc returns hover info for a user-module function referenced
// as module.name. ResolveUserModules merges module functions into the program
// under a "<module>_<name>" prefix, so a lookup by the bare name never matches.
func hoverUserModuleFunc(program *ast.Program, moduleName, name string) string {
	isUserModule := false
	for _, m := range program.UserModules {
		if m == moduleName {
			isUserModule = true
			break
		}
	}
	if !isUserModule {
		return ""
	}

	qualified := moduleName + "_" + name
	for i := range program.Functions {
		if program.Functions[i].Name != qualified {
			continue
		}
		// Display the call-site spelling (module.name), not the merged name.
		fn := program.Functions[i]
		fn.Name = moduleName + "." + name
		return formatFunctionHover(&fn)
	}
	return ""
}

func formatFunctionHover(fn *ast.Function) string {
	var params []string
	for _, p := range fn.Params {
		params = append(params, fmt.Sprintf("%s: %s", p.Name, typeName(p.Type)))
	}
	sig := fmt.Sprintf("fn %s(%s): %s%s", fn.Name, strings.Join(params, ", "), typeName(fn.ReturnType), throwsClause(fn))
	return fmt.Sprintf("```dex\n%s\n```\n\nUser-defined function.", sig)
}

// throwsClause renders the `throws` part of a signature, which is the whole
// reason it is written: a caller reading the signature is told an exception may
// escape, and which kinds when the declaration names them.
func throwsClause(fn *ast.Function) string {
	if !fn.Throws {
		return ""
	}
	if len(fn.ThrowsTypes) == 0 {
		return " throws"
	}
	names := make([]string, len(fn.ThrowsTypes))
	for i, t := range fn.ThrowsTypes {
		names[i] = ast.DisplayStructName(t)
	}
	return " throws " + strings.Join(names, ", ")
}

func formatModuleHover(path string) string {
	mod := stdlib.Lookup(path)
	if mod == nil {
		return fmt.Sprintf("**module** `%s`", path)
	}

	var funcs []string
	for name, fdef := range mod.Funcs {
		var paramStr string
		retStr := typeName(fdef.ReturnType)
		if sp, sr, ok := stdlib.SpecialSignature(path, name, &fdef); ok {
			paramStr = sp
			retStr = sr
		} else {
			var params []string
			for i, p := range fdef.Params {
				pname := fmt.Sprintf("arg%d", i+1)
				if i < len(fdef.ParamNames) {
					pname = fdef.ParamNames[i]
				}
				params = append(params, fmt.Sprintf("%s: %s", pname, typeName(p)))
			}
			paramStr = strings.Join(params, ", ")
		}
		funcs = append(funcs, fmt.Sprintf("- `%s(%s): %s`", name, paramStr, retStr))
	}
	return fmt.Sprintf("**module** `%s`\n\nFunctions:\n%s", path, strings.Join(funcs, "\n"))
}

func formatStdlibFuncHover(moduleName, funcName string, fdef *stdlib.FuncDef) string {
	var paramStr string
	retStr := typeName(fdef.ReturnType)

	if sp, sr, ok := stdlib.SpecialSignature(moduleName, funcName, fdef); ok {
		paramStr = sp
		retStr = sr
	} else {
		var params []string
		for i, p := range fdef.Params {
			pname := fmt.Sprintf("arg%d", i+1)
			if i < len(fdef.ParamNames) {
				pname = fdef.ParamNames[i]
			}
			params = append(params, fmt.Sprintf("%s: %s", pname, typeName(p)))
		}
		paramStr = strings.Join(params, ", ")
	}

	sig := fmt.Sprintf("%s.%s(%s): %s", moduleName, funcName, paramStr, retStr)
	doc := "Standard library function."
	if fdef.Doc != "" {
		doc = fdef.Doc
	}
	return fmt.Sprintf("```dex\n%s\n```\n\n%s", sig, doc)
}

func formatStructHover(def *ast.StructDef) string {
	var members []string
	for _, f := range def.Fields {
		members = append(members, fmt.Sprintf("    %s: %s", f.Name, typeName(f.Type)))
	}
	if len(def.Methods) > 0 {
		members = append(members, "")
		for i := range def.Methods {
			members = append(members, "    "+methodSignature(&def.Methods[i]))
		}
	}
	sig := fmt.Sprintf("struct %s {\n%s\n}", def.Name, strings.Join(members, "\n"))

	doc := "User-defined struct."
	if def.Doc != "" {
		doc = def.Doc
	}

	out := fmt.Sprintf("```dex\n%s\n```\n\n%s", sig, doc)

	if size := formatStructSize(def); size != "" {
		out += "\n\n" + size
	}

	if len(def.Fields) > 0 {
		var fieldDocs []string
		for _, f := range def.Fields {
			entry := fmt.Sprintf("- `%s`: `%s`", f.Name, typeName(f.Type))
			if f.Doc != "" {
				entry += " — " + f.Doc
			}
			fieldDocs = append(fieldDocs, entry)
		}
		out += "\n\n**Fields:**\n" + strings.Join(fieldDocs, "\n")
	}

	if len(def.Methods) > 0 {
		var methodDocs []string
		for i := range def.Methods {
			methodDocs = append(methodDocs, "- `"+methodSignature(&def.Methods[i])+"`")
		}
		out += "\n\n**Methods:**\n" + strings.Join(methodDocs, "\n")
	}

	return out
}

func formatStructSize(def *ast.StructDef) string {
	for _, f := range def.Fields {
		if ast.IsTypeParamType(f.Type) {
			return ""
		}
	}
	l := ast.LayoutFields(def.Fields)
	if t, ok := ast.LookupStructType(def.Name); ok {
		l = ast.LayoutOf(t)
	}
	out := fmt.Sprintf("**Size:** %s (align %d", plural(l.Size, "byte"), l.Align)
	if l.Padding > 0 {
		out += fmt.Sprintf(", %s of padding", plural(l.Padding, "byte"))
	}
	out += ")"
	if l.Declared > l.Size {
		out += fmt.Sprintf("\n\nLaid out as `%s`; in declared order it would be %s.", strings.Join(l.Order, ", "), plural(l.Declared, "byte"))
	}
	return out
}

func plural(n int64, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

// methodSignature renders a method as it was declared, receiver implicit: the
// `self` parameter FlattenStructMethods prepends is an implementation detail of
// the lowering, not part of the surface the caller writes.
func methodSignature(m *ast.Function) string {
	var params []string
	for _, p := range m.Params {
		if p.Name == "self" {
			continue
		}
		params = append(params, fmt.Sprintf("%s: %s", p.Name, typeName(p.Type)))
	}
	sig := fmt.Sprintf("fn %s(%s): %s%s", m.Name, strings.Join(params, ", "), typeName(m.ReturnType), throwsClause(m))
	if m.IsPrivate {
		sig = "private " + sig
	}
	if m.Throws {
		sig += " throws"
	}
	return sig
}

func formatEnumHover(def *ast.EnumDef) string {
	var variants []string
	for _, v := range def.Variants {
		variants = append(variants, "    "+v)
	}
	sig := fmt.Sprintf("enum %s {\n%s\n}", def.Name, strings.Join(variants, "\n"))
	return fmt.Sprintf("```dex\n%s\n```\n\nEnum type with %d variant(s).", sig, len(def.Variants))
}

func formatFieldHover(structName string, f *ast.StructField) string {
	header := fmt.Sprintf("```dex\n(field) %s.%s: %s\n```", structName, f.Name, typeName(f.Type))
	if f.Doc != "" {
		return header + "\n\n" + f.Doc
	}
	return header
}

func formatMethodHover(structName string, m *ast.Function) string {
	return fmt.Sprintf("```dex\n(method) %s.%s\n```\n\nMethod of `%s`.", structName, strings.TrimPrefix(methodSignature(m), "fn "), structName)
}

func (s *Server) hoverFieldAccess(program *ast.Program, objName, fieldName string) string {
	objType, ok := findVariableTypeID(program, objName)
	if !ok || !ast.IsStructType(objType) {
		return ""
	}

	def := ast.GetStructDef(objType)
	if def == nil {
		return ""
	}

	for i := range def.Fields {
		if def.Fields[i].Name == fieldName {
			return formatFieldHover(def.Name, &def.Fields[i])
		}
	}
	// The registry entry is written when the header is parsed and carries no
	// methods, so the declaration in this program is the only place they can be
	// read from.
	if decl := structDeclByName(program, def.Name); decl != nil {
		for i := range decl.Methods {
			if decl.Methods[i].Name == fieldName {
				return formatMethodHover(decl.Name, &decl.Methods[i])
			}
		}
	}
	return ""
}

// structDeclByName finds a struct's declaration. The program is asked first,
// since its declarations carry methods and doc comments, and the global type
// registry second — `Exception` is registered there rather than declared in any
// file, as are the struct types the stdlib modules provide, so a lookup that
// only walked the program found nothing to say about either.
func structDeclByName(program *ast.Program, name string) *ast.StructDef {
	if program != nil {
		for i := range program.Structs {
			if program.Structs[i].Name == name {
				return &program.Structs[i]
			}
		}
	}
	if t, ok := ast.LookupStructType(name); ok {
		if def := ast.GetStructDef(t); def != nil && def.Name == name {
			return def
		}
	}
	return nil
}

func tokenIndex(tokens []token.Token, tok *token.Token) int {
	for i := range tokens {
		if &tokens[i] == tok {
			return i
		}
	}
	return -1
}

func findVariableTypeID(program *ast.Program, name string) (ast.Type, bool) {
	for _, fn := range program.Functions {
		for _, p := range fn.Params {
			if p.Name == name {
				return p.Type, true
			}
		}
		var found ast.Type
		ok := false
		set := func(t ast.Type) {
			if !ok {
				found, ok = t, true
			}
		}
		resolve.VisitStmts(fn.Body, func(st ast.Stmt) {
			switch s := st.(type) {
			case *ast.LetStmt:
				if s.Name == name {
					set(s.Type)
				}
				for _, n := range s.Names {
					if n == name {
						set(s.Type)
					}
				}
			case *ast.ForeachStmt:
				if s.ValueVar == name {
					set(s.ElemType)
				} else if s.IndexVar == name {
					set(ast.TypeInt)
				}
			case *ast.TryCatchStmt:
				for i := range s.Catches {
					if s.Catches[i].VarName == name {
						if t, ok := ast.LookupStructType(s.Catches[i].TypeName); ok {
							set(t)
						}
					}
				}
			}
		}, nil)
		if ok {
			return found, true
		}
	}
	return 0, false
}

func formatArrayHover(program *ast.Program, name string, t ast.Type) string {
	elemType := ast.ElementType(t)
	out := fmt.Sprintf("```dex\n(variable) %s: %s\n```\n\nArray of `%s`.", name, typeName(t), typeName(elemType))
	if def := structDeclByName(program, ast.StructName(elemType)); ast.IsStructType(elemType) && def != nil && len(def.Fields) > 0 {
		var fields []string
		for _, f := range def.Fields {
			fields = append(fields, fmt.Sprintf("- `%s`: `%s`", f.Name, typeName(f.Type)))
		}
		out += "\n\n**Element fields:**\n" + strings.Join(fields, "\n")
	}
	return out + "\n\n" + formatMethodList(t)
}

// formatReceiverHover is the variable hover for a map or a StringBuilder: its
// type, then the methods it has with that type's key and value filled in.
func formatReceiverHover(name string, t ast.Type) string {
	desc := "A growable string buffer."
	if ast.IsMapType(t) {
		desc = fmt.Sprintf("Map from `%s` to `%s`.", typeName(ast.MapKeyType(t)), typeName(ast.MapValueType(t)))
	}
	return fmt.Sprintf("```dex\n(variable) %s: %s\n```\n\n%s\n\n%s", name, typeName(t), desc, formatMethodList(t))
}

func formatMethodList(t ast.Type) string {
	builtins, _ := builtinMethods(t)
	var methods []string
	for _, m := range builtins {
		methods = append(methods, fmt.Sprintf("- `%s` — %s", m.sig, m.doc))
	}
	return "**Methods:**\n" + strings.Join(methods, "\n")
}
