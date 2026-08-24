package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/ensamuel7/dex/ast"
	"github.com/ensamuel7/dex/checker"
	"github.com/ensamuel7/dex/lexer"
	"github.com/ensamuel7/dex/parser"
	"github.com/ensamuel7/dex/resolve"
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

type TextEdit struct {
	Range   Range  `json:"range"`
	NewText string `json:"newText"`
}

type CompletionItem struct {
	Label               string     `json:"label"`
	Kind                int        `json:"kind"`
	Detail              string     `json:"detail,omitempty"`
	Documentation       string     `json:"documentation,omitempty"`
	AdditionalTextEdits []TextEdit `json:"additionalTextEdits,omitempty"`
}

// Completion item kinds
const (
	CompletionKindFunction = 3
	CompletionKindModule   = 9
	CompletionKindKeyword  = 14
	CompletionKindType     = 25 // TypeParameter
	CompletionKindValue    = 12 // Value (LSP EnumMember)
)

// --- Server ---

type Server struct {
	mu           sync.Mutex
	documents    map[string]string
	writer       io.Writer
	logger       *log.Logger
	importedURIs map[string][]string // main document URI -> list of imported file URIs we published diagnostics for
}

// Run starts the LSP server on stdin/stdout.
func Run() {
	logger := log.New(os.Stderr, "[dex-lsp] ", log.LstdFlags)

	s := &Server{
		documents:    make(map[string]string),
		writer:       os.Stdout,
		logger:       logger,
		importedURIs: make(map[string][]string),
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
	case "textDocument/definition":
		s.handleDefinition(msg)
	case "textDocument/semanticTokens/full":
		s.handleSemanticTokens(msg)
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
			"textDocumentSync":  1, // Full document sync
			"hoverProvider":     true,
			"definitionProvider": true,
			"completionProvider": map[string]interface{}{
				"triggerCharacters": []string{"."},
			},
			"semanticTokensProvider": map[string]interface{}{
				"legend": map[string]interface{}{
					"tokenTypes":     semanticTokenTypes,
					"tokenModifiers": semanticTokenModifiers,
				},
				"full": true,
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
	// Clear diagnostics for imported files
	if uris, ok := s.importedURIs[params.TextDocument.URI]; ok {
		for _, impURI := range uris {
			s.publishDiagnostics(impURI, []Diagnostic{})
		}
		delete(s.importedURIs, params.TextDocument.URI)
	}
	// Clear diagnostics for the main file
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

	content := s.hoverAt(params.TextDocument.URI, text, params.Position)
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

	items := s.completionsAt(text, params.Position, params.TextDocument.URI)
	s.sendResponse(msg.ID, items)
}

// --- Go to Definition ---

func (s *Server) handleDefinition(msg *jsonrpcMessage) {
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

	lex := lexer.New(text)
	tokens, err := lex.Tokenize()
	if err != nil {
		s.sendResponse(msg.ID, nil)
		return
	}

	tok := tokenAtPosition(tokens, params.Position)
	if tok == nil || tok.Kind != token.TokenIdent {
		s.sendResponse(msg.ID, nil)
		return
	}

	name := tok.Value

	// Search for definition in tokens: fn/function <name>, struct <name>, let/const <name>
	for i := 0; i < len(tokens)-1; i++ {
		t := tokens[i]
		next := tokens[i+1]

		if next.Kind != token.TokenIdent || next.Value != name {
			continue
		}

		isDef := t.Kind == token.TokenFn || t.Kind == token.TokenFunction ||
			t.Kind == token.TokenStruct ||
			t.Kind == token.TokenLet || t.Kind == token.TokenConst

		if !isDef {
			continue
		}

		// Don't jump to self (cursor is already on the definition)
		if next.Line == tok.Line && next.Col == tok.Col {
			continue
		}

		s.sendResponse(msg.ID, map[string]interface{}{
			"uri": params.TextDocument.URI,
			"range": Range{
				Start: Position{Line: next.Line - 1, Character: next.Col - 1},
				End:   Position{Line: next.Line - 1, Character: next.Col - 1 + len(next.Value)},
			},
		})
		return
	}

	// No definition found (e.g. stdlib types — no source location)
	s.sendResponse(msg.ID, nil)
}

// --- Diagnostics ---

var errPosRegex = regexp.MustCompile(`^(?:(.+?):)?(\d+):(\d+):\s*(.*)$`)

func (s *Server) diagnose(uri string, text string) {
	var diagnostics []Diagnostic

	// Clear previously published diagnostics for imported files
	if prevURIs, ok := s.importedURIs[uri]; ok {
		for _, impURI := range prevURIs {
			s.publishDiagnostics(impURI, []Diagnostic{})
		}
	}
	s.importedURIs[uri] = nil

	// crossFileDiags collects diagnostics keyed by file URI for cross-file routing
	crossFileDiags := map[string][]Diagnostic{}

	// Reset and register module types
	ast.ResetStructTypes()
	ast.ResetChanTypes()
	ast.ResetTaskTypes()
	ast.ResetWeakTypes()
	ast.ResetStructArrayTypes()
	ast.ResetOptionalTypes()
	ast.ResetRefTypes()
	ast.ResetFuncTypes()
	ast.ResetMapTypes()
	ast.ResetEnumTypes()
	stdlib.RegisterAllModuleTypes()
	ast.RegisterExceptionType()

	// Lex
	lex := lexer.New(text)
	tokens, err := lex.Tokenize()
	if err != nil {
		diagnostics = append(diagnostics, makeDiagnosticWithSource(err.Error(), text))
		s.publishDiagnostics(uri, diagnostics)
		return
	}

	filePath := uriToPath(uri)
	sourceDir := filepath.Dir(filePath)

	// Diagnose imported user module files for syntax errors
	importedPaths := extractUserImportPaths(tokens, sourceDir)
	for _, impPath := range importedPaths {
		impDiags := diagnoseImportedFile(impPath)
		if len(impDiags) > 0 {
			impURI := pathToURI(impPath)
			crossFileDiags[impURI] = append(crossFileDiags[impURI], impDiags...)
		}
	}

	// Parse
	p := parser.New(tokens)
	seedParserModuleTypes(p, tokens)
	// Pre-register struct/enum names from user module files so the parser
	// recognizes struct literal syntax (e.g. User { field: value }).
	importPaths := resolve.ExtractImportPaths(tokens)
	for _, name := range resolve.PreRegisterUserStructs(importPaths, sourceDir) {
		p.AddStructName(name)
	}
	program, parseErrs := p.Parse()
	if len(parseErrs) > 0 {
		for _, e := range parseErrs {
			diagnostics = append(diagnostics, makeDiagnosticWithSource(e.Error(), text))
		}
		s.publishDiagnostics(uri, diagnostics)
		s.publishCrossFileDiags(uri, crossFileDiags)
		return
	}

	// Resolve user modules so the checker doesn't flag them as unknown
	resolve.FlattenStructMethods(program)
	if err := resolve.ResolveUserModules(program, sourceDir); err != nil {
		result := makeDiagnosticForFileWithSource(err.Error(), filePath, text)
		if result.file != "" {
			// Error belongs to a different file — route it there
			impURI := pathToURI(result.file)
			// Read the imported file source for token length
			impSource, _ := os.ReadFile(result.file)
			impText := string(impSource)
			tokenLen := 1
			if impText != "" {
				tokenLen = tokenLengthAt(impText, result.line1, result.col1)
			}
			crossFileDiags[impURI] = append(crossFileDiags[impURI], Diagnostic{
				Range: Range{
					Start: Position{Line: result.line1 - 1, Character: result.col1 - 1},
					End:   Position{Line: result.line1 - 1, Character: result.col1 - 1 + tokenLen},
				},
				Severity: 1,
				Source:   "dex",
				Message:  result.message,
			})
		} else {
			diagnostics = append(diagnostics, result.diag)
		}
		s.publishDiagnostics(uri, diagnostics)
		s.publishCrossFileDiags(uri, crossFileDiags)
		return
	}

	// Type check
	ch := checker.New()
	if checkErrs := ch.Check(program); len(checkErrs) > 0 {
		for _, e := range checkErrs {
			result := makeDiagnosticForFileWithSource(e.Error(), filePath, text)
			if result.file != "" {
				// Error belongs to a different file — route it there
				impURI := pathToURI(result.file)
				impSource, _ := os.ReadFile(result.file)
				impText := string(impSource)
				tokenLen := 1
				if impText != "" {
					tokenLen = tokenLengthAt(impText, result.line1, result.col1)
				}
				crossFileDiags[impURI] = append(crossFileDiags[impURI], Diagnostic{
					Range: Range{
						Start: Position{Line: result.line1 - 1, Character: result.col1 - 1},
						End:   Position{Line: result.line1 - 1, Character: result.col1 - 1 + tokenLen},
					},
					Severity: 1,
					Source:   "dex",
					Message:  result.message,
				})
			} else {
				diagnostics = append(diagnostics, result.diag)
			}
		}
		s.publishDiagnostics(uri, diagnostics)
		s.publishCrossFileDiags(uri, crossFileDiags)
		return
	}

	// No errors — clear diagnostics
	s.publishDiagnostics(uri, diagnostics)
	s.publishCrossFileDiags(uri, crossFileDiags)
}

// publishCrossFileDiags publishes diagnostics to imported file URIs and tracks
// the URIs for cleanup on close/change.
func (s *Server) publishCrossFileDiags(mainURI string, crossFileDiags map[string][]Diagnostic) {
	var publishedURIs []string
	for impURI, diags := range crossFileDiags {
		s.publishDiagnostics(impURI, diags)
		publishedURIs = append(publishedURIs, impURI)
	}
	s.importedURIs[mainURI] = publishedURIs
}

// crossFileDiagResult holds the parsed error info for cross-file diagnostic routing.
type crossFileDiagResult struct {
	diag      Diagnostic
	file      string // non-empty if error belongs to a different file
	line1     int    // 1-based line from error (for cross-file use)
	col1      int    // 1-based col from error (for cross-file use)
	message   string // cleaned message without location prefix
}

func makeDiagnosticForFileWithSource(errMsg, currentFile, sourceText string) crossFileDiagResult {
	line := 0
	col := 0
	message := errMsg
	file := ""

	if m := errPosRegex.FindStringSubmatch(errMsg); m != nil {
		file = m[1] // may be empty if no filename prefix
		if l, err := strconv.Atoi(m[2]); err == nil {
			line = l - 1 // LSP is 0-based
		}
		if c, err := strconv.Atoi(m[3]); err == nil {
			col = c - 1
		}
		message = m[4]
	}

	// If the error is from a different file, return it for cross-file routing
	if file != "" && currentFile != "" && file != currentFile {
		return crossFileDiagResult{
			file:    file,
			line1:   line + 1,
			col1:    col + 1,
			message: message,
		}
	}

	if line < 0 {
		line = 0
	}
	if col < 0 {
		col = 0
	}

	// Compute token length for better error ranges
	tokenLen := 1
	if sourceText != "" && line >= 0 && col >= 0 {
		tokenLen = tokenLengthAt(sourceText, line+1, col+1) // convert to 1-based
	}

	return crossFileDiagResult{
		diag: Diagnostic{
			Range: Range{
				Start: Position{Line: line, Character: col},
				End:   Position{Line: line, Character: col + tokenLen},
			},
			Severity: 1, // Error
			Source:   "dex",
			Message:  message,
		},
	}
}

func makeDiagnosticWithSource(errMsg, sourceText string) Diagnostic {
	result := makeDiagnosticForFileWithSource(errMsg, "", sourceText)
	return result.diag
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
		if ast.IsOptionalType(t) {
			return typeName(ast.OptionalInnerType(t)) + "?"
		}
		if ast.IsStructArrayType(t) {
			elemType := ast.ElementType(t)
			if ast.IsEnumType(elemType) {
				return ast.EnumName(elemType) + "[]"
			}
			return ast.StructName(elemType) + "[]"
		}
		if ast.IsStructType(t) {
			return ast.StructName(t)
		}
		if ast.IsEnumType(t) {
			return ast.EnumName(t)
		}
		if ast.IsMapType(t) {
			return "map[" + typeName(ast.MapKeyType(t)) + ", " + typeName(ast.MapValueType(t)) + "]"
		}
		return "unknown"
	}
}

// seedParserModuleTypes extracts import paths from tokens and seeds the parser
// with struct type names from imported modules.
func seedParserModuleTypes(p *parser.Parser, tokens []token.Token) {
	var importPaths []string
	for i := 0; i < len(tokens)-1; i++ {
		if tokens[i].Kind == token.TokenImport && tokens[i+1].Kind == token.TokenString {
			importPaths = append(importPaths, tokens[i+1].Value)
		}
	}
	for _, name := range stdlib.ModuleTypesForImports(importPaths) {
		p.AddStructName(name)
	}
	p.AddStructName("Exception") // built-in Exception type
}

// pathToURI converts a local file path to a file:// URI.
func pathToURI(filePath string) string {
	abs, err := filepath.Abs(filePath)
	if err != nil {
		abs = filePath
	}
	return "file://" + abs
}

// extractUserImportPaths scans tokens for import declarations and returns
// absolute paths to user .dx module files (skipping stdlib imports).
func extractUserImportPaths(tokens []token.Token, sourceDir string) []string {
	var paths []string
	for i := 0; i < len(tokens)-1; i++ {
		if tokens[i].Kind == token.TokenImport && tokens[i+1].Kind == token.TokenString {
			importPath := tokens[i+1].Value
			// Skip stdlib imports
			if stdlib.Lookup(importPath) != nil {
				continue
			}
			// Resolve to absolute .dx file path
			modFile := importPath + ".dx"
			absPath := filepath.Join(sourceDir, modFile)
			paths = append(paths, absPath)
		}
	}
	return paths
}

// diagnoseImportedFile reads a .dx file from disk and runs lexer+parser,
// returning any diagnostics found.
func diagnoseImportedFile(filePath string) []Diagnostic {
	source, err := os.ReadFile(filePath)
	if err != nil {
		return nil // file not found — resolve will catch this
	}

	text := string(source)
	var diags []Diagnostic

	lex := lexer.NewWithFile(text, filePath)
	tokens, err := lex.Tokenize()
	if err != nil {
		diags = append(diags, makeDiagnosticWithSource(err.Error(), text))
		return diags
	}

	p := parser.New(tokens)
	seedParserModuleTypes(p, tokens)
	_, parseErrs := p.Parse()
	for _, e := range parseErrs {
		diags = append(diags, makeDiagnosticWithSource(e.Error(), text))
	}

	return diags
}

// tokenLengthAt computes the length of the token at the given 1-based line and
// column in the source text. Returns 1 as a fallback.
func tokenLengthAt(text string, line, col int) int {
	lines := strings.Split(text, "\n")
	if line < 1 || line > len(lines) {
		return 1
	}
	lineText := lines[line-1]
	idx := col - 1 // convert to 0-based
	if idx < 0 || idx >= len(lineText) {
		return 1
	}

	ch := lineText[idx]

	// String literal
	if ch == '"' || ch == '\'' || ch == '`' {
		for i := idx + 1; i < len(lineText); i++ {
			if lineText[i] == ch && (i == 0 || lineText[i-1] != '\\') {
				return i - idx + 1
			}
		}
		return len(lineText) - idx
	}

	// Identifier or keyword
	if isIdentStart(ch) {
		end := idx + 1
		for end < len(lineText) && isIdentContinue(lineText[end]) {
			end++
		}
		return end - idx
	}

	// Number
	if ch >= '0' && ch <= '9' {
		end := idx + 1
		for end < len(lineText) && (lineText[end] >= '0' && lineText[end] <= '9' || lineText[end] == '.') {
			end++
		}
		return end - idx
	}

	// Multi-char operators
	remaining := lineText[idx:]
	for _, op := range []string{"!==", "===", "!=", "==", "<=", ">=", "&&", "||", "++", "--", "+=", "-=", "*=", "/="} {
		if strings.HasPrefix(remaining, op) {
			return len(op)
		}
	}

	return 1
}

func isIdentStart(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_'
}

func isIdentContinue(ch byte) bool {
	return isIdentStart(ch) || (ch >= '0' && ch <= '9')
}

// uriToPath converts a file:// URI to a local file path.
func uriToPath(uri string) string {
	u, err := url.Parse(uri)
	if err != nil {
		// Fallback: strip file:// prefix
		return strings.TrimPrefix(uri, "file://")
	}
	return u.Path
}

// importModuleName returns the effective name used to reference a module:
// the alias if set, otherwise the last path segment.
func importModuleName(imp ast.Import) string {
	if imp.Alias != "" {
		return imp.Alias
	}
	return imp.Path
}

// resolveAliasToPath scans the text for import declarations and returns the
// original path for a given alias name, or "" if not found.
func resolveAliasToPath(alias string, text string) string {
	lex := lexer.New(text)
	tokens, err := lex.Tokenize()
	if err != nil {
		return ""
	}
	for i := 0; i < len(tokens)-1; i++ {
		if tokens[i].Kind == token.TokenImport && tokens[i+1].Kind == token.TokenString {
			path := tokens[i+1].Value
			// Check for 'as "alias"' after the path
			if i+3 < len(tokens) && tokens[i+2].Kind == token.TokenAs && tokens[i+3].Kind == token.TokenString {
				if tokens[i+3].Value == alias {
					return path
				}
			}
		}
	}
	return ""
}
