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
	ptr   *C.git_refdb_backend
	owner *Repository
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
	if v == nil || v.ptr == nil {
		return &GitError{Message: "refdb is nil or already freed", Class: ErrorClassInvalid, Code: ErrorCodeInvalid}
	}
	if backend == nil || backend.ptr == nil {
		return &GitError{Message: "refdb backend is nil or already freed", Class: ErrorClassInvalid, Code: ErrorCodeInvalid}
	}
	if backend.owner != nil && v.r != nil && backend.owner != v.r {
		return &GitError{Message: "refdb backend belongs to a different repository", Class: ErrorClassInvalid, Code: ErrorCodeInvalid}
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	ptr := backend.ptr
	ret := C.git_refdb_set_backend(v.ptr, ptr)
	runtime.KeepAlive(v)
	runtime.KeepAlive(backend)
	if ret < 0 {
		// libgit2 did not take ownership; the caller may retry or free it.
		return MakeGitError(ret)
	}
	// Ownership transferred to the refdb. The Refdb itself keeps the repository
	// alive, so the temporary backend wrapper no longer needs its owner anchor.
	backend.ptr = nil
	backend.owner = nil
	return nil
}

// Refdb returns the reference database for this repository.
//
// If no custom refdb has been set, libgit2 returns the default database for
// the repository (files or reftable, depending on `extensions.refStorage`).
// The returned Refdb must be freed once it is no longer used.
//
// Wraps `git_repository_refdb`.
func (v *Repository) Refdb() (refdb *Refdb, err error) {
	var ptr *C.git_refdb

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	ret := C.git_repository_refdb(&ptr, v.ptr)
	runtime.KeepAlive(v)
	if ret < 0 {
		return nil, MakeGitError(ret)
	}

	refdb = &Refdb{ptr: ptr, r: v}
	runtime.SetFinalizer(refdb, (*Refdb).Free)
	return refdb, nil
}

// OpenRefdb creates a new reference database and automatically adds the
// repository's default backend (files or reftable, as configured by
// `extensions.refStorage`).
//
// Unlike NewRefdb, the returned Refdb is immediately usable for read/write
// without calling SetBackend.
//
// Wraps `git_refdb_open`.
func (v *Repository) OpenRefdb() (refdb *Refdb, err error) {
	var ptr *C.git_refdb

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	ret := C.git_refdb_open(&ptr, v.ptr)
	runtime.KeepAlive(v)
	if ret < 0 {
		return nil, MakeGitError(ret)
	}

	refdb = &Refdb{ptr: ptr, r: v}
	runtime.SetFinalizer(refdb, (*Refdb).Free)
	return refdb, nil
}

// Compress suggests that the refdb compress or optimize its references.
//
// The exact behaviour is backend specific:
//   - for the files backend this packs loose references;
//   - for the reftable backend this compacts the reftable stack.
//
// Wraps `git_refdb_compress`.
func (v *Refdb) Compress() error {
	if v == nil || v.ptr == nil {
		return &GitError{Message: "refdb is nil or already freed", Class: ErrorClassInvalid, Code: ErrorCodeInvalid}
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	ret := C.git_refdb_compress(v.ptr)
	runtime.KeepAlive(v)
	if ret < 0 {
		return MakeGitError(ret)
	}
	return nil
}

// NewRefdbBackendFs explicitly constructs the filesystem-based (loose +
// packed refs) refdb backend for a repository.
//
// Under normal usage this backend is created for you when a repository is
// opened; this is provided for advanced scenarios such as wrapping or
// stacking backends manually.
//
// Wraps `git_refdb_backend_fs`.
func (v *Repository) NewRefdbBackendFs() (backend *RefdbBackend, err error) {
	var ptr *C.git_refdb_backend

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	ret := C.git_refdb_backend_fs(&ptr, v.ptr)
	runtime.KeepAlive(v)
	if ret < 0 {
		return nil, MakeGitError(ret)
	}

	backend = &RefdbBackend{ptr: ptr, owner: v}
	runtime.SetFinalizer(backend, (*RefdbBackend).Free)
	return backend, nil
}

// NewRefdbBackendReftable explicitly constructs the reftable-based refdb
// backend for a repository. It is only available when git2go is built with
// the `libgit2_reftable` build tag against a libgit2 that has reftable
// support (see reftable_on.go / reftable_off.go).

func (v *RefdbBackend) Free() {
	if v == nil || v.ptr == nil {
		return
	}
	ptr := v.ptr
	v.ptr = nil
	owner := v.owner
	v.owner = nil
	runtime.SetFinalizer(v, nil)
	C._go_git_refdb_backend_free(ptr)
	runtime.KeepAlive(owner)
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

// String returns the canonical `extensions.refStorage` config value for the
// backend, matching the tokens used by both git and libgit2 ("files" /
// "reftable"). RefdbDefault reports "files" since that is libgit2's current
// default.
func (t RefdbType) String() string {
	switch t {
	case RefdbReftable:
		return "reftable"
	case RefdbFiles, RefdbDefault:
		return "files"
	default:
		return "unknown"
	}
}

// RefStorageFormat reports which reference storage backend the repository is
// using, by reading the `extensions.refStorage` configuration entry.
//
// A repository without that extension set uses the traditional files backend,
// so this returns RefdbFiles in that case. A value of "reftable" maps to
// RefdbReftable.
//
// This is the recommended way to detect reftable repositories at runtime,
// since libgit2 does not expose a GIT_FEATURE_REFTABLE feature flag.
func (v *Repository) RefStorageFormat() (RefdbType, error) {
	cfg, err := v.Config()
	if err != nil {
		return RefdbDefault, err
	}
	defer cfg.Free()

	val, err := cfg.LookupString("extensions.refStorage")
	if err != nil {
		if IsErrorCode(err, ErrorCodeNotFound) {
			// No extension configured: default files backend.
			return RefdbFiles, nil
		}
		return RefdbDefault, err
	}

	switch val {
	case "reftable":
		return RefdbReftable, nil
	case "files", "":
		return RefdbFiles, nil
	default:
		// Unknown/future value: surface it to the caller as files-compatible
		// default rather than guessing, but do not error.
		return RefdbFiles, nil
	}
}
