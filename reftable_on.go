//go:build libgit2_reftable
// +build libgit2_reftable

package git

/*
#cgo CFLAGS: -DGIT2GO_HAS_REFDB_BACKEND_INIT
#include <git2.h>
#include <git2/sys/refdb_backend.h>

extern int _go_git_refdb_backend_invoke_init(git_refdb_backend *backend, const char *head_target, uint32_t mode, uint32_t flags);
*/
import "C"
import (
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"unsafe"
)

// reftableSupported is a compile-time constant reporting whether this build of
// git2go includes reftable bindings (i.e. was built with the
// `libgit2_reftable` build tag against a reftable-capable libgit2).
const reftableSupported = true

// applyRefdbType writes the requested reference-storage backend into the C
// init options. This variant is compiled only with the `libgit2_reftable`
// build tag, where the underlying libgit2 exposes the `refdb_type` field and
// the `git_refdb_t` enum.
func applyRefdbType(copts *C.git_repository_init_options, t RefdbType) error {
	copts.refdb_type = C.git_refdb_t(t)
	return nil
}

// invokeRefdbBackendInit is an internal bridge used to verify the optional
// latest-main init callback. Repository initialization normally selects its
// own built-in backend, so libgit2 has no public entry point that can invoke a
// separately constructed custom backend's init function.
func invokeRefdbBackendInit(backend *RefdbBackend, initialHead *string, mode RepositoryInitMode, flags RefdbBackendInitFlag) error {
	if backend == nil || backend.ptr == nil {
		return &GitError{Message: "refdb backend is nil or already freed", Class: ErrorClassInvalid, Code: ErrorCodeInvalid}
	}
	var cInitialHead *C.char
	if initialHead != nil {
		cInitialHead = C.CString(*initialHead)
		defer C.free(unsafe.Pointer(cInitialHead))
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ret := C._go_git_refdb_backend_invoke_init(backend.ptr, cInitialHead, C.uint32_t(mode), C.uint32_t(flags))
	runtime.KeepAlive(backend)
	if ret < 0 {
		return MakeGitError(ret)
	}
	return nil
}

// NewRefdbBackendReftable explicitly constructs the reftable-based refdb
// backend for a repository.
//
// Under normal usage this backend is created for you when a repository that
// uses the reftable format is opened; this is provided for advanced scenarios
// where you want to construct the reftable backend explicitly (for example to
// attach it to a Refdb created with NewRefdb).
//
// This function is only available when git2go is built with the
// `libgit2_reftable` build tag against a libgit2 that has reftable support
// (PR #7117 or later on main). Without that tag, calling it returns an error
// (see refdb_noreftable.go).
//
// Example (attach an explicit reftable backend to a fresh refdb):
//
//	refdb, err := repo.NewRefdb()
//	if err != nil { /* ... */ }
//	backend, err := repo.NewRefdbBackendReftable()
//	if err != nil { /* reftable unsupported by this build */ }
//	if err := refdb.SetBackend(backend); err != nil { /* ... */ }
//	if err := repo.SetRefdb(refdb); err != nil { /* ... */ }
//
// Wraps `git_refdb_backend_reftable`.
func (v *Repository) NewRefdbBackendReftable() (backend *RefdbBackend, err error) {
	var ptr *C.git_refdb_backend

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	ret := C.git_refdb_backend_reftable(&ptr, v.ptr)
	runtime.KeepAlive(v)
	if ret < 0 {
		return nil, MakeGitError(ret)
	}

	backend = &RefdbBackend{ptr: ptr}
	return backend, nil
}

// IsReftableSupported reports whether the linked libgit2 build supports the
// reftable reference storage backend.
//
// libgit2 does not expose a GIT_FEATURE_REFTABLE flag, so this probes support
// by attempting to initialize a throwaway bare repository with the reftable
// backend in a temporary directory. The probe repository is always removed
// before returning.
//
// In a build without the `libgit2_reftable` tag this always returns false
// (see refdb_noreftable.go), because reftable cannot be requested at all.
func IsReftableSupported() bool {
	dir, err := ioutil.TempDir("", "git2go-reftable-probe")
	if err != nil {
		return false
	}
	defer os.RemoveAll(dir)

	repo, err := InitRepositoryExt(filepath.Join(dir, "probe"), &RepositoryInitOptions{
		Flags:     RepositoryInitMkpath | RepositoryInitBare,
		RefdbType: RefdbReftable,
	})
	if err != nil {
		return false
	}
	repo.Free()
	return true
}
