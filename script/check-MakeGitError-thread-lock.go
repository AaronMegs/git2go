//go:build ignore
// +build ignore

package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/printer"
	"go/token"
	"log"
	"os"
	"path/filepath"
	"strings"
)

var (
	fset = token.NewFileSet()
)

func main() {
	log.SetFlags(0)

	bpkg, err := build.ImportDir(".", 0)
	if err != nil {
		log.Fatal(err)
	}

	pkgs, err := parser.ParseDir(fset, bpkg.Dir, func(fi os.FileInfo) bool { return filepath.Ext(fi.Name()) == ".go" }, 0)
	if err != nil {
		log.Fatal(err)
	}

	for _, pkg := range pkgs {
		if err := checkPkg(pkg); err != nil {
			log.Fatal(err)
		}
	}
	if len(pkgs) == 0 {
		log.Fatal("No packages to check.")
	}
}

var ignoreViolationsInFunc = map[string]bool{
	"MakeGitError":  true,
	"MakeGitError2": true,
}

func checkPkg(pkg *ast.Package) error {
	var violations []string
	ast.Inspect(pkg, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.FuncDecl:
			var b bytes.Buffer
			if err := printer.Fprint(&b, fset, node); err != nil {
				log.Fatal(err)
			}
			src := b.String()

			if !strings.Contains(src, "MakeGitError") || ignoreViolationsInFunc[node.Name.Name] {
				return true
			}

			// Both halves are required. Locking without the deferred unlock
			// pins the goroutine to its OS thread for good, which is a leak
			// rather than a fix, so a function with only one of the two is
			// still reported.
			hasLock := strings.Contains(src, "runtime.LockOSThread()")
			hasUnlock := strings.Contains(src, "defer runtime.UnlockOSThread()")
			if hasLock && hasUnlock {
				return true
			}

			pos := fset.Position(node.Pos())
			detail := "missing both LockOSThread and the deferred UnlockOSThread"
			if hasLock {
				detail = "locks the OS thread but never defers UnlockOSThread"
			} else if hasUnlock {
				detail = "defers UnlockOSThread but never calls LockOSThread"
			}
			violations = append(violations, fmt.Sprintf("%s at %s:%d (%s)", node.Name.Name, pos.Filename, pos.Line, detail))
		}
		return true
	})
	if len(violations) > 0 {
		return fmt.Errorf("%d non-thread-locked calls to MakeGitError found. To fix, add the following to each func below that calls MakeGitError, before the cgo call that might produce the error:\n\n\truntime.LockOSThread()\n\tdefer runtime.UnlockOSThread()\n\n%s", len(violations), strings.Join(violations, "\n"))
	}
	return nil
}
