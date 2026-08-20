package lsp

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ensamuel7/dex/ast"
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
		return "**keyword** `catch`\n\nCatches an exception thrown in a `try` block. Syntax: `catch (e: Exception) { ... }`"
	case token.TokenFinally:
		return "**keyword** `finally`\n\nBlock that always executes after `try` or `catch`, used for cleanup."
	case token.TokenThrow:
		return "**keyword** `throw`\n\nThrows an exception. Syntax: `throw Exception(\"message\")`"
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
		return "**keyword** `defer`\n\nDefers execution of a statement until the enclosing scope exits."
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
	p := parser.New(tokens)
	seedParserModuleTypes(p, tokens)
	program, errs := p.Parse()
	if len(errs) > 0 {
		// Still attempt hover with partial parse results
		if program == nil {
			return ""
		}
	}

	// Resolve user modules for cross-module hover info
	resolve.FlattenStructMethods(program)
	filePath := uriToPath(uri)
	sourceDir := filepath.Dir(filePath)
	_ = resolve.ResolveUserModules(program, sourceDir)

	name := tok.Value

	// Check if this identifier is after a dot (field access: object.field)
	idx := tokenIndex(tokens, tok)
	if idx >= 2 && tokens[idx-1].Kind == token.TokenDot {
		objTok := tokens[idx-2]
		if objTok.Kind == token.TokenIdent {
			if result := s.hoverFieldAccess(program, objTok.Value, name); result != "" {
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

	// Check if it's a user-defined struct name
	for i := range program.Structs {
		if program.Structs[i].Name == name {
			return formatStructHover(&program.Structs[i])
		}
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
	varType := findVariableType(program, name)
	if varType != "" {
		return fmt.Sprintf("**variable** `%s`\n\nType: `%s`", name, varType)
	}

	return ""
}

func findVariableType(program *ast.Program, name string) string {
	for _, fn := range program.Functions {
		// Check params
		for _, p := range fn.Params {
			if p.Name == name {
				return typeName(p.Type)
			}
		}
		// Check let statements
		if t := findLetInStmts(fn.Body, name); t != "" {
			return t
		}
	}
	return ""
}

func findLetInStmts(stmts []ast.Stmt, name string) string {
	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case *ast.LetStmt:
			if s.Name == name {
				return typeName(s.Type)
			}
		case *ast.IfStmt:
			if t := findLetInStmts(s.Then, name); t != "" {
				return t
			}
			if t := findLetInStmts(s.Else, name); t != "" {
				return t
			}
		case *ast.WhileStmt:
			if t := findLetInStmts(s.Body, name); t != "" {
				return t
			}
		case *ast.BlockStmt:
			if t := findLetInStmts(s.Stmts, name); t != "" {
				return t
			}
		case *ast.TryCatchStmt:
			if s.CatchVar == name {
				return "Exception"
			}
			if t := findLetInStmts(s.Body, name); t != "" {
				return t
			}
			if t := findLetInStmts(s.CatchBody, name); t != "" {
				return t
			}
			if t := findLetInStmts(s.FinallyBody, name); t != "" {
				return t
			}
		case *ast.SwitchStmt:
			for _, sc := range s.Cases {
				if t := findLetInStmts(sc.Body, name); t != "" {
					return t
				}
			}
			if t := findLetInStmts(s.Default, name); t != "" {
				return t
			}
		}
	}
	return ""
}

func formatFunctionHover(fn *ast.Function) string {
	var params []string
	for _, p := range fn.Params {
		params = append(params, fmt.Sprintf("%s: %s", p.Name, typeName(p.Type)))
	}
	sig := fmt.Sprintf("fn %s(%s): %s", fn.Name, strings.Join(params, ", "), typeName(fn.ReturnType))
	return fmt.Sprintf("```dex\n%s\n```\n\nUser-defined function.", sig)
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
	var fields []string
	for _, f := range def.Fields {
		fields = append(fields, fmt.Sprintf("    %s: %s", f.Name, typeName(f.Type)))
	}
	sig := fmt.Sprintf("struct %s {\n%s\n}", def.Name, strings.Join(fields, "\n"))

	doc := "User-defined struct."
	if def.Doc != "" {
		doc = def.Doc
	}

	var fieldDocs []string
	for _, f := range def.Fields {
		entry := fmt.Sprintf("- `%s`: `%s`", f.Name, typeName(f.Type))
		if f.Doc != "" {
			entry += " — " + f.Doc
		}
		fieldDocs = append(fieldDocs, entry)
	}

	return fmt.Sprintf("```dex\n%s\n```\n\n%s\n\n**Fields:**\n%s", sig, doc, strings.Join(fieldDocs, "\n"))
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
	return ""
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
		if t, ok := findLetTypeIDInStmts(fn.Body, name); ok {
			return t, true
		}
	}
	return 0, false
}

func findLetTypeIDInStmts(stmts []ast.Stmt, name string) (ast.Type, bool) {
	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case *ast.LetStmt:
			if s.Name == name {
				return s.Type, true
			}
		case *ast.IfStmt:
			if t, ok := findLetTypeIDInStmts(s.Then, name); ok {
				return t, true
			}
			if t, ok := findLetTypeIDInStmts(s.Else, name); ok {
				return t, true
			}
		case *ast.WhileStmt:
			if t, ok := findLetTypeIDInStmts(s.Body, name); ok {
				return t, true
			}
		case *ast.BlockStmt:
			if t, ok := findLetTypeIDInStmts(s.Stmts, name); ok {
				return t, true
			}
		case *ast.TryCatchStmt:
			if s.CatchVar == name {
				if excType, ok := ast.LookupStructType("Exception"); ok {
					return excType, true
				}
			}
			if t, ok := findLetTypeIDInStmts(s.Body, name); ok {
				return t, true
			}
			if t, ok := findLetTypeIDInStmts(s.CatchBody, name); ok {
				return t, true
			}
			if t, ok := findLetTypeIDInStmts(s.FinallyBody, name); ok {
				return t, true
			}
		case *ast.SwitchStmt:
			for _, sc := range s.Cases {
				if t, ok := findLetTypeIDInStmts(sc.Body, name); ok {
					return t, true
				}
			}
			if t, ok := findLetTypeIDInStmts(s.Default, name); ok {
				return t, true
			}
		}
	}
	return 0, false
}
