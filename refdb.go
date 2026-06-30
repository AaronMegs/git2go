package git

/*
#include <git2.h>
#include <git2/sys/refdb_backend.h>

extern void _go_git_refdb_backend_free(git_refdb_backend *backend);
*/
import "C"
import (
	"runtime"
	"unsafe"
)

type Refdb struct {
	doNotCompare
	ptr *C.git_refdb
	r   *Repository
}

type RefdbBackend struct {
	doNotCompare
	ptr *C.git_refdb_backend
}

func (v *Repository) NewRefdb() (refdb *Refdb, err error) {
	var ptr *C.git_refdb

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	ret := C.git_refdb_new(&ptr, v.ptr)
	if ret < 0 {
		return nil, MakeGitError(ret)
	}

	refdb = &Refdb{ptr: ptr, r: v}
	runtime.SetFinalizer(refdb, (*Refdb).Free)
	return refdb, nil
}

func NewRefdbBackendFromC(ptr unsafe.Pointer) (backend *RefdbBackend) {
	backend = &RefdbBackend{ptr: (*C.git_refdb_backend)(ptr)}
	return backend
}

func (v *Refdb) SetBackend(backend *RefdbBackend) (err error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	ret := C.git_refdb_set_backend(v.ptr, backend.ptr)
	runtime.KeepAlive(v)
	runtime.KeepAlive(backend)
	if ret < 0 {
		backend.Free()
		return MakeGitError(ret)
	}
	return nil
}

func (v *RefdbBackend) Free() {
	runtime.SetFinalizer(v, nil)
	C._go_git_refdb_backend_free(v.ptr)
}

// RefdbType selects which on-disk reference storage format a repository uses.
//
// This mirrors the upstream libgit2 `git_refdb_t` enumeration introduced in
// master (PR #7117). The default zero value means "use libgit2's default",
// which is currently the traditional `files` backend (loose + packed refs).
//
// Reftable support requires:
//   - a libgit2 build that includes PR #7117 (post `af1e2fa3d` on `main`);
//     it is NOT available in released v1.9.3 / v1.9.4.
//   - the `extensions.refStorage = reftable` configuration entry on the
//     resulting repository (set automatically by `git_repository_init_ext`
//     when `RefdbReftable` is requested).
//
// On a libgit2 build that does not understand `RefdbReftable`, attempting
// to init a repository with this value will fail with an error from
// libgit2 itself.
type RefdbType int

const (
	// RefdbDefault asks libgit2 to use its default backend (currently "files").
	// Equivalent to passing 0 in the C API.
	RefdbDefault RefdbType = 0
	// RefdbFiles selects the traditional loose + packed refs storage.
	// Maps to GIT_REFDB_FILES (= 1) upstream.
	RefdbFiles RefdbType = 1
	// RefdbReftable selects the reftable storage backend.
	// Maps to GIT_REFDB_REFTABLE (= 2) upstream. Requires a libgit2 build
	// that includes reftable support (see RefdbType doc comment).
	RefdbReftable RefdbType = 2
)
