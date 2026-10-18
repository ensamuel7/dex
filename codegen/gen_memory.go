package codegen

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ensamuel7/dex/ast"
	"github.com/ensamuel7/dex/resolve"
)

// Scope management
func (g *Generator) pushScope() {
	g.scopeStack = append(g.scopeStack, scopeFrame{})
}

func (g *Generator) popScope(out *strings.Builder, prefix string) {
	if len(g.scopeStack) == 0 {
		return
	}
	scope := g.scopeStack[len(g.scopeStack)-1]
	g.scopeStack = g.scopeStack[:len(g.scopeStack)-1]
	for i := len(scope.vars) - 1; i >= 0; i-- {
		sv := scope.vars[i]
		g.emitReleaseVar(out, prefix, sv.name, sv.typ)
	}
	// The releases above already happened, so the entries are dropped without
	// releasing anything. Only an unwind releases through them.
	g.emitCleanupRestore(out, prefix, scope.mark)
}

// registerScopeVar records a variable for release at scope exit, and registers
// its value with the runtime so an exception unwinding past this frame releases
// it too. Emission happens here rather than at the call sites because the two
// have to stay together: a variable released at scope exit but never registered
// leaks on an unwind, and one registered but not released leaks on the normal
// path.
func (g *Generator) registerScopeVar(out *strings.Builder, prefix, name string, typ ast.Type) {
	if !ast.NeedsRelease(typ) {
		return
	}
	if len(g.scopeStack) == 0 {
		return
	}
	idx := len(g.scopeStack) - 1
	sv := scopeVar{name: name, typ: typ}

	switch {
	// An interface local owns the box in its _obj field, so that is what the
	// unwind entry has to name — the value itself is a multi-word struct the
	// cleanup stack cannot hold, and reading it back out of a dead frame is the
	// use-after-free the stack exists to avoid.
	case g.usesExceptions && ast.IsInterfaceType(typ):
		g.markScopeForUnwind(out, prefix, idx)
		if g.isReassigned(name) {
			sv.slot = g.nextCleanupSlot()
			out.WriteString(fmt.Sprintf("%ssize_t %s = _dex_cleanup_push(%s._obj);\n", prefix, sv.slot, name))
		} else {
			out.WriteString(fmt.Sprintf("%s_dex_cleanup_push(%s._obj);\n", prefix, name))
		}
	case g.unwindTrackable(typ):
		g.markScopeForUnwind(out, prefix, idx)
		if g.isReassigned(name) {
			// Assigned again later, so the entry has to be findable: the slot
			// is kept in a C variable the assignment writes through.
			sv.slot = g.nextCleanupSlot()
			out.WriteString(fmt.Sprintf("%ssize_t %s = _dex_cleanup_push(%s);\n", prefix, sv.slot, name))
		} else {
			out.WriteString(fmt.Sprintf("%s_dex_cleanup_push(%s);\n", prefix, name))
		}
	case ast.IsStructType(typ):
		sv.fieldSlots = g.registerStructFieldsForUnwind(out, prefix, idx, name, typ)
	}

	g.scopeStack[idx].vars = append(g.scopeStack[idx].vars, sv)
}

// registerStructFieldsForUnwind registers the heap fields of a struct local.
//
// The struct itself cannot be registered — releasing one means releasing its
// fields through its address, and the unwind cannot safely read a dead frame's
// stack. Its fields are ordinary pointers though, and a struct owns them, so
// registering those is the same bargain as for any other heap local: released
// once, by whichever path runs.
//
// Each field keeps a slot, because every way its value can stop being the
// field's own has to be able to say so: writing the field, assigning the whole
// struct over it, and throwing the struct — a throw consumes it and releases
// its fields, so the entries have to be cleared or the unwind releases them a
// second time.
//
// A field that is itself a struct or an optional is skipped rather than
// descended into: it is still released on the normal path, and leaks on an
// unwind as before.
func (g *Generator) registerStructFieldsForUnwind(out *strings.Builder, prefix string, idx int, name string, typ ast.Type) map[string]string {
	if !g.usesExceptions {
		return nil
	}
	def := ast.GetStructDef(typ)
	if def == nil {
		return nil
	}
	var slots map[string]string
	for _, f := range def.Fields {
		if !ast.IsHeapType(f.Type) {
			continue
		}
		g.markScopeForUnwind(out, prefix, idx)
		slot := g.nextCleanupSlot()
		if slots == nil {
			slots = map[string]string{}
		}
		slots[f.Name] = slot
		out.WriteString(fmt.Sprintf("%ssize_t %s = _dex_cleanup_push(%s.%s);\n", prefix, slot, name, f.Name))
	}
	return slots
}

