//go:build static && !system_libgit2
// +build static,!system_libgit2

package git

/*
#cgo windows CFLAGS: -I${SRCDIR}/static-build/install/include/
#cgo windows LDFLAGS: -L${SRCDIR}/static-build/install/lib/ -lgit2 -lwinhttp -lws2_32 -lole32 -lrpcrt4 -lcrypt32
#cgo !windows pkg-config: --static libgit2
#cgo CFLAGS: -DGIT_STATIC
#include <git2.h>

// The compile-time version and ABI capability checks are centralized here.
#include "git2go_version_check.h"
*/
import "C"
