//go:build static && !system_libgit2
// +build static,!system_libgit2

package git

/*
#cgo windows CFLAGS: -I${SRCDIR}/static-build/install/include/
#cgo windows LDFLAGS: -L${SRCDIR}/static-build/install/lib/ -lgit2 -lwinhttp -lws2_32 -lole32 -lrpcrt4 -lcrypt32
#cgo !windows pkg-config: --static ${SRCDIR}/static-build/install/lib/pkgconfig/libgit2.pc
#cgo CFLAGS: -DGIT_STATIC -DLIBGIT2_STATIC
#include <git2.h>

#include "git2go_version_check.h"
*/
import "C"
