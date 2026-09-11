//go:build libgit2_no_reftable
// +build libgit2_no_reftable

package git

/*
#include <git2.h>
*/
import "C"

// This file is the opt-out counterpart of reftable_on.go. Build git2go with the
// `libgit2_no_reftable` tag to link against a libgit2 that has the promoted
// typed object-id API but predates reftable support, where
// `git_repository_init_options.refdb_type` and `git_refdb_backend_reftable` do
// not exist yet.

// applyRefdbType is a no-op in builds with the `libgit2_no_reftable` tag.
//
// The files-only build deliberately leaves refdb_type at its default.
// Requesting a non-default backend (i.e. RefdbReftable) is reported as an
// error rather than silently producing a files repository.
func applyRefdbType(copts *C.git_repository_init_options, t RefdbType) error {
	if t == RefdbDefault || t == RefdbFiles {
		return nil
	}
	return &GitError{
		Message: "reftable reference storage is unavailable: git2go was built " +
			"with the \"libgit2_no_reftable\" tag",
		Class: ErrorClassNone,
		Code:  ErrorCodeInvalid,
	}
}

// NewRefdbBackendReftable is unavailable in builds with the
// `libgit2_no_reftable` tag; it returns an error explaining why.
//
// Wraps `git_refdb_backend_reftable` in the default build (see reftable_on.go).
func (v *Repository) NewRefdbBackendReftable() (backend *RefdbBackend, err error) {
	return nil, &GitError{
		Message: "reftable backend is unavailable: git2go was built with the " +
			"\"libgit2_no_reftable\" tag",
		Class: ErrorClassNone,
		Code:  ErrorCodeInvalid,
	}
}

// IsReftableSupported reports whether the linked libgit2 build supports the
// reftable reference storage backend. With the `libgit2_no_reftable` build tag
// this is always false.
func IsReftableSupported() bool {
	return false
}
