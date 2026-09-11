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
//  1. Every //export'ed callback must defer a panic guard as its first
//     statement. A panic unwinding across the cgo boundary is undefined
//     behaviour, because the intervening libgit2 C frames carry no unwind
//     information.
//
//  2. Every exported Free method must be idempotent: it has to nil-check the
//     receiver's pointer before releasing it, so that an explicit call plus a
//     deferred cleanup cannot double free.
//
// Both rules support an explicit opt-out comment on the line above the
// declaration, so that a deliberate exception is visible in review rather than
// silently tolerated:
//
//	//git2go:allow-unguarded-callback reason
//	//git2go:allow-nonidempotent-free reason
package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"log"
	"os"
	"path/filepath"
	"strings"
)

const (
	allowUnguardedCallback = "//git2go:allow-unguarded-callback"
	allowNonIdempotentFree = "//git2go:allow-nonidempotent-free"
)

// freeAllowlist holds Free methods that legitimately cannot follow the
// nil-check pattern. Keep this list short and justified.
var freeAllowlist = map[string]string{
	// Frees a C struct it does not own a Go wrapper for.
	"RefdbBackend.Free": "clears ptr and owner explicitly, verified by TestRefdbBackendOwnershipTransferIsIdempotent",
}

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

		if recv, isFree := freeMethod(fn); isFree {
			// Only Free methods that actually hand a pointer to libgit2 are in
			// scope. Pure-Go implementations (test fixtures, interface
			// implementations) have nothing to double free.
			if !releasesCPointer(fset, fn) {
				continue
			}
			key := recv + ".Free"
			if _, allowed := freeAllowlist[key]; allowed {
				continue
			}
			if !hasNilGuard(fset, fn) && !markerAbove(line, allowNonIdempotentFree) {
				out = append(out, violation{
					pos:  fset.Position(fn.Pos()),
					rule: "free-idempotent",
					msg: fmt.Sprintf("%s must nil-check its pointer before releasing it so that "+
						"calling it twice is safe", key),
				})
			}
		}
	}
	return out
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

// freeMethod reports the receiver type name when fn is an exported Free method.
func freeMethod(fn *ast.FuncDecl) (string, bool) {
	if fn.Name.Name != "Free" || fn.Recv == nil || len(fn.Recv.List) != 1 {
		return "", false
	}
	switch t := fn.Recv.List[0].Type.(type) {
	case *ast.StarExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			return id.Name, true
		}
	case *ast.Ident:
		return t.Name, true
	}
	return "", false
}

// releasesCPointer reports whether the body calls into C, which is what makes
// a double Free dangerous. A pure-Go Free (test fixture, interface
// implementation) has nothing to protect.
func releasesCPointer(fset *token.FileSet, fn *ast.FuncDecl) bool {
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fset, fn.Body); err != nil {
		return false
	}
	src := buf.String()
	return strings.Contains(src, "C.git_") || strings.Contains(src, "C._go_git_") ||
		strings.Contains(src, "C.free(")
}

// hasNilGuard reports whether the body opens with a nil comparison, which is
// how the idempotent Free pattern starts.
func hasNilGuard(fset *token.FileSet, fn *ast.FuncDecl) bool {
	if len(fn.Body.List) == 0 {
		return false
	}
	// Only the leading statements matter; a nil check further down does not
	// protect the release call above it.
	limit := 2
	if len(fn.Body.List) < limit {
		limit = len(fn.Body.List)
	}
	for _, stmt := range fn.Body.List[:limit] {
		ifStmt, ok := stmt.(*ast.IfStmt)
		if !ok {
			continue
		}
		var buf bytes.Buffer
		if err := printer.Fprint(&buf, fset, ifStmt.Cond); err != nil {
			continue
		}
		if strings.Contains(buf.String(), "nil") {
			return true
		}
	}
	return false
}
