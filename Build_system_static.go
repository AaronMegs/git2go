//go:build static && system_libgit2
// +build static,system_libgit2

package git

/*
#cgo pkg-config: libgit2 --static
#cgo CFLAGS: -DLIBGIT2_STATIC
#include <git2.h>

#if LIBGIT2_VER_MAJOR != 1 || LIBGIT2_VER_MINOR < 9 || LIBGIT2_VER_MINOR > 9
# error "Invalid libgit2 version; this git2go currently targets the promoted-SHA256 libgit2 main baseline (version headers still report 1.9.x)"
#endif

#if !defined(GIT_OID_SHA256_SIZE) || !defined(GIT_OBJECT_ID_OPTIONS_VERSION) || !defined(GIT_INDEX_OPTIONS_VERSION) || !defined(GIT_DIFF_PARSE_OPTIONS_VERSION)
# error "This git2go requires libgit2's promoted typed object-id API (SHA256 plus object-id/index/diff options); use the pinned vendored baseline or a compatible release"
#endif
*/
import "C"
