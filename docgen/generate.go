package docgen

import (
	"bytes"
	_ "embed"
	"fmt"
	"html/template"
	"os/exec"
	"sort"
	"strings"

	"github.com/ensamuel7/dex/ast"
	"github.com/ensamuel7/dex/stdlib"
	"github.com/ensamuel7/dex/token"
)

//go:embed template.html
var tmplHTML string

//go:embed favicon.svg
var faviconSVG []byte

//go:embed icon.png
var iconPNG []byte

// Assets are the static files the page links to, written beside index.html.
func Assets() map[string][]byte {
	return map[string][]byte{"favicon.svg": faviconSVG, "icon.png": iconPNG}
}

// TemplateData holds all data passed to the HTML template.
type TemplateData struct {
	Modules   []ModuleData
	Generated []ModuleData
	Releases  []ReleaseData
	DbModule  ModuleData
	Keywords  []KeywordData
	Types     []KeywordData
	ValueType TypeDoc
}

// TypeDoc documents a library type that carries methods rather than being a
// bag of module functions — json.Value is the first of these.
type TypeDoc struct {
	Name    string
	Summary string
	Methods []MethodData
}

// MethodData is one method on a documented type.
type MethodData struct {
	Name   string
	Params string
	Return string
	Doc    string
}

// ModuleData represents a stdlib module.
type ModuleData struct {
	Name    string
	Page    string
	Summary string
	Funcs   []FuncData
}

// ReleaseData is one tagged release, newest first on the Versions page.
type ReleaseData struct {
	Tag     string
	Version string
	Date    string
}

// releases reads the release tags from the repository the docs are generated
// in, so the Versions page is current whenever the docs are. Outside a git
// checkout it is empty and the page links to GitHub instead.
func releases(projectRoot string) []ReleaseData {
	out, err := exec.Command("git", "-C", projectRoot, "tag", "--list", "v*", "--sort=-v:refname",
		"--format=%(refname:short)|%(creatordate:short)").Output()
	if err != nil {
		return nil
	}
	var rs []ReleaseData
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		tag, date, ok := strings.Cut(line, "|")
		if !ok {
			continue
		}
		rs = append(rs, ReleaseData{Tag: tag, Version: strings.TrimPrefix(tag, "v"), Date: date})
	}
	return rs
}

// moduleSummaries is the one-line description of each stdlib module, shown in
// the overview table and at the top of a generated page. A module missing here
// fails TestEveryModuleDocumented.
var moduleSummaries = map[string]string{
	"crypto":  "Random tokens, password hashing, encryption, HMAC, JWT signatures, base64/base32 and TOTP",
	"db":      "Database access: SQLite, PostgreSQL, MySQL and MongoDB",
	"file":    "Files and directories: read, write, list, permissions, locks, atomic writes",
	"fmt":     "Formatted output to stdout",
	"http":    "HTTP server and client",
	"io":      "Read user input from stdin",
	"json":    "JSON to and from structs, arrays and maps, plus the json.Value document type",
	"kafka":   "Kafka producer and consumer",
	"math":    "Math functions and constants",
	"num":     "Numeric conversions between int, long and double",
	"os":      "Environment variables, arguments and running commands",
	"redis":   "Redis client: keys, hashes, lists, sets and pub/sub",
	"reflect": "Read and write a struct's fields by position at runtime",
	"smtp":    "Send email over SMTP",
	"str":     "Conversions between strings and numbers",
	"time":    "Clocks, sleeping, timers and formatting",
	"ws":      "WebSocket server and client (RFC 6455)",
}

// handWritten lists the modules whose page is written in template.html. Every
// other module gets a page generated from its registry entry alone.
var handWritten = map[string]bool{
	"crypto": true, "db": true, "file": true, "fmt": true, "http": true, "io": true, "json": true,
	"math": true, "os": true, "reflect": true, "str": true, "time": true, "ws": true,
}

