package checker

import (
	"fmt"

	"github.com/ensamuel7/dex/ast"
	"github.com/ensamuel7/dex/stdlib"
)

type funcSig struct {
	Params        []ast.Type
	ParamNames    []string
	DefaultValues []ast.Expr // parallel to Params; nil = no default
	ReturnType    ast.Type
	IsPrivate     bool
}

type Checker struct {
	scopes           []map[string]ast.Type
	constScopes      []map[string]bool
	annotationScopes []map[string][]string // per-variable annotation tracking
	funcs            map[string]funcSig
	imports          map[string]*stdlib.Module
	userModules      map[string]bool
	loopDepth        int
	errors           []error
	maxErrors        int

	structMethods      map[string]map[string]funcSig // structName -> methodName -> sig
	structConstructors map[string][]ast.Type          // structName -> constructor param types
	structModule       map[string]string              // structName -> moduleName (for cross-module structs)
}

// addError records a checker error.
func (c *Checker) addError(err error) {
	c.errors = append(c.errors, err)
}

func New() *Checker {
	return &Checker{
		funcs:              make(map[string]funcSig),
		imports:            make(map[string]*stdlib.Module),
		userModules:        make(map[string]bool),
		structMethods:      make(map[string]map[string]funcSig),
		structConstructors: make(map[string][]ast.Type),
		structModule:       make(map[string]string),
		maxErrors:          20,
	}
}

// errAt formats an error with position information when available.
func (c *Checker) errAt(pos ast.Pos, format string, args ...interface{}) error {
	if pos.Line > 0 {
		if pos.File != "" {
			return fmt.Errorf("%s:%d:%d: "+format, append([]interface{}{pos.File, pos.Line, pos.Col}, args...)...)
		}
		return fmt.Errorf("%d:%d: "+format, append([]interface{}{pos.Line, pos.Col}, args...)...)
	}
	return fmt.Errorf(format, args...)
}


