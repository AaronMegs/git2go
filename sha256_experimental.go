//go:build git_experimental_sha256
// +build git_experimental_sha256

package git

// This file is only compiled when the `git_experimental_sha256` build tag is
// set. It must be paired with a libgit2 that was built with
// `-DEXPERIMENTAL_SHA256=ON` (which defines the `GIT_EXPERIMENTAL_SHA256`
// macro). Under this configuration the `git_oid` C struct gains a leading
// `type` byte and its `id` array grows from 20 to 32 bytes, and a number of
// oid-related functions take an additional `git_oid_t` parameter.
//
// TODO(sha256-merge): when upstream promotes SHA256 out of the experimental
// gate (the GIT_EXPERIMENTAL_SHA256 macro is removed / git_oid is unified),
// this whole file and the `git_experimental_sha256` build tag can be deleted.
// See docs/sha256-compat-design.md section 4.6 for the full convergence list.
//
// We inject the `GIT_EXPERIMENTAL_SHA256` define here (as a no-op when the
// installed headers already define it to the same value) so that the
// `#ifdef GIT_EXPERIMENTAL_SHA256` branches in wrapper.c and the cgo
// preambles select the experimental ABI consistently with the Go-side `Oid`
// representation in oid_sha256.go.

/*
#cgo CFLAGS: -DGIT_EXPERIMENTAL_SHA256=1
*/
import "C"
