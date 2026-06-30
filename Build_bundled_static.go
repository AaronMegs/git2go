//go:build static && !system_libgit2 && !git_experimental_sha256
// +build static,!system_libgit2,!git_experimental_sha256

package git

/*
#cgo windows CFLAGS: -I${SRCDIR}/static-build/install/include/
#cgo windows LDFLAGS: -L${SRCDIR}/static-build/install/lib/ -lgit2 -lwinhttp -lws2_32 -lole32 -lrpcrt4 -lcrypt32
#cgo !windows pkg-config: --static ${SRCDIR}/static-build/install/lib/pkgconfig/libgit2.pc
#cgo CFLAGS: -DLIBGIT2_STATIC
#include <git2.h>

#if LIBGIT2_VER_MAJOR != 1 || LIBGIT2_VER_MINOR < 9 || LIBGIT2_VER_MINOR > 9
# error "Invalid libgit2 version; this git2go supports libgit2 between v1.9.0 and v1.9.x"
#endif

// TODO(sha256-merge): when bumping this guard to the libgit2 version that
// promotes SHA256 out of the experimental gate, revisit the dual SHA1/SHA256
// build paths and collapse them per docs/sha256-compat-design.md section 4.6.
*/
import "C"
