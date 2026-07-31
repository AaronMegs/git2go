package git

/*
#include <git2.h>
*/
import "C"
import "runtime"

// Reflog is a minimal binding of libgit2's git_reflog, sufficient to pass
// reflog handles through custom refdb backend callbacks (see
// RefdbBackendInterface). A fuller reflog API (reading/appending entries) can
// be layered on top of this type later.
type Reflog struct {
	doNotCompare
	ptr  *C.git_reflog
	repo *Repository
}

func newReflogFromC(ptr *C.git_reflog, repo *Repository) *Reflog {
	if ptr == nil {
		return nil
	}
	reflog := &Reflog{ptr: ptr, repo: repo}
	runtime.SetFinalizer(reflog, (*Reflog).Free)
	return reflog
}

// Free releases the memory held by the reflog.
func (r *Reflog) Free() {
	runtime.SetFinalizer(r, nil)
	C.git_reflog_free(r.ptr)
}