func modulePage(name string) string {
	if name == "db" {
		return "database"
	}
	return "mod-" + name
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
	case token.TokenNull:
		return "TokenNull"
	case token.TokenTry:
		return "TokenTry"
	case token.TokenCatch:
		return "TokenCatch"
	case token.TokenFinally:
		return "TokenFinally"
	case token.TokenThrow:
		return "TokenThrow"
	case token.TokenThrows:
		return "TokenThrows"
	case token.TokenSwitch:
		return "TokenSwitch"
	case token.TokenCase:
		return "TokenCase"
	case token.TokenDefault:
		return "TokenDefault"
	case token.TokenEnum:
		return "TokenEnum"
	case token.TokenMap:
		return "TokenMap"
	case token.TokenMatch:
		return "TokenMatch"
	case token.TokenInterface:
		return "TokenInterface"
	case token.TokenDefer:
		return "TokenDefer"
	case token.TokenMutex:
		return "TokenMutex"
	case token.TokenWeak:
		return "TokenWeak"
	default:
		return fmt.Sprintf("TokenKind(%d)", k)
	}
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
		md := ModuleData{Name: name, Page: modulePage(name), Summary: moduleSummaries[name]}

		funcNames := make([]string, 0, len(mod.Funcs))
		for fn := range mod.Funcs {
			funcNames = append(funcNames, fn)
		}
		sort.Strings(funcNames)

		for _, fnName := range funcNames {
			fd := mod.Funcs[fnName]
			var paramStr string
			retType := typeName(fd.ReturnType)
			if sp, sr, ok := stdlib.SpecialSignature(name, fnName, &fd); ok {
				paramStr = sp
				retType = sr
			} else {
				params := make([]string, len(fd.Params))
				for i, p := range fd.Params {
					params[i] = typeName(p)
					if i < len(fd.ParamNames) && fd.ParamNames[i] != "" {
						params[i] = fd.ParamNames[i] + ": " + params[i]
					}
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
		if !handWritten[name] {
			data.Generated = append(data.Generated, md)
		}
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

	data.ValueType = jsonValueDoc()
	data.Releases = releases(projectRoot)

	// Parse and execute the template
	tmpl, err := template.New("docs").Funcs(template.FuncMap{"funcs": data.funcs}).Parse(tmplHTML)
	if err != nil {
		return "", fmt.Errorf("parsing template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("executing template: %w", err)
	}

	return buf.String(), nil
}

// funcs gives the template a module's functions, all of them or the named ones
// in the order named, so a hand-written page can group them under its own
// headings while the signatures and descriptions still come from the registry.
func (d TemplateData) funcs(module string, names ...string) ([]FuncData, error) {
	for _, md := range d.Modules {
		if md.Name != module {
			continue
		}
		if len(names) == 0 {
			return md.Funcs, nil
		}
		out := make([]FuncData, 0, len(names))
		for _, n := range names {
			found := false
			for _, fd := range md.Funcs {
				if fd.Name == n {
					out = append(out, fd)
					found = true
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("docs name %s.%s, which the stdlib does not have", module, n)
			}
		}
		return out, nil
	}
	return nil, fmt.Errorf("docs name module %s, which the stdlib does not have", module)
}

// jsonValueDoc describes json.Value and its methods. It is written out here
// rather than derived from the stdlib registry because json.Value is a language
// type with methods, not a set of module functions.
func jsonValueDoc() TypeDoc {
	return TypeDoc{
		Name: "json.Value",
		Summary: "A JSON document: null, a boolean, a number, a string, an array, or an object. " +
			"Write one with ordinary literal syntax — an object literal { key: value } is always a json.Value, " +
			"and an array literal becomes one when the target says so, which is what lets its elements differ in type. " +
			"Index with an int to read an array position or a string to read an object key; both give back a json.Value, " +
			"so a path is walked one step at a time. A missing key or out-of-range index yields null rather than failing.",
		Methods: []MethodData{
			{"asInt", "", "int", "Value as an int."},
			{"asLong", "", "long", "Value as a long."},
			{"asDouble", "", "double", "Value as a double."},
			{"asString", "", "string", "String contents; any other value as its JSON text."},
			{"asBool", "", "bool", "Value as a bool."},
			{"isNull", "", "bool", "Is it null, or absent?"},
			{"isBool", "", "bool", "Is it a boolean?"},
			{"isNumber", "", "bool", "Is it a number?"},
			{"isString", "", "bool", "Is it a string?"},
			{"isArray", "", "bool", "Is it an array?"},
			{"isObject", "", "bool", "Is it an object?"},
			{"len", "", "int", "Element count for an array or object, character count for a string."},
			{"has", "key: string", "bool", "Does this object have that key?"},
			{"keys", "", "string[]", "This object\u0027s keys, in insertion order."},
		},
	}
}
