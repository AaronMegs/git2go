//go:build !libgit2_no_reftable
// +build !libgit2_no_reftable

package git

/*
#include <git2.h>
#include <git2/sys/refdb_backend.h>

// Fail the build loudly if the enum values ever drift, since RefdbType is
// converted straight into git_refdb_t.
typedef char git2go_refdb_files_value_must_be_1[(GIT_REFDB_FILES == 1) ? 1 : -1];
typedef char git2go_refdb_reftable_value_must_be_2[(GIT_REFDB_REFTABLE == 2) ? 1 : -1];
*/
import "C"
import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

// applyRefdbType writes the requested reference-storage backend into the C init
// options.
//
// This variant is compiled by default, because the libgit2 baseline this git2go
// targets always exposes the `refdb_type` field and the `git_refdb_t` enum.
// Build with the `libgit2_no_reftable` tag to link against a libgit2 that
// predates reftable support (see reftable_off.go).
func applyRefdbType(copts *C.git_repository_init_options, t RefdbType) error {
	copts.refdb_type = C.git_refdb_t(t)
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

	backend = &RefdbBackend{ptr: ptr, owner: v}
	runtime.SetFinalizer(backend, (*RefdbBackend).Free)
	return backend, nil
}

// IsReftableSupported reports whether the linked libgit2 build supports the
// reftable reference storage backend.
//
// libgit2 does not expose a GIT_FEATURE_REFTABLE flag, so this probes support
// by attempting to initialize a throwaway bare repository with the reftable
// backend in a temporary directory. The probe repository is always removed
// before returning. The probe matters for dynamically linked builds, where the
// headers used at compile time may be newer than the library loaded at runtime.
//
// The result is cached: it depends only on the linked library, which cannot
// change during the process's lifetime. Without caching, every call would
// create and delete a repository on disk, which is far too expensive for a
// predicate that callers reasonably treat as a cheap capability check.
//
// In a build with the `libgit2_no_reftable` tag this always returns false
// (see reftable_off.go), because reftable cannot be requested at all.
func IsReftableSupported() bool {
	reftableSupportedOnce.Do(func() {
		reftableSupported = probeReftableSupport()
	})
	return reftableSupported
}

var (
	reftableSupportedOnce sync.Once
	reftableSupported     bool
)

func probeReftableSupport() bool {
	dir, err := os.MkdirTemp("", "git2go-reftable-probe")
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