func (c *Checker) Check(program *ast.Program) []error {
	// Populate user modules set
	for _, modName := range program.UserModules {
		c.userModules[modName] = true
	}

	// Validate imports (only stdlib imports remain after user module resolution)
	for _, imp := range program.Imports {
		mod := stdlib.Lookup(imp.Path)
		if mod == nil {
			c.addError(fmt.Errorf("unknown import '%s'", imp.Path))
			continue
		}
		key := imp.Path
		if imp.Alias != "" {
			key = imp.Alias
		}
		c.imports[key] = mod
	}

	// Validate struct definitions
	seen := map[string]bool{}
	for _, sd := range program.Structs {
		if seen[sd.Name] {
			c.addError(fmt.Errorf("duplicate struct type '%s'", sd.Name))
			continue
		}
		seen[sd.Name] = true
		fieldNames := map[string]bool{}
		for _, f := range sd.Fields {
			if fieldNames[f.Name] {
				c.addError(fmt.Errorf("duplicate field '%s' in struct '%s'", f.Name, sd.Name))
				continue
			}
			fieldNames[f.Name] = true
			if !isValidFieldType(f.Type) {
				c.addError(fmt.Errorf("invalid type for field '%s' in struct '%s'", f.Name, sd.Name))
			}
		}
	}

	// Validate enum definitions
	for _, ed := range program.Enums {
		if seen[ed.Name] {
			c.addError(fmt.Errorf("duplicate type name '%s'", ed.Name))
			continue
		}
		seen[ed.Name] = true
		variantNames := map[string]bool{}
		for _, v := range ed.Variants {
			if variantNames[v] {
				c.addError(fmt.Errorf("duplicate variant '%s' in enum '%s'", v, ed.Name))
				continue
			}
			variantNames[v] = true
		}
	}

	// Register struct methods and constructor params
	for _, sd := range program.Structs {
		if len(sd.ConstructorParams) > 0 {
			var paramTypes []ast.Type
			for _, cp := range sd.ConstructorParams {
				paramTypes = append(paramTypes, cp.Type)
			}
			c.structConstructors[sd.Name] = paramTypes
		}
		if len(sd.Methods) > 0 {
			methods := make(map[string]funcSig)
			for _, m := range sd.Methods {
				var mParamTypes []ast.Type
				var mParamNames []string
				var mDefaults []ast.Expr
				for _, p := range m.Params {
					mParamTypes = append(mParamTypes, p.Type)
					mParamNames = append(mParamNames, p.Name)
					mDefaults = append(mDefaults, p.DefaultValue)
				}
				methods[m.Name] = funcSig{Params: mParamTypes, ParamNames: mParamNames, DefaultValues: mDefaults, ReturnType: m.ReturnType, IsPrivate: m.IsPrivate}
			}
			c.structMethods[sd.Name] = methods
		}

	}

	// Register built-in Exception constructor
	c.structConstructors["Exception"] = []ast.Type{ast.TypeString}

	// Populate struct module mapping from program
	for sName, modName := range program.StructModule {
		c.structModule[sName] = modName
	}

	// Register interface types
	for _, ifaceDef := range program.Interfaces {
		ast.RegisterInterfaceType(ifaceDef)
	}

	// First pass: register all function signatures
	for _, fn := range program.Functions {
		var paramTypes []ast.Type
		var paramNames []string
		var defaultValues []ast.Expr
		for _, p := range fn.Params {
			paramTypes = append(paramTypes, p.Type)
			paramNames = append(paramNames, p.Name)
			defaultValues = append(defaultValues, p.DefaultValue)
		}
		c.funcs[fn.Name] = funcSig{
			Params:        paramTypes,
			ParamNames:    paramNames,
			DefaultValues: defaultValues,
			ReturnType:    fn.ReturnType,
			IsPrivate:     fn.IsPrivate,
		}
	}

	// Check module-level let/const declarations in a global scope
	c.pushScope()

	for i := range program.GlobalLets {
		if err := c.checkStmt(&program.GlobalLets[i], ast.TypeVoid); err != nil {
			c.addError(err)
		}
	}

	// Second pass: check function bodies (global scope remains so functions can resolve globals)
	for _, fn := range program.Functions {
		c.pushScope()

		for _, p := range fn.Params {
			c.define(p.Name, p.Type)
		}

		for _, stmt := range fn.Body {
			if err := c.checkStmt(stmt, fn.ReturnType); err != nil {
				c.addError(err)
				break // stop checking this function, but continue to next
			}
		}

		c.popScope()

		if len(c.errors) >= c.maxErrors {
			break
		}
	}

	c.popScope() // pop global scope

	return c.errors
}

// Scope management

func (c *Checker) pushScope() {
	c.scopes = append(c.scopes, make(map[string]ast.Type))
	c.constScopes = append(c.constScopes, make(map[string]bool))
	c.annotationScopes = append(c.annotationScopes, make(map[string][]string))
}

func (c *Checker) popScope() {
	c.scopes = c.scopes[:len(c.scopes)-1]
	c.constScopes = c.constScopes[:len(c.constScopes)-1]
	c.annotationScopes = c.annotationScopes[:len(c.annotationScopes)-1]
}

func (c *Checker) define(name string, typ ast.Type) {
	c.scopes[len(c.scopes)-1][name] = typ
}

func (c *Checker) defineConst(name string) {
	c.constScopes[len(c.constScopes)-1][name] = true
}

func (c *Checker) isConst(name string) bool {
	for i := len(c.constScopes) - 1; i >= 0; i-- {
		if c.constScopes[i][name] {
			return true
		}
	}
	return false
}

func (c *Checker) resolve(name string) (ast.Type, bool) {
	for i := len(c.scopes) - 1; i >= 0; i-- {
		if typ, ok := c.scopes[i][name]; ok {
			return typ, true
		}
	}
	return 0, false
}

func (c *Checker) defineAnnotations(name string, annotations []string) {
	if len(annotations) > 0 {
		c.annotationScopes[len(c.annotationScopes)-1][name] = annotations
	}
}

func (c *Checker) resolveAnnotations(name string) []string {
	for i := len(c.annotationScopes) - 1; i >= 0; i-- {
		if annots, ok := c.annotationScopes[i][name]; ok {
			return annots
		}
	}
	return nil
}
