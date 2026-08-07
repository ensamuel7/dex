package docgen

import (
	"bytes"
	_ "embed"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ensamuel7/dex/ast"
	"github.com/ensamuel7/dex/stdlib"
	"github.com/ensamuel7/dex/token"
)

//go:embed template.html
var tmplHTML string

// TemplateData holds all data passed to the HTML template.
type TemplateData struct {
	Modules  []ModuleData
	DbModule ModuleData
	Keywords []KeywordData
	Types    []KeywordData
	Examples []ExampleData
}

// ModuleData represents a stdlib module.
type ModuleData struct {
	Name  string
	Funcs []FuncData
}

// FuncData represents a single function in a module.
type FuncData struct {
	Module string
	Name   string
	Params string
	Return string
	Doc    string
}

// KeywordData represents a keyword or type keyword entry.
type KeywordData struct {
	Name  string
	Token string
}

// ExampleData represents an example file.
type ExampleData struct {
	Title    string
	Filename string
	Content  string
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
		if ast.IsStructType(t) {
			return ast.StructName(t)
		}
		return "unknown"
	}
}

func tokenKindName(k token.TokenKind) string {
	switch k {
	case token.TokenFn:
		return "TokenFn"
	case token.TokenFunction:
		return "TokenFunction"
	case token.TokenLet:
		return "TokenLet"
	case token.TokenConst:
		return "TokenConst"
	case token.TokenReturn:
		return "TokenReturn"
	case token.TokenIf:
		return "TokenIf"
	case token.TokenElse:
		return "TokenElse"
	case token.TokenWhile:
		return "TokenWhile"
	case token.TokenFor:
		return "TokenFor"
	case token.TokenForeach:
		return "TokenForeach"
	case token.TokenAs:
		return "TokenAs"
	case token.TokenBreak:
		return "TokenBreak"
	case token.TokenContinue:
		return "TokenContinue"
	case token.TokenTrue:
		return "TokenTrue"
	case token.TokenFalse:
		return "TokenFalse"
	case token.TokenImport:
		return "TokenImport"
	case token.TokenStruct:
		return "TokenStruct"
	case token.TokenPublic:
		return "TokenPublic"
	case token.TokenPrivate:
		return "TokenPrivate"
	case token.TokenSpawn:
		return "TokenSpawn"
	case token.TokenChan:
		return "TokenChan"
	case token.TokenIntKw:
		return "TokenIntKw"
	case token.TokenBool:
		return "TokenBool"
	case token.TokenStringKw:
		return "TokenStringKw"
	case token.TokenLong:
		return "TokenLong"
	case token.TokenDouble:
		return "TokenDouble"
	case token.TokenVoid:
		return "TokenVoid"
	case token.TokenCharKw:
		return "TokenCharKw"
	default:
		return fmt.Sprintf("TokenKind(%d)", k)
	}
}

