//go:build !libgit2_reftable
// +build !libgit2_reftable

package git

/*
#include <git2.h>
*/
import "C"

// applyRefdbType is a no-op in builds without the `libgit2_reftable` tag.
//
// The files-only build deliberately leaves refdb_type at its default.
// Requesting a non-default backend (i.e. RefdbReftable) is reported as an
// error rather than silently producing a files repository.
func applyRefdbType(copts *C.git_repository_init_options, t RefdbType) error {
	if t == RefdbDefault || t == RefdbFiles {
		return nil
	}
	return &GitError{
		Message: "reftable reference storage requires building git2go with the " +
			"\"libgit2_reftable\" build tag against a reftable-capable libgit2",
		Class: ErrorClassNone,
		Code:  ErrorCodeInvalid,
	}
}

// NewRefdbBackendReftable is unavailable in builds without the
// `libgit2_reftable` tag; it returns an error explaining how to enable it.
//
// Wraps `git_refdb_backend_reftable` in the tagged build (see reftable_on.go).
func (v *Repository) NewRefdbBackendReftable() (backend *RefdbBackend, err error) {
	return nil, &GitError{
		Message: "reftable backend requires building git2go with the " +
			"\"libgit2_reftable\" build tag against a reftable-capable libgit2",
		Class: ErrorClassNone,
		Code:  ErrorCodeInvalid,
	}
}

// IsReftableSupported reports whether the linked libgit2 build supports the
// reftable reference storage backend. Without the `libgit2_reftable` build
// tag this is always false.
func IsReftableSupported() bool {
	return false
}
