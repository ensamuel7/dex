package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/ensamuel7/dex/ast"
	"github.com/ensamuel7/dex/checker"
	"github.com/ensamuel7/dex/lexer"
	"github.com/ensamuel7/dex/parser"
	"github.com/ensamuel7/dex/stdlib"
	"github.com/ensamuel7/dex/token"
)

// --- JSON-RPC types ---

type jsonrpcMessage struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method,omitempty"`
	Params  json.RawMessage  `json:"params,omitempty"`
	Result  interface{}      `json:"result,omitempty"`
	Error   *jsonrpcError    `json:"error,omitempty"`
}

type jsonrpcError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// --- LSP types ---

type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity"`
	Source   string `json:"source"`
	Message  string `json:"message"`
}

type CompletionItem struct {
	Label         string `json:"label"`
	Kind          int    `json:"kind"`
	Detail        string `json:"detail,omitempty"`
	Documentation string `json:"documentation,omitempty"`
}

// Completion item kinds
const (
	CompletionKindFunction = 3
	CompletionKindModule   = 9
	CompletionKindKeyword  = 14
	CompletionKindType     = 25 // TypeParameter
)

// --- Server ---

type Server struct {
	mu        sync.Mutex
	documents map[string]string
	writer    io.Writer
	logger    *log.Logger
}

// Run starts the LSP server on stdin/stdout.
func Run() {
	logger := log.New(os.Stderr, "[dex-lsp] ", log.LstdFlags)

	s := &Server{
		documents: make(map[string]string),
		writer:    os.Stdout,
		logger:    logger,
	}

	reader := bufio.NewReader(os.Stdin)

	for {
		msg, err := s.readMessage(reader)
		if err != nil {
			if err == io.EOF {
				return
			}
			logger.Printf("read error: %v", err)
			return
		}

		s.handleMessage(msg)
	}
}

// --- Message I/O ---

func (s *Server) readMessage(reader *bufio.Reader) (*jsonrpcMessage, error) {
	var contentLength int
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		if strings.HasPrefix(line, "Content-Length: ") {
			contentLength, _ = strconv.Atoi(strings.TrimPrefix(line, "Content-Length: "))
		}
	}

	if contentLength == 0 {
		return nil, fmt.Errorf("missing Content-Length header")
	}

	body := make([]byte, contentLength)
	if _, err := io.ReadFull(reader, body); err != nil {
		return nil, err
	}

	var msg jsonrpcMessage
	if err := json.Unmarshal(body, &msg); err != nil {
		return nil, err
	}

	return &msg, nil
}