func titleCase(filename string) string {
	title := strings.TrimSuffix(filename, ".dx")
	title = strings.ReplaceAll(title, "_", " ")
	words := strings.Fields(title)
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

func Generate(projectRoot string) (string, error) {
	var data TemplateData

	// Collect stdlib modules
	modules := stdlib.AllModules()
	moduleNames := make([]string, 0, len(modules))
	for name := range modules {
		moduleNames = append(moduleNames, name)
	}
	sort.Strings(moduleNames)

	for _, name := range moduleNames {
		mod := modules[name]
		md := ModuleData{Name: name}

		funcNames := make([]string, 0, len(mod.Funcs))
		for fn := range mod.Funcs {
			funcNames = append(funcNames, fn)
		}
		sort.Strings(funcNames)

		for _, fnName := range funcNames {
			fd := mod.Funcs[fnName]
			var paramStr string
			retType := typeName(fd.ReturnType)
			if name == "fmt" && fnName == "print" {
				paramStr = "int|long|double|string|bool"
			} else if name == "json" && fnName == "set" {
				paramStr = "string, string, int|long|double|string|bool"
			} else if name == "db" && fnName == "col" {
				paramStr = "int, int"
				retType = "int|string|double|bool"
			} else if name == "http" && fd.Params == nil {
				// HTTP special-cased functions
				switch fnName {
				case "route":
					paramStr = "method: string, path: string, handler: fn"
				case "response":
					paramStr = "statusCode: int, body: string, contentType: string"
					retType = "HttpResponse"
				case "get":
					paramStr = "string, [string]"
					retType = "HttpResponse"
				case "post", "put", "patch":
					paramStr = "string, string, [string]"
					retType = "HttpResponse"
				case "delete":
					paramStr = "string, [string]"
					retType = "HttpResponse"
				case "request":
					paramStr = "string, string, string, string"
					retType = "HttpResponse"
				case "header":
					paramStr = "string, string, string"
					retType = "string"
				case "formNew":
					paramStr = ""
					retType = "string"
				case "formField", "formFile":
					paramStr = "string, string, string"
					retType = "string"
				case "postForm":
					paramStr = "string, string, [string]"
					retType = "HttpResponse"
				}
			} else if name == "time" && fd.Params == nil {
				// Timer special-cased functions
				switch fnName {
				case "setTimeout":
					paramStr = "fn: fn(): void, ms: int"
				case "setInterval":
					paramStr = "fn: fn(): void, ms: int"
					retType = "int"
				}
			} else if name == "ws" && fd.Params == nil {
				// WebSocket special-cased functions
				switch fnName {
				case "handleMessage":
					paramStr = "handler: fn(ws.Conn, string): void"
				case "connect":
					paramStr = "url: string"
					retType = "Conn"
				case "send":
					paramStr = "conn: Conn, msg: string"
				case "receive":
					paramStr = "conn: Conn"
					retType = "string"
				case "close":
					paramStr = "conn: Conn"
				}
			} else {
				params := make([]string, len(fd.Params))
				for i, p := range fd.Params {
					params[i] = typeName(p)
				}
				paramStr = strings.Join(params, ", ")
			}
			md.Funcs = append(md.Funcs, FuncData{
				Module: name,
				Name:   fnName,
				Params: paramStr,
				Return: retType,
				Doc:    fd.Doc,
			})
		}
		data.Modules = append(data.Modules, md)
	}

	// Separate db module for dedicated database section
	for _, md := range data.Modules {
		if md.Name == "db" {
			data.DbModule = md
			break
		}
	}

	// Collect keywords and type keywords
	keywords := make([]string, 0)
	types := make([]string, 0)
	for kw, kind := range token.Keywords {
		switch kind {
		case token.TokenIntKw, token.TokenBool, token.TokenStringKw, token.TokenLong, token.TokenDouble, token.TokenCharKw:
			types = append(types, kw)
		default:
			keywords = append(keywords, kw)
		}
	}
	sort.Strings(keywords)
	sort.Strings(types)

	for _, kw := range keywords {
		data.Keywords = append(data.Keywords, KeywordData{
			Name:  kw,
			Token: tokenKindName(token.Keywords[kw]),
		})
	}
	for _, t := range types {
		data.Types = append(data.Types, KeywordData{
			Name:  t,
			Token: tokenKindName(token.Keywords[t]),
		})
	}

	// Read example files
	exDir := filepath.Join(projectRoot, "examples")
	entries, err := os.ReadDir(exDir)
	if err == nil {
		for _, e := range entries {
			if filepath.Ext(e.Name()) == ".dx" {
				content, err := os.ReadFile(filepath.Join(exDir, e.Name()))
				if err != nil {
					continue
				}
				data.Examples = append(data.Examples, ExampleData{
					Title:    titleCase(e.Name()),
					Filename: e.Name(),
					Content:  string(content),
				})
			}
		}
	}

	// Parse and execute the template
	tmpl, err := template.New("docs").Parse(tmplHTML)
	if err != nil {
		return "", fmt.Errorf("parsing template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("executing template: %w", err)
	}

	return buf.String(), nil
}