// markScopeForUnwind gives a scope its cleanup-stack mark, once, on the first
// registration that needs one.
func (g *Generator) markScopeForUnwind(out *strings.Builder, prefix string, idx int) {
	if g.scopeStack[idx].mark != "" {
		return
	}
	g.scopeStack[idx].mark = g.nextCleanupMark()
	out.WriteString(fmt.Sprintf("%ssize_t %s = _dex_cleanup_top;\n", prefix, g.scopeStack[idx].mark))
}

// emitUnwindSlotUpdate points a registered variable's cleanup entry at the
// value it now holds. Emitted after an assignment, which has already released
// the old one — leaving the entry behind would name freed memory.
func (g *Generator) emitUnwindSlotUpdate(out *strings.Builder, prefix, name string) {
	sv := g.lookupScopeVar(name)
	if sv == nil {
		return
	}
	if sv.slot != "" {
		// An interface local's entry names the box it owns, not the value.
		held := name
		if ast.IsInterfaceType(sv.typ) {
			held = name + "._obj"
		}
		out.WriteString(fmt.Sprintf("%s_dex_cleanup_set(%s, %s);\n", prefix, sv.slot, held))
	}
	// Assigning a struct replaces every field at once, so every field's entry
	// moves with it. Sorted, because a map's order would churn the output.
	fields := make([]string, 0, len(sv.fieldSlots))
	for field := range sv.fieldSlots {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	for _, field := range fields {
		out.WriteString(fmt.Sprintf("%s_dex_cleanup_set(%s, %s.%s);\n", prefix, sv.fieldSlots[field], name, field))
	}
}

// emitUnwindFieldUpdate does the same for one field of a struct local.
func (g *Generator) emitUnwindFieldUpdate(out *strings.Builder, prefix, name, field string) {
	sv := g.lookupScopeVar(name)
	if sv == nil || sv.fieldSlots == nil {
		return
	}
	if slot, ok := sv.fieldSlots[field]; ok {
		out.WriteString(fmt.Sprintf("%s_dex_cleanup_set(%s, %s.%s);\n", prefix, slot, name, field))
	}
}

// emitUnwindEntriesCleared gives up a variable's cleanup entries without
// releasing anything: whatever is about to release the values has taken them
// over. Clearing rather than dropping the height, because entries below this
// one on the stack are still live.
func (g *Generator) emitUnwindEntriesCleared(out *strings.Builder, prefix, name string) {
	sv := g.lookupScopeVar(name)
	if sv == nil {
		return
	}
	if sv.slot != "" {
		out.WriteString(fmt.Sprintf("%s_dex_cleanup_set(%s, 0);\n", prefix, sv.slot))
	}
	fields := make([]string, 0, len(sv.fieldSlots))
	for field := range sv.fieldSlots {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	for _, field := range fields {
		out.WriteString(fmt.Sprintf("%s_dex_cleanup_set(%s, 0);\n", prefix, sv.fieldSlots[field]))
	}
}

// lookupScopeVar finds a name's innermost declaration, which is the one in
// scope where a shadowing inner block has redeclared it.
func (g *Generator) lookupScopeVar(name string) *scopeVar {
	for i := len(g.scopeStack) - 1; i >= 0; i-- {
		vars := g.scopeStack[i].vars
		for j := len(vars) - 1; j >= 0; j-- {
			if vars[j].name == name {
				return &vars[j]
			}
		}
	}
	return nil
}

func (g *Generator) nextCleanupSlot() string {
	name := fmt.Sprintf("_dex_slot_%d", g.cleanupMarks)
	g.cleanupMarks++
	return name
}

// unwindTrackable reports whether a value of this type is a single pointer the
// cleanup stack can hold and dex_release can free. A struct is not — see
// registerStructFieldsForUnwind, which registers its fields instead.
func (g *Generator) unwindTrackable(typ ast.Type) bool {
	if !g.usesExceptions {
		return false
	}
	if ast.IsOptionalType(typ) {
		return ast.IsHeapType(ast.OptionalInnerType(typ))
	}
	return ast.IsHeapType(typ)
}

// registerTempForUnwind registers a hoisted temporary, so a throw from inside
// the statement that built it releases it. A temporary is written once and read
// once, so it needs no slot: nothing can replace the value its entry names.
func (g *Generator) registerTempForUnwind(out *strings.Builder, name string, typ ast.Type) {
	if !g.unwindTrackable(typ) {
		return
	}
	out.WriteString(fmt.Sprintf("_dex_cleanup_push(%s); ", name))
	g.stmtTempsRegistered = true
}

// emitCleanupRestore drops the cleanup entries a scope registered, without
// releasing them.
func (g *Generator) emitCleanupRestore(out *strings.Builder, prefix, mark string) {
	if mark == "" {
		return
	}
	out.WriteString(fmt.Sprintf("%s_dex_cleanup_drop_to(%s);\n", prefix, mark))
}

// outermostMark returns the cleanup height to restore when leaving every scope
// from depth down — the first mark at or above it, since a scope with no mark
// registered nothing.
func (g *Generator) outermostMark(depth int) string {
	for i := depth; i < len(g.scopeStack); i++ {
		if g.scopeStack[i].mark != "" {
			return g.scopeStack[i].mark
		}
	}
	return ""
}

func (g *Generator) nextCleanupMark() string {
	name := fmt.Sprintf("_dex_clm_%d", g.cleanupMarks)
	g.cleanupMarks++
	return name
}

// isFieldAssigned reports whether the function being generated ever writes a
// field of this variable. `p.message = x` releases what the field held, so a
// cleanup entry naming the old value would be stale; `p.inner.message = x` is
// the same question about the same variable, which is why only the head of the
// access matters.
func (g *Generator) isFieldAssigned(name string) bool {
	if g.currentFn == nil {
		return true
	}
	found := false
	resolve.VisitStmts(g.currentFn.Body, func(st ast.Stmt) {
		switch s := st.(type) {
		case *ast.FieldAssignStmt:
			if headIdentName(s.Object) == name {
				found = true
			}
		case *ast.IndexAssignStmt:
			// An element of an array held in a field, reached through this
			// variable: the element is replaced, and a field of it may be what
			// was registered.
			if headIdentName(s.Array) == name {
				found = true
			}
		}
	}, nil)
	return found
}

// headIdentName returns the variable an access chain starts from: cmd for
// cmd.action.name, and "" for anything not rooted in a plain name.
func headIdentName(expr ast.Expr) string {
	for {
		switch e := expr.(type) {
		case *ast.Ident:
			return e.Name
		case *ast.FieldAccessExpr:
			expr = e.Object
		case *ast.IndexExpr:
			expr = e.Array
		default:
			return ""
		}
	}
}

// isReassigned reports whether the function being generated ever assigns to a
// name after its declaration. A compound assignment counts, and so does one
// inside a lambda that captured it, and so does passing it to a C function that
// writes through an out-parameter: the write replaces the value exactly as an
// assignment does, and a variable with no unwind slot would leave the cleanup
// stack naming what it held before.
func (g *Generator) isReassigned(name string) bool {
	if g.currentFn == nil {
		return true
	}
	found := false
	resolve.VisitStmts(g.currentFn.Body, func(st ast.Stmt) {
		switch s := st.(type) {
		case *ast.AssignStmt:
			if s.Name == name {
				found = true
			}
		case *ast.CompoundAssignStmt:
			if s.Name == name {
				found = true
			}
		}
	}, func(ex ast.Expr) {
		call, ok := ex.(*ast.CallExpr)
		if !ok || call.Module == "" {
			return
		}
		fd, ok := g.lookupFunc(call.Module, call.Name)
		if !ok {
			return
		}
		for i, arg := range call.Args {
			if id, isIdent := arg.(*ast.Ident); isIdent && id.Name == name && i < len(fd.Params) && ast.IsRefType(fd.Params[i]) {
				found = true
			}
		}
	})
	return found
}

func (g *Generator) emitReleaseVar(out *strings.Builder, prefix, name string, typ ast.Type) {
	if ast.IsOptionalType(typ) {
		inner := ast.OptionalInnerType(typ)
		if ast.IsValueType(inner) {
			// Value-type optional: no cleanup needed
			return
		}
		if ast.IsHeapType(inner) {
			// Heap-type optional: guard with null check
			out.WriteString(fmt.Sprintf("%sif (%s) { dex_release(%s); }\n", prefix, name, name))
			return
		}
		if ast.IsStructType(inner) {
			// Struct-type optional: free heap fields then free pointer
			def := ast.GetStructDef(inner)
			if def != nil {
				out.WriteString(fmt.Sprintf("%sif (%s) {\n", prefix, name))
				for _, f := range def.Fields {
					if ast.NeedsRelease(f.Type) {
						g.emitReleaseVar(out, prefix+"    ", name+"->"+f.Name, f.Type)
					}
				}
				out.WriteString(fmt.Sprintf("%s    free(%s);\n", prefix, name))
				out.WriteString(fmt.Sprintf("%s}\n", prefix))
			}
			return
		}
		return
	}
	// An interface value is two pointers and a table passed by value; the
	// reference it owns is the box in _obj, so the release goes through that
	// field rather than through the value itself.
	if ast.IsInterfaceType(typ) {
		out.WriteString(fmt.Sprintf("%sdex_release(%s._obj);\n", prefix, name))
		return
	}
	if ast.IsHeapType(typ) {
		annots := g.varAnnotations[name]
		if ast.HasAnnotation(annots, ast.AnnotOwned) {
			out.WriteString(fmt.Sprintf("%sdex_owned_free(%s);\n", prefix, name))
		} else if ast.HasAnnotation(annots, ast.AnnotRegion) {
			// Skip — arena handles it
		} else {
			out.WriteString(fmt.Sprintf("%sdex_release(%s);\n", prefix, name))
		}
	} else if ast.IsStructType(typ) {
		def := ast.GetStructDef(typ)
		if def != nil {
			for _, f := range def.Fields {
				if ast.NeedsRelease(f.Type) {
					g.emitReleaseVar(out, prefix, name+"."+f.Name, f.Type)
				}
			}
		}
	}
}

// emitCleanupAll releases all vars in ALL scopes (for return statements), skipping exceptVar
func (g *Generator) emitCleanupAll(out *strings.Builder, prefix string, exceptVar string) {
	for i := len(g.scopeStack) - 1; i >= 0; i-- {
		scope := g.scopeStack[i]
		for j := len(scope.vars) - 1; j >= 0; j-- {
			sv := scope.vars[j]
			if sv.name == exceptVar {
				continue
			}
			g.emitReleaseVar(out, prefix, sv.name, sv.typ)
		}
	}
	g.emitCleanupRestore(out, prefix, g.outermostMark(0))
}

// emitCleanupInnerScopes releases vars from inner scopes down to (and including) targetDepth
func (g *Generator) emitCleanupInnerScopes(out *strings.Builder, prefix string, targetDepth int) {
	for i := len(g.scopeStack) - 1; i >= targetDepth; i-- {
		scope := g.scopeStack[i]
		for j := len(scope.vars) - 1; j >= 0; j-- {
			sv := scope.vars[j]
			g.emitReleaseVar(out, prefix, sv.name, sv.typ)
		}
	}
	g.emitCleanupRestore(out, prefix, g.outermostMark(targetDepth))
}

// emitExcFramePops closes the exception frames a jump leaves behind. A try
// pushes its frame before its setjmp and pops it after the handlers, so a
// return or a break from inside the try body skips that pop — and the frame
// stack is a fixed array, so skipped pops accumulate until it is walked past
// its end.
func (g *Generator) emitExcFramePops(out *strings.Builder, prefix string, frames int) {
	if frames <= 0 {
		return
	}
	out.WriteString(fmt.Sprintf("%s_dex_exc_top -= %d;\n", prefix, frames))
}

// emitDeferredCalls runs the deferred calls this frame registered, most recent
// first, and is emitted at every way out of the frame. The calls themselves are
// on a runtime stack rather than inlined here: which of them to run is a
// question about what actually executed, which only the running program knows.
func (g *Generator) emitDeferredCalls(out *strings.Builder, prefix string) {
	if !g.hasDefers() {
		return
	}
	out.WriteString(prefix + "_dex_defer_run_to(_dex_defer_base);\n")
}

// hasDefers reports whether the function being generated contains a defer, and
// so needs the base height recorded on entry.
func (g *Generator) hasDefers() bool {
	return g.fnHasDefer
}

// isBorrowedExpr reports whether an expression yields a reference owned by
// someone else (a variable, an array element, or a struct field) rather than a
// freshly-created temporary. Borrowed references must be retained when they are
// stored somewhere that outlives the current owner; owned temporaries must not
// be, or they leak.
func isBorrowedExpr(e ast.Expr) bool {
	switch e.(type) {
	case *ast.Ident, *ast.IndexExpr, *ast.FieldAccessExpr:
		return true
	}
	return false
}

// borrowsHeapValue is the generator-aware form of isBorrowedExpr. It exists
// because json.Value does not follow the general rule: indexing one mints a
// reference where indexing an array only borrows the element. Ownership
// decisions go through here so the two predicates cannot disagree.
func (g *Generator) borrowsHeapValue(e ast.Expr) bool {
	if g.typeOfExpr(e) == ast.TypeJsonValue {
		return !g.jsonValueOwned(e)
	}
	// Naming a method builds a closure over the receiver rather than reading a
	// field, so it is a fresh reference despite looking like a field access.
	if fa, ok := e.(*ast.FieldAccessExpr); ok && fa.IsMethodValue {
		return false
	}
	// And naming a plain function builds one over no environment at all.
	if g.isFuncValueIdent(e) {
		return false
	}
	return isBorrowedExpr(e)
}

// isFuncValueIdent reports whether an identifier names a top-level function
// used as a value rather than a variable. genExpr compiles that to a
// dex_closure_new, so it mints a reference — but it is spelled as an Ident, so
// without this both ownership predicates read it as borrowed.
// `let f: fn(int): int = twice` then retained a closure that already held the
// only reference there was and leaked it, and `apply(twice, 4)` minted one
// that nothing ever released.
//
// A local may shadow a function name, so a known variable wins — the same
// order genExpr resolves the identifier in.
func (g *Generator) isFuncValueIdent(e ast.Expr) bool {
	ident, ok := e.(*ast.Ident)
	if !ok {
		return false
	}
	if _, isVar := g.varTypes[ident.Name]; isVar {
		return false
	}
	_, isFn := g.funcs[ident.Name]
	return isFn
}

// emitRetainReturnedLitFields retains the heap fields of a returned struct
// literal that were initialised from a borrowed reference. Those references are
// owned by locals that the caller-side cleanup is about to release, so without
// this the returned struct would carry dangling pointers. Fields built from a
// fresh temporary already carry the only reference and are deliberately skipped
// so ownership simply moves to the caller.
func (g *Generator) emitRetainReturnedLitFields(out *strings.Builder, prefix string, retType ast.Type, lit ast.Expr) {
	g.emitRetainStructLitFields(out, prefix, "_ret_tmp", retType, lit)
}

// emitRetainStructLitFields retains the heap fields of a struct literal that were
// initialised from a borrowed reference. The literal is a C compound literal, so
// it copies those pointers without taking ownership; without this the struct and
// the borrowed variable would each release the same reference at scope exit.
// structAssignments returns the field names and values a struct-producing
// expression assigns, for either way of writing one: a brace literal, or a call
// to the struct's auto-constructor.
//
// The two compile to the same C initialiser, so they have the same ownership
// question — and keying the retain logic on the literal alone meant the
// constructor form stored a borrowed value without taking a reference to it.
// `fn notFound(m: string): Result { return Result(Status.NotFound, m) }` left
// the caller's string in the struct, released it at the call, and released it
// again at scope exit: a dangling read, then a double free.
func structAssignments(expr ast.Expr) (names []string, values []ast.Expr, ok bool) {
	switch e := expr.(type) {
	case *ast.StructLitExpr:
		return e.FieldNames, e.FieldValues, true
	case *ast.CallExpr:
		if !e.IsConstructor || !ast.IsStructType(e.StructType) {
			return nil, nil, false
		}
		def := ast.GetStructDef(e.StructType)
		if def == nil {
			return nil, nil, false
		}
		for _, cp := range def.ConstructorParams {
			names = append(names, cp.Name)
		}
		return names, e.Args, true
	}
	return nil, nil, false
}

func (g *Generator) emitRetainStructLitFields(out *strings.Builder, prefix, target string, structType ast.Type, lit ast.Expr) {
	def := ast.GetStructDef(structType)
	if def == nil {
		return
	}
	litNames, litValues, ok := structAssignments(lit)
	if !ok {
		return
	}
	fieldTypes := make(map[string]ast.Type, len(def.Fields))
	for _, f := range def.Fields {
		fieldTypes[f.Name] = f.Type
	}
	for i, name := range litNames {
		if i >= len(litValues) {
			break
		}
		ft, ok := fieldTypes[name]
		if !ok || !ast.NeedsRelease(ft) {
			continue
		}
		val := litValues[i]
		// A nested struct *literal* is built right here, so the question is not
		// whether it is borrowed — a literal never is — but which of its own
		// fields are. Recurse, exactly as emitReleaseVar descends into it.
		//
		// Asking borrowsHeapValue first skipped this case entirely:
		//
		//	return Sel { source: source, cols: Cols { exprs: exprs, at: at } }
		//
		// retained `source` and nothing else, and the scope then released
		// `exprs` and `at`. The returned Sel carried a freed array and a freed
		// map, and the caller's first push into them hung the program.
		if _, _, isLit := structAssignments(val); isLit && ast.IsStructType(ft) {
			g.emitRetainStructLitFields(out, prefix, target+"."+name, ft, val)
			continue
		}
		if !g.borrowsHeapValue(val) {
			// A fresh temporary already carries the only reference there is;
			// ownership simply moves into the literal.
			continue
		}
		switch {
		case ast.IsHeapType(ft):
			out.WriteString(fmt.Sprintf("%sdex_retain(%s.%s);\n", prefix, target, name))
		case ast.IsInterfaceType(ft):
			// A struct coerced into the field minted its own box and needs no
			// retain. A value that was already an interface is borrowed from
			// whoever still holds it, so the struct takes its own reference.
			if ast.IsInterfaceType(g.typeOfExpr(val)) {
				out.WriteString(fmt.Sprintf("%sdex_retain(%s.%s._obj);\n", prefix, target, name))
			}
		case ast.IsStructType(ft):
			// A nested struct is a value, not a heap type, so it is not
			// retained — but its own heap fields are, because emitReleaseVar
			// recurses into it and releases them.
			//
			// `Result { output: entry }` copied entry's string pointers and
			// took no reference to them, and the scope then released `entry`.
			// The returned Result carried freed strings: a filename came back
			// empty and the allocator was corrupt some calls later.
			g.emitRetainBorrowedStructFields(out, prefix, target+"."+name, ft)
		}
	}
}

// emitRetainBorrowedStructFields retains the reference-counted fields of a
// struct that was *copied* out of somewhere it does not own — an array element,
// a field of another struct, another variable.
//
// The copy duplicates those field pointers without taking a reference, and
// emitReleaseVar releases every one of them at scope exit. Without the matching
// retain the scope frees memory whose real owner is still using it. The symptom
// is not a crash at the copy: it is a hang or a corrupted allocator on the
// *next* allocation, pointing at a line that has nothing to do with it.
//
//	let rule: Rule = rules[i]     // copies siteIds, takes no reference
//	let n: long[] = rule.siteIds  // aliases it again
//	                              // scope exit released both. Twice, from one.
//
// The mirror of emitRetainStructLitFields, for copies rather than literals, and
// it recurses the same way emitReleaseVar does so a nested struct field is not
// left unbalanced.
// emitRetainValue takes a reference to one named value of a given type, which
// is what a container does when it stores something handed to it borrowed. It
// is the single-value form of emitRetainBorrowedStructFields, and the two
// agree case for case -- a struct still recurses into its fields, because a
// struct is a value and the references it holds are the ones that matter.
func (g *Generator) emitRetainValue(out *strings.Builder, prefix, target string, typ ast.Type) {
	switch {
	case ast.IsOptionalType(typ):
		// dex_retain is NULL-safe, which is what absent looks like.
		if ast.IsHeapType(ast.OptionalInnerType(typ)) {
			out.WriteString(fmt.Sprintf("%sdex_retain(%s);\n", prefix, target))
		}
	case ast.IsHeapType(typ):
		out.WriteString(fmt.Sprintf("%sdex_retain(%s);\n", prefix, target))
	case ast.IsInterfaceType(typ):
		out.WriteString(fmt.Sprintf("%sdex_retain(%s._obj);\n", prefix, target))
	case ast.IsStructType(typ):
		g.emitRetainBorrowedStructFields(out, prefix, target, typ)
	}
}

func (g *Generator) emitRetainBorrowedStructFields(out *strings.Builder, prefix, target string, structType ast.Type) {
	def := ast.GetStructDef(structType)
	if def == nil {
		return
	}
	for _, f := range def.Fields {
		if !ast.NeedsRelease(f.Type) {
			continue
		}
		field := target + "." + f.Name
		switch {
		case ast.IsOptionalType(f.Type):
			// Only the heap-backed optionals are released, so only those are
			// retained. dex_retain is NULL-safe, which is what absent looks like.
			if ast.IsHeapType(ast.OptionalInnerType(f.Type)) {
				out.WriteString(fmt.Sprintf("%sdex_retain(%s);\n", prefix, field))
			}
		case ast.IsHeapType(f.Type):
			out.WriteString(fmt.Sprintf("%sdex_retain(%s);\n", prefix, field))
		case ast.IsInterfaceType(f.Type):
			out.WriteString(fmt.Sprintf("%sdex_retain(%s._obj);\n", prefix, field))
		case ast.IsStructType(f.Type):
			g.emitRetainBorrowedStructFields(out, prefix, field, f.Type)
		}
	}
}

// hasHeapVarsInScope checks if there are any heap vars tracked in any scope
func (g *Generator) hasHeapVarsInScope() bool {
	for _, scope := range g.scopeStack {
		if len(scope.vars) > 0 {
			return true
		}
	}
	return false
}
