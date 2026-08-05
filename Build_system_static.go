//go:build static && system_libgit2
// +build static,system_libgit2

package git

/*
#cgo pkg-config: libgit2 --static
#cgo CFLAGS: -DGIT_STATIC -DLIBGIT2_STATIC
#include <git2.h>

#include "git2go_version_check.h"
*/
import "C"