func (s *Server) sendResponse(id *json.RawMessage, result interface{}) {
	resp := struct {
		JSONRPC string           `json:"jsonrpc"`
		ID      *json.RawMessage `json:"id"`
		Result  interface{}      `json:"result"`
	}{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
	s.writeJSON(resp)
}

func (s *Server) sendNotification(method string, params interface{}) {
	msg := struct {
		JSONRPC string      `json:"jsonrpc"`
		Method  string      `json:"method"`
		Params  interface{} `json:"params"`
	}{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
	s.writeJSON(msg)
}

func (s *Server) writeJSON(v interface{}) {
	body, err := json.Marshal(v)
	if err != nil {
		s.logger.Printf("marshal error: %v", err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	fmt.Fprintf(s.writer, "Content-Length: %d\r\n\r\n", len(body))
	s.writer.Write(body)
}

// --- Message dispatch ---

func (s *Server) handleMessage(msg *jsonrpcMessage) {
	switch msg.Method {
	case "initialize":
		s.handleInitialize(msg)
	case "initialized":
		// no-op
	case "shutdown":
		s.sendResponse(msg.ID, nil)
	case "exit":
		os.Exit(0)
	case "textDocument/didOpen":
		s.handleDidOpen(msg)
	case "textDocument/didChange":
		s.handleDidChange(msg)
	case "textDocument/didClose":
		s.handleDidClose(msg)
	case "textDocument/hover":
		s.handleHover(msg)
	case "textDocument/completion":
		s.handleCompletion(msg)
	default:
		// Unknown method — respond with method not found if it has an ID
		if msg.ID != nil {
			s.sendResponse(msg.ID, nil)
		}
	}
}

// --- Handlers ---

func (s *Server) handleInitialize(msg *jsonrpcMessage) {
	result := map[string]interface{}{
		"capabilities": map[string]interface{}{
			"textDocumentSync": 1, // Full document sync
			"hoverProvider":    true,
			"completionProvider": map[string]interface{}{
				"triggerCharacters": []string{"."},
			},
		},
		"serverInfo": map[string]interface{}{
			"name":    "dex-lsp",
			"version": "0.1.0",
		},
	}
	s.sendResponse(msg.ID, result)
}

func (s *Server) handleDidOpen(msg *jsonrpcMessage) {
	var params struct {
		TextDocument struct {
			URI  string `json:"uri"`
			Text string `json:"text"`
		} `json:"textDocument"`
	}
	json.Unmarshal(msg.Params, &params)

	s.documents[params.TextDocument.URI] = params.TextDocument.Text
	s.diagnose(params.TextDocument.URI, params.TextDocument.Text)
}

func (s *Server) handleDidChange(msg *jsonrpcMessage) {
	var params struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
		ContentChanges []struct {
			Text string `json:"text"`
		} `json:"contentChanges"`
	}
	json.Unmarshal(msg.Params, &params)

	if len(params.ContentChanges) > 0 {
		text := params.ContentChanges[len(params.ContentChanges)-1].Text
		s.documents[params.TextDocument.URI] = text
		s.diagnose(params.TextDocument.URI, text)
	}
}

func (s *Server) handleDidClose(msg *jsonrpcMessage) {
	var params struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
	}
	json.Unmarshal(msg.Params, &params)

	delete(s.documents, params.TextDocument.URI)
	// Clear diagnostics
	s.publishDiagnostics(params.TextDocument.URI, []Diagnostic{})
}

func (s *Server) handleHover(msg *jsonrpcMessage) {
	var params struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
		Position Position `json:"position"`
	}
	json.Unmarshal(msg.Params, &params)

	text, ok := s.documents[params.TextDocument.URI]
	if !ok {
		s.sendResponse(msg.ID, nil)
		return
	}

	content := s.hoverAt(text, params.Position)
	if content == "" {
		s.sendResponse(msg.ID, nil)
		return
	}

	s.sendResponse(msg.ID, map[string]interface{}{
		"contents": map[string]interface{}{
			"kind":  "markdown",
			"value": content,
		},
	})
}

func (s *Server) handleCompletion(msg *jsonrpcMessage) {
	var params struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
		Position Position `json:"position"`
	}
	json.Unmarshal(msg.Params, &params)

	text, ok := s.documents[params.TextDocument.URI]
	if !ok {
		s.sendResponse(msg.ID, nil)
		return
	}

	items := s.completionsAt(text, params.Position)
	s.sendResponse(msg.ID, items)
}

// --- Diagnostics ---

var errPosRegex = regexp.MustCompile(`^(\d+):(\d+):\s*(.*)$`)

func (s *Server) diagnose(uri string, text string) {
	var diagnostics []Diagnostic

	// Lex
	lex := lexer.New(text)
	tokens, err := lex.Tokenize()
	if err != nil {
		diagnostics = append(diagnostics, makeDiagnostic(err.Error()))
		s.publishDiagnostics(uri, diagnostics)
		return
	}

	// Parse
	p := parser.New(tokens)
	program, err := p.Parse()
	if err != nil {
		diagnostics = append(diagnostics, makeDiagnostic(err.Error()))
		s.publishDiagnostics(uri, diagnostics)
		return
	}

	// Type check
	ch := checker.New()
	if err := ch.Check(program); err != nil {
		diagnostics = append(diagnostics, makeDiagnostic(err.Error()))
		s.publishDiagnostics(uri, diagnostics)
		return
	}

	// No errors — clear diagnostics
	s.publishDiagnostics(uri, diagnostics)
}

