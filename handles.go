package git

/*
#include <stdlib.h>
*/
import "C"
import (
	"fmt"
	"sync"
	"unsafe"
)

type HandleList struct {
	doNotCompare
	sync.RWMutex
	// stores the Go pointers
	handles map[unsafe.Pointer]interface{}
}

func NewHandleList() *HandleList {
	return &HandleList{
		handles: make(map[unsafe.Pointer]interface{}),
	}
}

// Track adds the given pointer to the list of pointers to track and
// returns a pointer value which can be passed to C as an opaque
// pointer.
func (v *HandleList) Track(pointer interface{}) unsafe.Pointer {
	handle := C.malloc(1)

	v.Lock()
	v.handles[handle] = pointer
	v.Unlock()

	return handle
}

// Untrack stops tracking the pointer given by the handle
func (v *HandleList) Untrack(handle unsafe.Pointer) {
	v.Lock()
	delete(v.handles, handle)
	C.free(handle)
	v.Unlock()
}

// Clear stops tracking all the managed pointers.
func (v *HandleList) Clear() {
	v.Lock()
	for handle := range v.handles {
		delete(v.handles, handle)
		C.free(handle)
	}
	v.Unlock()
}

// Get retrieves the pointer from the given handle. It panics when the handle is
// no longer tracked, which indicates a use-after-free in the calling binding.
func (v *HandleList) Get(handle unsafe.Pointer) interface{} {
	ptr, ok := v.GetOk(handle)
	if !ok {
		panic(fmt.Sprintf("invalid pointer handle: %p", handle))
	}

	return ptr
}

// GetOk retrieves the pointer from the given handle and reports whether the
// handle is still tracked.
//
// Callbacks invoked by libgit2 during teardown (for example a refdb backend
// `free` that libgit2 may call more than once) must tolerate an already
// untracked handle instead of panicking across the cgo boundary.
func (v *HandleList) GetOk(handle unsafe.Pointer) (interface{}, bool) {
	v.RLock()
	defer v.RUnlock()

	ptr, ok := v.handles[handle]
	return ptr, ok
}
