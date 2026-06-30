//go:build static && !system_libgit2 && git_experimental_sha256
// +build static,!system_libgit2,git_experimental_sha256

package git

// Bundled static build wiring for the experimental SHA256 libgit2.
//
// When libgit2 is built with -DEXPERIMENTAL_SHA256=ON the install layout uses an
// "-experimental" suffix throughout: the static archive is libgit2-experimental.a,
// the headers live under include/git2-experimental/ with a git2-experimental.h
// umbrella header, and the pkg-config file is libgit2-experimental.pc. That
// umbrella header transitively defines GIT_EXPERIMENTAL_SHA256, so simply
// including it selects the experimental ABI (the manual -D injection in
// sha256_experimental.go remains a harmless no-op redefinition to the same
// value, and keeps the system/dynamic builds consistent).
//
// Build/run with:
//   EXPERIMENTAL_SHA256=ON ./script/build-libgit2.sh --static
//   go test -tags "static git_experimental_sha256" ./...
//
// TODO(sha256-merge): once upstream promotes SHA256 and the "-experimental"
// install layout goes away, fold this back into Build_bundled_static.go.
// See docs/sha256-compat-design.md section 4.6.

/*
#cgo windows CFLAGS: -I${SRCDIR}/static-build/install/include/
#cgo windows LDFLAGS: -L${SRCDIR}/static-build/install/lib/ -lgit2-experimental -lwinhttp -lws2_32 -lole32 -lrpcrt4 -lcrypt32
#cgo !windows pkg-config: --static ${SRCDIR}/static-build/install/lib/pkgconfig/libgit2-experimental.pc
#cgo CFLAGS: -DLIBGIT2_STATIC
// The build script creates git2.h -> git2-experimental.h and git2 ->
// git2-experimental compatibility symlinks in the install include dir so that
// the shared cgo files (wrapper.c, etc.) which include <git2.h> / <git2/sys/...>
// resolve against the experimental headers unchanged.
#include <git2.h>

#if LIBGIT2_VER_MAJOR != 1 || LIBGIT2_VER_MINOR < 9 || LIBGIT2_VER_MINOR > 9
# error "Invalid libgit2 version; this git2go supports libgit2 between v1.9.0 and v1.9.x"
#endif

#ifndef GIT_EXPERIMENTAL_SHA256
# error "git_experimental_sha256 build tag requires a libgit2 built with -DEXPERIMENTAL_SHA256=ON"
#endif
*/
import "C"
