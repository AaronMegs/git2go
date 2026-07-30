//go:build libgit2_next
// +build libgit2_next

package git

// This file is only compiled when the `libgit2_next` build tag is set. It opts
// into the libgit2 `main`-branch experimental object-id API shape.
//
// Background: between the pinned release (libgit2 1.9.4, which the vendored
// submodule tracks) and the current libgit2 `main` branch, the experimental
// SHA256 API was refactored from an "overload the legacy function" shape to a
// "new, separately-named *_ext / git_oid_from_* function" shape, while the
// legacy names were frozen to SHA1-only. Crucially, libgit2 main's version.h
// still reports 1.9.0 (LOWER than the 1.9.4 release), so the two shapes cannot
// be distinguished by LIBGIT2_VERSION_NUMBER at the C preprocessor level; an
// explicit opt-in is required.
//
// Define GIT2GO_LIBGIT2_OID_EXT_API so the #if branches in wrapper.c select the
// main-branch entry points (git_oid_from_prefix/from_raw, git_odb_new_ext,
// git_index_new_ext/open_ext, git_diff_from_buffer_ext, ...). This MUST be
// combined with the `git_experimental_sha256` tag (and a libgit2 built with
// -DEXPERIMENTAL_SHA256=ON), e.g.:
//
//   go build -tags "static git_experimental_sha256 libgit2_next" ./...
//
// NOTE: this path is designed from libgit2 main header analysis and has not been
// compiled against an actual main build in this repository (the vendored
// submodule is 1.9.4). Verify when bumping the submodule to a main-based libgit2.
//
// TODO(sha256-merge): once main promotes SHA256 to stable, fold this into the
// default path and drop the tag. See docs/sha256-compat-design.md section 4.6.

/*
#cgo CFLAGS: -DGIT2GO_LIBGIT2_OID_EXT_API=1
*/
import "C"
