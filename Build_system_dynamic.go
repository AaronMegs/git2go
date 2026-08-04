//go:build !static
// +build !static

package git

/*
#cgo pkg-config: libgit2
#cgo CFLAGS: -DLIBGIT2_DYNAMIC
#include <git2.h>

#if LIBGIT2_VER_MAJOR != 1 || LIBGIT2_VER_MINOR < 9 || LIBGIT2_VER_MINOR > 9
# error "Invalid libgit2 version; this git2go currently targets the promoted-SHA256 libgit2 main baseline (version headers still report 1.9.x)"
#endif

#ifndef GIT_OID_SHA256_SIZE
# error "This git2go requires a libgit2 with promoted SHA256 support (git_oid is typed); use the vendored main baseline or a compatible release"
#endif
*/
import "C"