func makeDiagnostic(errMsg string) Diagnostic {
	line := 0
	col := 0
	message := errMsg

	if m := errPosRegex.FindStringSubmatch(errMsg); m != nil {
		if l, err := strconv.Atoi(m[1]); err == nil {
			line = l - 1 // LSP is 0-based
		}
		if c, err := strconv.Atoi(m[2]); err == nil {
			col = c - 1
		}
		message = m[3]
	}

	if line < 0 {
		line = 0
	}
	if col < 0 {
		col = 0
	}

	return Diagnostic{
		Range: Range{
			Start: Position{Line: line, Character: col},
			End:   Position{Line: line, Character: col + 1},
		},
		Severity: 1, // Error
		Source:   "dex",
		Message:  message,
	}
}

func (s *Server) publishDiagnostics(uri string, diagnostics []Diagnostic) {
	if diagnostics == nil {
		diagnostics = []Diagnostic{}
	}
	s.sendNotification("textDocument/publishDiagnostics", map[string]interface{}{
		"uri":         uri,
		"diagnostics": diagnostics,
	})
}

// --- Hover ---

func (s *Server) hoverAt(text string, pos Position) string {
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
		return "**keyword** `import`\n\nImports a standard library module."
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
	case token.TokenTrue, token.TokenFalse:
		return "**constant** `bool`\n\nBoolean literal."
	case token.TokenIdent:
		return s.hoverIdent(text, tokens, tok)
	case token.TokenString:
		return fmt.Sprintf("**string literal**\n\n`\"%s\"`", tok.Value)
	case token.TokenInt:
		return fmt.Sprintf("**integer literal** `int`\n\n`%s`", tok.Value)
	case token.TokenFloat:
		return fmt.Sprintf("**float literal** `double`\n\n`%s`", tok.Value)
	}

	return ""
}

