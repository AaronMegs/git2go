//go:build !static
// +build !static

package git

/*
#cgo pkg-config: libgit2
#cgo CFLAGS: -DLIBGIT2_DYNAMIC
#include <git2.h>

// The compile-time version and ABI capability checks are centralized here.
#include "git2go_version_check.h"
*/
import "C"
