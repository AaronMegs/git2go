//go:build ignore
// +build ignore

package main

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

func TestFreeInvariants(t *testing.T) {
	const valid = `func (v *Object) Free() {
	if v == nil || v.ptr == nil { return }
	ptr := v.ptr
	v.ptr = nil
	runtime.SetFinalizer(v, nil)
	C.git_object_free(ptr)
}`
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{"valid", valid, ""},
		{"private helper", strings.Replace(valid, "Free()", "free()", 1), ""},
		{"refdb without allowlist", strings.Replace(valid, "*Object", "*RefdbBackend", 1), ""},
		{"shim", strings.Replace(valid, "git_object_free", "_go_git_refdb_backend_free", 1), ""},
		{"C free receiver", strings.Replace(valid, "C.git_object_free(ptr)", "C.free(unsafe.Pointer(ptr))", 1), ""},
		{"reversed nil comparison", strings.Replace(valid, "v.ptr == nil", "nil == v.ptr", 1), ""},
		{"borrowed early return", strings.Replace(valid, "runtime.SetFinalizer", "if !v.owned { return }; runtime.SetFinalizer", 1), ""},
		{"no guard", strings.Replace(valid, "if v == nil || v.ptr == nil { return }", "", 1), "free-idempotent"},
		{"wrong guarded field", strings.Replace(valid, "v.ptr == nil", "v.owner == nil", 1), "free-idempotent"},
		{"guard without return", strings.Replace(valid, "{ return }", "{ logNil() }", 1), "free-idempotent"},
		{"conditional guard", strings.Replace(valid, "v == nil || v.ptr == nil", "v.ptr == nil && v.ready", 1), "free-idempotent"},
		{"guard after release", strings.Replace(strings.Replace(valid, "if v == nil || v.ptr == nil { return }", "", 1), "C.git_object_free(ptr)", "C.git_object_free(ptr); if v.ptr == nil { return }", 1), "free-idempotent"},
		{"wrong cleared field", strings.Replace(valid, "v.ptr = nil", "v.owner = nil", 1), "free-clear-first"},
		{"clear too late", strings.Replace(strings.Replace(valid, "v.ptr = nil", "", 1), "C.git_object_free(ptr)", "C.git_object_free(ptr); v.ptr = nil", 1), "free-clear-first"},
		{"conditional clear", strings.Replace(valid, "v.ptr = nil", "if v.ready { v.ptr = nil }", 1), "free-clear-first"},
		{"field restored", strings.Replace(valid, "v.ptr = nil", "v.ptr = nil; v.ptr = ptr", 1), "free-clear-first"},
		{"free cleared field", strings.Replace(valid, "C.git_object_free(ptr)", "C.git_object_free(v.ptr)", 1), "free-clear-first"},
		{"no finalizer detach", strings.Replace(valid, "runtime.SetFinalizer(v, nil)", "", 1), "free-finalizer"},
		{"wrong finalizer receiver", strings.Replace(valid, "runtime.SetFinalizer(v, nil)", "runtime.SetFinalizer(other, nil)", 1), "free-finalizer"},
		{"late finalizer detach", strings.Replace(strings.Replace(valid, "runtime.SetFinalizer(v, nil)", "", 1), "C.git_object_free(ptr)", "C.git_object_free(ptr); runtime.SetFinalizer(v, nil)", 1), "free-finalizer"},
		{"deferred finalizer detach", strings.Replace(valid, "runtime.SetFinalizer", "defer runtime.SetFinalizer", 1), "free-finalizer"},
		{"conditional finalizer detach", strings.Replace(valid, "runtime.SetFinalizer(v, nil)", "if v.ready { runtime.SetFinalizer(v, nil) }", 1), "free-finalizer"},
		{"string not finalizer", strings.Replace(valid, "runtime.SetFinalizer(v, nil)", `_ = "runtime.SetFinalizer(v, nil)"`, 1), "free-finalizer"},
		{"comment not finalizer", strings.Replace(valid, "runtime.SetFinalizer(v, nil)", "// runtime.SetFinalizer(v, nil)", 1), "free-finalizer"},
		{"pure Go", `func (v *Fixture) Free() { v.data = nil }`, ""},
		{"string not release", `func (v *Fixture) Free() { _ = "C.git_object_free(v.ptr)" }`, ""},
		{"scratch cleanup", `func (v *Fixture) Free() { p := C.CString(v.name); defer C.free(unsafe.Pointer(p)) }`, ""},
		{"unregister preserves state on failure", `func (v *Transport) Free() error {
	if v == nil || v.handle == nil { return nil }
	name := C.CString(v.name)
	defer C.free(unsafe.Pointer(name))
	if ret := C.git_transport_unregister(name); ret < 0 { return MakeGitError(ret) }
	pointerHandles.Untrack(v.handle)
	runtime.SetFinalizer(v, nil)
	v.handle = nil
	return nil
}`, ""},
		{"delegated bad helper", `func (v *Object) Free() { v.free() }
func (v *Object) free() {
	if v == nil || v.ptr == nil { return }
	runtime.SetFinalizer(v, nil)
	C.git_object_free(v.ptr)
	v.ptr = nil
}`, "free-clear-first"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "fixture.go", "package fixture\n"+tc.source, parser.ParseComments)
			if err != nil {
				t.Fatal(err)
			}
			violations := checkFile(fset, "fixture.go", file)
			if tc.want == "" {
				if len(violations) != 0 {
					t.Fatalf("unexpected violations: %+v", violations)
				}
			} else if len(violations) != 1 || violations[0].rule != tc.want {
				t.Fatalf("violations = %+v, want only %s", violations, tc.want)
			}
		})
	}
}

func TestCallbackInvariant(t *testing.T) {
	for _, guard := range []string{"", "defer recoverVoidCallback()"} {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, "callback.go", "package fixture\n//export callback\nfunc callback() { "+guard+" }", parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}
		violations := checkFile(fset, "callback.go", file)
		if guard == "" {
			if len(violations) != 1 || violations[0].rule != "callback-panic-guard" {
				t.Fatalf("unguarded callback: %+v", violations)
			}
		} else if len(violations) != 0 {
			t.Fatalf("guarded callback: %+v", violations)
		}
	}
}
