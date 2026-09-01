//go:build !static
// +build !static

package git

/*
#cgo pkg-config: libgit2
#cgo CFLAGS: -DLIBGIT2_DYNAMIC
#include <git2.h>

// Use LIBGIT2_VERSION_*, not the LIBGIT2_VER_* aliases: upstream keeps the
// latter in deprecated.h behind GIT_DEPRECATE_HARD, so a hard-deprecated build
// leaves them undefined and the preprocessor would silently compare against 0.
#if !defined(LIBGIT2_VERSION_MAJOR) || !defined(LIBGIT2_VERSION_MINOR)
# error "Cannot determine the libgit2 version; LIBGIT2_VERSION_MAJOR/LIBGIT2_VERSION_MINOR are not defined by the libgit2 headers being used"
#endif

#if LIBGIT2_VERSION_MAJOR != 1 || LIBGIT2_VERSION_MINOR != 9
# error "Invalid libgit2 version; this git2go currently targets the promoted-SHA256 libgit2 main baseline (version headers still report 1.9.x)"
#endif

#if !defined(GIT_OID_SHA256_SIZE) || !defined(GIT_OBJECT_ID_OPTIONS_VERSION) || !defined(GIT_INDEX_OPTIONS_VERSION) || !defined(GIT_DIFF_PARSE_OPTIONS_VERSION)
# error "This git2go requires libgit2's promoted typed object-id API (SHA256 plus object-id/index/diff options); use the pinned vendored baseline or a compatible release"
#endif
*/
import "C"
