package docgen

import (
	"bytes"
	_ "embed"
	"fmt"
	"html/template"
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
			if sp, sr, ok := stdlib.SpecialSignature(name, fnName, &fd); ok {
				paramStr = sp
				retType = sr
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