func (s *Server) hoverIdent(text string, tokens []token.Token, tok *token.Token) string {
	// Parse the file for function/variable info
	p := parser.New(tokens)
	program, err := p.Parse()
	if err != nil {
		return ""
	}

	name := tok.Value

	// Check if it's a user-defined function name
	for _, fn := range program.Functions {
		if fn.Name == name {
			return formatFunctionHover(&fn)
		}
	}

	// Check if it's a module name
	for _, imp := range program.Imports {
		if imp.Path == name {
			return formatModuleHover(imp.Path)
		}
	}

	// Check if it's a stdlib function name (after a dot)
	// Look for module.funcName pattern — check the token before this one
	for _, imp := range program.Imports {
		mod := stdlib.Lookup(imp.Path)
		if mod == nil {
			continue
		}
		if fdef, ok := mod.Funcs[name]; ok {
			return formatStdlibFuncHover(imp.Path, name, &fdef)
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
		var params []string
		for i, p := range fdef.Params {
			params = append(params, fmt.Sprintf("arg%d: %s", i+1, typeName(p)))
		}
		funcs = append(funcs, fmt.Sprintf("- `%s(%s): %s`", name, strings.Join(params, ", "), typeName(fdef.ReturnType)))
	}
	return fmt.Sprintf("**module** `%s`\n\nFunctions:\n%s", path, strings.Join(funcs, "\n"))
}

func formatStdlibFuncHover(moduleName, funcName string, fdef *stdlib.FuncDef) string {
	var params []string
	for i, p := range fdef.Params {
		params = append(params, fmt.Sprintf("arg%d: %s", i+1, typeName(p)))
	}
	sig := fmt.Sprintf("%s.%s(%s): %s", moduleName, funcName, strings.Join(params, ", "), typeName(fdef.ReturnType))
	return fmt.Sprintf("```dex\n%s\n```\n\nStandard library function.", sig)
}

// --- Completion ---

func (s *Server) completionsAt(text string, pos Position) []CompletionItem {
	lines := strings.Split(text, "\n")
	if pos.Line >= len(lines) {
		return nil
	}

	line := lines[pos.Line]
	col := pos.Character
	if col > len(line) {
		col = len(line)
	}
	prefix := line[:col]

	// Check if we're completing after a dot (module.func)
	if idx := strings.LastIndex(prefix, "."); idx >= 0 {
		// Extract module name before the dot
		before := strings.TrimSpace(prefix[:idx])
		words := strings.Fields(before)
		if len(words) > 0 {
			moduleName := words[len(words)-1]
			return s.moduleCompletions(moduleName)
		}
	}

	var items []CompletionItem

	// Keywords
	for _, kw := range []struct {
		label  string
		detail string
	}{
		{"fn", "Function declaration (short)"},
		{"function", "Function declaration"},
		{"let", "Variable declaration"},
		{"const", "Immutable variable declaration"},
		{"return", "Return from function"},
		{"if", "Conditional branch"},
		{"else", "Else branch"},
		{"while", "While loop"},
		{"import", "Import module"},
		{"public", "Public access modifier"},
		{"private", "Private access modifier"},
		{"true", "Boolean true"},
		{"false", "Boolean false"},
	} {
		items = append(items, CompletionItem{
			Label:  kw.label,
			Kind:   CompletionKindKeyword,
			Detail: kw.detail,
		})
	}

	// Types
	for _, t := range []string{"int", "bool", "string", "long", "double", "char", "int[]", "bool[]", "string[]", "long[]", "double[]", "char[]"} {
		items = append(items, CompletionItem{
			Label:  t,
			Kind:   CompletionKindType,
			Detail: "Built-in type",
		})
	}

	// User-defined functions from current file
	lex := lexer.New(text)
	tokens, err := lex.Tokenize()
	if err == nil {
		p := parser.New(tokens)
		program, err := p.Parse()
		if err == nil {
			for _, fn := range program.Functions {
				items = append(items, CompletionItem{
					Label:  fn.Name,
					Kind:   CompletionKindFunction,
					Detail: formatFuncSignature(&fn),
				})
			}

			// Imported module names
			for _, imp := range program.Imports {
				items = append(items, CompletionItem{
					Label:  imp.Path,
					Kind:   CompletionKindModule,
					Detail: "Module",
				})
			}
		}
	}

	return items
}

func (s *Server) moduleCompletions(moduleName string) []CompletionItem {
	mod := stdlib.Lookup(moduleName)
	if mod != nil {
		var items []CompletionItem
		for name, fdef := range mod.Funcs {
			var params []string
			for i, p := range fdef.Params {
				params = append(params, fmt.Sprintf("arg%d: %s", i+1, typeName(p)))
			}
			detail := fmt.Sprintf("(%s): %s", strings.Join(params, ", "), typeName(fdef.ReturnType))
			items = append(items, CompletionItem{
				Label:  name,
				Kind:   CompletionKindFunction,
				Detail: detail,
			})
		}
		return items
	}

	// Check if it's an array variable — offer push/len
	return []CompletionItem{
		{Label: "push", Kind: CompletionKindFunction, Detail: "(val): void"},
		{Label: "len", Kind: CompletionKindFunction, Detail: "(): int"},
	}
}

// --- Helpers ---

func tokenAtPosition(tokens []token.Token, pos Position) *token.Token {
	// LSP positions are 0-based, tokens are 1-based
	for i := range tokens {
		t := &tokens[i]
		if t.Kind == token.TokenEOF {
			continue
		}
		tLine := t.Line - 1
		tCol := t.Col - 1
		tokenLen := utf8.RuneCountInString(t.Value)

		if tLine == pos.Line && tCol <= pos.Character && tCol+tokenLen > pos.Character {
			return t
		}
	}
	return nil
}

func formatFuncSignature(fn *ast.Function) string {
	var params []string
	for _, p := range fn.Params {
		params = append(params, fmt.Sprintf("%s: %s", p.Name, typeName(p.Type)))
	}
	return fmt.Sprintf("fn(%s): %s", strings.Join(params, ", "), typeName(fn.ReturnType))
}

func typeName(t ast.Type) string {
	switch t {
	case ast.TypeInt:
		return "int"
	case ast.TypeBool:
		return "bool"
	case ast.TypeString:
		return "string"
	case ast.TypeVoid:
		return "void"
	case ast.TypeLong:
		return "long"
	case ast.TypeDouble:
		return "double"
	case ast.TypeArrayInt:
		return "int[]"
	case ast.TypeArrayBool:
		return "bool[]"
	case ast.TypeArrayString:
		return "string[]"
	case ast.TypeArrayLong:
		return "long[]"
	case ast.TypeArrayDouble:
		return "double[]"
	case ast.TypeChar:
		return "char"
	case ast.TypeArrayChar:
		return "char[]"
	default:
		return "unknown"
	}
}
