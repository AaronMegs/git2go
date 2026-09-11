//go:build static && system_libgit2
// +build static,system_libgit2

package git

/*
#cgo pkg-config: libgit2 --static
#cgo CFLAGS: -DLIBGIT2_STATIC
#include <git2.h>

// The compile-time version and ABI capability checks are centralized here.
#include "git2go_version_check.h"
*/
import "C"
