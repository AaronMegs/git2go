//go:build ignore
// +build ignore

// check-binding-invariants enforces the cross-language conventions that this
// binding layer relies on but the Go compiler cannot express.
//
// It is the counterpart of check-MakeGitError-thread-lock.go, which guards the
// thread-local error invariant. The rules here are documented in
// docs/binding-design-guidelines.md; encoding them as a build step keeps the
// documentation from silently drifting away from the code.
//
// Rules
//
//  1. Every //export'ed callback must defer a panic guard. Coverage of user
//     code by that guard remains a review obligation.
//
//  2. Free methods and private free() helpers releasing C pointers must check
//     the released field for nil, save and clear it before release, and detach
//     the finalizer before release. These syntax checks protect sequential
//     cleanup conventions; they do not make concurrent Free/use safe.
//     KeepAlive on a parent depends on ownership and remains a review item.
//
// Both rules support an explicit opt-out comment on the line above the
// declaration, so that a deliberate exception is visible in review rather than
// silently tolerated:
//
//	//git2go:allow-unguarded-callback reason
//	//git2go:allow-nonidempotent-free reason
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	allowUnguardedCallback = "//git2go:allow-unguarded-callback"
	allowNonIdempotentFree = "//git2go:allow-nonidempotent-free"
)

type violation struct {
	pos  token.Position
	rule string
	msg  string
}

func main() {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return filepath.Ext(fi.Name()) == ".go"
	}, parser.ParseComments)
	if err != nil {
		log.Fatal(err)
	}

	var violations []violation
	for _, pkg := range pkgs {
		for path, file := range pkg.Files {
			violations = append(violations, checkFile(fset, path, file)...)
		}
	}

	if len(violations) == 0 {
		return
	}
	sort.Slice(violations, func(i, j int) bool {
		a, b := violations[i], violations[j]
		if a.pos.Filename != b.pos.Filename {
			return a.pos.Filename < b.pos.Filename
		}
		if a.pos.Offset != b.pos.Offset {
			return a.pos.Offset < b.pos.Offset
		}
		return a.rule < b.rule
	})
	fmt.Fprintf(os.Stderr, "binding invariant violations:\n\n")
	for _, v := range violations {
		fmt.Fprintf(os.Stderr, "  %s:%d: [%s] %s\n", v.pos.Filename, v.pos.Line, v.rule, v.msg)
	}
	fmt.Fprintf(os.Stderr, "\nSee docs/binding-design-guidelines.md.\n")
	os.Exit(1)
}

func checkFile(fset *token.FileSet, path string, file *ast.File) []violation {
	var out []violation

	// Map a line number to the comment text ending on the line above it, so
	// opt-out markers can be matched against declarations.
	markers := map[int]string{}
	for _, cg := range file.Comments {
		for _, c := range cg.List {
			markers[fset.Position(c.Pos()).Line] = c.Text
		}
	}
	markerAbove := func(declLine int, want string) bool {
		for line := declLine - 1; line >= declLine-3 && line > 0; line-- {
			if strings.HasPrefix(markers[line], want) {
				return true
			}
		}
		return false
	}

	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		line := fset.Position(fn.Pos()).Line

		if isExportedCallback(fn) {
			if !hasPanicGuard(fn) && !markerAbove(line, allowUnguardedCallback) {
				out = append(out, violation{
					pos:  fset.Position(fn.Pos()),
					rule: "callback-panic-guard",
					msg: fmt.Sprintf("//export %s must defer a panic guard (recoverCallback / "+
						"recoverCallbackCode / recoverVoidCallback) before running user code", fn.Name.Name),
				})
			}
		}

		if recv, recvVar, isFree := freeMethod(fn); isFree && !markerAbove(line, allowNonIdempotentFree) {
			out = append(out, checkFree(fset, fn, recv, recvVar)...)
		}
	}
	return out
}

// checkFree recognizes the straight-line lifecycle pattern, not arbitrary Go
// control flow. Only C free calls on receiver fields (or their local aliases)
// are releases: unregister operations and scratch CString cleanup are not.
func checkFree(fset *token.FileSet, fn *ast.FuncDecl, recv, recvVar string) []violation {
	var out []violation
	aliases := map[string]string{}
	cleared := map[string]bool{}
	detached := false
	key := recv + "." + fn.Name.Name
	for index, stmt := range fn.Body.List {
		ast.Inspect(stmt, func(node ast.Node) bool {
			if _, ok := node.(*ast.FuncLit); ok {
				return false
			}
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) != 1 || !isCRelease(call) {
				return true
			}
			field := pointerField(call.Args[0], recvVar, aliases)
			if field == "" {
				return true
			}
			pos := fset.Position(call.Pos())
			if !hasNilGuard(fn.Body.List[:index], recvVar, field) {
				out = append(out, violation{pos, "free-idempotent",
					fmt.Sprintf("%s must return early when %s.%s is nil", key, recvVar, field)})
			}
			// Passing the cleared field itself would free nil and leak the
			// original allocation. The release must use its saved alias.
			if !cleared[field] || pointerField(call.Args[0], recvVar, nil) != "" {
				out = append(out, violation{pos, "free-clear-first",
					fmt.Sprintf("%s must save %s.%s, clear that field, then release the saved pointer", key, recvVar, field)})
			}
			if !detached {
				out = append(out, violation{pos, "free-finalizer",
					fmt.Sprintf("%s must call runtime.SetFinalizer(%s, nil) before releasing the pointer", key, recvVar)})
			}
			return true
		})
		if expr, ok := stmt.(*ast.ExprStmt); ok {
			if call, ok := expr.X.(*ast.CallExpr); ok && selectorIs(call.Fun, "runtime", "SetFinalizer") &&
				len(call.Args) == 2 && identIs(call.Args[0], recvVar) {
				detached = identIs(call.Args[1], "nil")
			}
		}
		assign, ok := stmt.(*ast.AssignStmt)
		if !ok || len(assign.Lhs) != len(assign.Rhs) {
			continue
		}
		origins := make([]string, len(assign.Rhs))
		for i, rhs := range assign.Rhs {
			origins[i] = pointerField(rhs, recvVar, aliases)
		}
		for i, lhs := range assign.Lhs {
			if id, ok := lhs.(*ast.Ident); ok {
				aliases[id.Name] = origins[i]
			}
			if field := pointerField(lhs, recvVar, nil); field != "" {
				cleared[field] = identIs(assign.Rhs[i], "nil")
			}
		}
	}
	return out
}

func isCRelease(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && identIs(sel.X, "C") && (sel.Sel.Name == "free" ||
		((strings.HasPrefix(sel.Sel.Name, "git_") || strings.HasPrefix(sel.Sel.Name, "_go_git_")) &&
			strings.HasSuffix(sel.Sel.Name, "_free")))
}

func identIs(expr ast.Expr, name string) bool {
	id, ok := expr.(*ast.Ident)
	return ok && id.Name == name
}

func selectorIs(expr ast.Expr, receiver, name string) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	return ok && identIs(sel.X, receiver) && sel.Sel.Name == name
}

func pointerField(expr ast.Expr, recv string, aliases map[string]string) string {
	switch expr := expr.(type) {
	case *ast.Ident:
		return aliases[expr.Name]
	case *ast.SelectorExpr:
		if identIs(expr.X, recv) {
			return expr.Sel.Name
		}
	case *ast.ParenExpr:
		return pointerField(expr.X, recv, aliases)
	case *ast.CallExpr:
		if len(expr.Args) == 1 && selectorIs(expr.Fun, "unsafe", "Pointer") {
			return pointerField(expr.Args[0], recv, aliases)
		}
	}
	return ""
}

func hasNilGuard(statements []ast.Stmt, recv, field string) bool {
	if len(statements) > 2 {
		statements = statements[:2]
	}
	for _, stmt := range statements {
		guard, ok := stmt.(*ast.IfStmt)
		if !ok || guard.Init != nil || len(guard.Body.List) != 1 {
			continue
		}
		if _, returns := guard.Body.List[0].(*ast.ReturnStmt); returns && nilCondition(guard.Cond, recv, field) {
			return true
		}
	}
	return false
}

func nilCondition(expr ast.Expr, recv, field string) bool {
	if paren, ok := expr.(*ast.ParenExpr); ok {
		return nilCondition(paren.X, recv, field)
	}
	binary, ok := expr.(*ast.BinaryExpr)
	if !ok {
		return false
	}
	if binary.Op == token.LOR {
		return nilCondition(binary.X, recv, field) || nilCondition(binary.Y, recv, field)
	}
	return binary.Op == token.EQL &&
		((selectorIs(binary.X, recv, field) && identIs(binary.Y, "nil")) ||
			(selectorIs(binary.Y, recv, field) && identIs(binary.X, "nil")))
}

// isExportedCallback reports whether fn carries a //export directive.
func isExportedCallback(fn *ast.FuncDecl) bool {
	if fn.Doc == nil {
		return false
	}
	for _, c := range fn.Doc.List {
		if strings.HasPrefix(c.Text, "//export ") {
			return true
		}
	}
	return false
}

// hasPanicGuard reports whether the function defers a recover helper before it
// runs any user-supplied code.
//
// The guard does not have to be the very first statement: the refdb teardown
// callbacks legitimately resolve and untrack their handle first, then guard
// only the user Free call. What matters is that a defer covering recover()
// exists among the leading statements.
func hasPanicGuard(fn *ast.FuncDecl) bool {
	for _, stmt := range fn.Body.List {
		def, ok := stmt.(*ast.DeferStmt)
		if !ok {
			continue
		}
		switch call := def.Call.Fun.(type) {
		case *ast.Ident:
			if strings.HasPrefix(call.Name, "recover") {
				return true
			}
		case *ast.FuncLit:
			// defer func() { _ = recover() }()
			if bodyCallsRecover(call.Body) {
				return true
			}
		}
	}
	return false
}

func bodyCallsRecover(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name == "recover" {
			found = true
		}
		return !found
	})
	return found
}

// freeMethod reports the receiver type name and receiver variable when fn is a
// Free method or the private free() helper some types delegate to.
func freeMethod(fn *ast.FuncDecl) (typeName, recvVar string, ok bool) {
	if (fn.Name.Name != "Free" && fn.Name.Name != "free") || fn.Recv == nil || len(fn.Recv.List) != 1 {
		return "", "", false
	}
	field := fn.Recv.List[0]
	if len(field.Names) == 1 {
		recvVar = field.Names[0].Name
	}
	switch t := field.Type.(type) {
	case *ast.StarExpr:
		if id, isIdent := t.X.(*ast.Ident); isIdent {
			return id.Name, recvVar, true
		}
	case *ast.Ident:
		return t.Name, recvVar, true
	}
	return "", "", false
}
