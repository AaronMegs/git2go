package git

/*
#include <git2.h>
#include <git2/sys/refdb_backend.h>

extern int _go_git_refdb_backend_alloc(git_refdb_backend **out, void *handle);
extern void *_go_git_refdb_backend_handle(git_refdb_backend *backend);
*/
import "C"
import (
	"runtime"
	"unsafe"
)

// RefdbBackendInterface is implemented by custom, Go-defined reference
// database backends. Register an implementation with
// Repository.NewRefdbBackendFromInterface and attach it via Refdb.SetBackend.
//
// libgit2 marks most of these callbacks as mandatory for a functioning
// backend. Any error returned is propagated to libgit2 as a callback error.
//
// Reference and reflog objects passed to callbacks are owned by libgit2 for
// the duration of the call; do not free them or retain them beyond the call.
type RefdbBackendInterface interface {
	// Exists reports whether a reference with the given name exists.
	Exists(refName string) (bool, error)

	// Lookup returns the reference with the given name, or an error with code
	// ErrorCodeNotFound if it does not exist.
	Lookup(refName string) (*Reference, error)

	// Iterator returns an iterator over references matching the optional glob
	// (empty glob means all references).
	Iterator(glob string) (RefdbBackendIterator, error)

	// Write persists a reference. See git_refdb_backend.write for the exact
	// semantics of force/old/oldTarget.
	Write(ref *Reference, force bool, who *Signature, message string, old *Oid, oldTarget string) error

	// Rename renames a reference and returns the renamed reference.
	Rename(oldName, newName string, force bool, who *Signature, message string) (*Reference, error)

	// Delete removes a reference (and its reflog).
	Delete(refName string, oldID *Oid, oldTarget string) error

	// HasLog reports whether the given reference has a reflog.
	HasLog(refName string) (bool, error)

	// EnsureLog ensures the given reference will have a reflog.
	EnsureLog(refName string) error

	// Free releases any resources held by the backend.
	Free()

	// ReflogRead reads the reflog for the given reference name.
	ReflogRead(name string) (*Reflog, error)

	// ReflogWrite writes a reflog.
	ReflogWrite(reflog *Reflog) error

	// ReflogRename renames a reflog.
	ReflogRename(oldName, newName string) error

	// ReflogDelete removes a reflog.
	ReflogDelete(name string) error
}

// RefdbBackendIterator iterates the references produced by a custom backend's
// Iterator callback.
type RefdbBackendIterator interface {
	// Next returns the next reference, or an error with code ErrorCodeIterOver
	// when iteration is complete.
	Next() (*Reference, error)
	// Free releases resources held by the iterator.
	Free()
}

// refdbBackendRegistry keeps Go backend implementations alive while libgit2
// holds a C pointer to them, keyed by the handle passed through cgo.
type refdbBackendState struct {
	backend  RefdbBackendInterface
	iterator RefdbBackendIterator
}

// NewRefdbBackendFromInterface wraps a Go RefdbBackendInterface implementation
// into a RefdbBackend that can be attached to a Refdb via SetBackend.
//
// The returned backend takes ownership semantics matching libgit2: once passed
// to Refdb.SetBackend, libgit2 owns it and will call Free when done.
func NewRefdbBackendFromInterface(impl RefdbBackendInterface) (*RefdbBackend, error) {
	state := &refdbBackendState{backend: impl}
	handle := pointerHandles.Track(state)

	var ptr *C.git_refdb_backend
	ret := C._go_git_refdb_backend_alloc(&ptr, handle)
	if ret < 0 {
		pointerHandles.Untrack(handle)
		return nil, MakeGitError(ret)
	}

	return &RefdbBackend{ptr: ptr}, nil
}

// backendStateFromHandle resolves the Go backend state from a cgo handle.
func backendStateFromHandle(handle unsafe.Pointer) *refdbBackendState {
	return pointerHandles.Get(handle).(*refdbBackendState)
}

//export refdbBackendExistsCallback
func refdbBackendExistsCallback(errorMessage **C.char, exists *C.int, handle unsafe.Pointer, refName *C.char) C.int {
	state := backendStateFromHandle(handle)
	ok, err := state.backend.Exists(C.GoString(refName))
	if err != nil {
		return setCallbackError(errorMessage, err)
	}
	if ok {
		*exists = 1
	} else {
		*exists = 0
	}
	return C.int(ErrorCodeOK)
}

//export refdbBackendLookupCallback
func refdbBackendLookupCallback(errorMessage **C.char, out **C.git_reference, handle unsafe.Pointer, refName *C.char) C.int {
	state := backendStateFromHandle(handle)
	ref, err := state.backend.Lookup(C.GoString(refName))
	if err != nil {
		return setCallbackError(errorMessage, err)
	}
	// Hand ownership of the reference to libgit2 and detach the Go finalizer.
	*out = ref.ptr
	runtime.SetFinalizer(ref, nil)
	return C.int(ErrorCodeOK)
}

//export refdbBackendWriteCallback
func refdbBackendWriteCallback(errorMessage **C.char, handle unsafe.Pointer, ref *C.git_reference, force C.int, who *C.git_signature, message *C.char, old *C.git_oid, oldTarget *C.char) C.int {
	state := backendStateFromHandle(handle)
	goRef := newReferenceFromC(ref, nil)
	runtime.SetFinalizer(goRef, nil) // libgit2 owns ref during the call
	err := state.backend.Write(
		goRef,
		force != 0,
		newSignatureFromC(who),
		C.GoString(message),
		newOidFromC(old),
		goStringOrEmpty(oldTarget),
	)
	if err != nil {
		return setCallbackError(errorMessage, err)
	}
	return C.int(ErrorCodeOK)
}

//export refdbBackendRenameCallback
func refdbBackendRenameCallback(errorMessage **C.char, out **C.git_reference, handle unsafe.Pointer, oldName *C.char, newName *C.char, force C.int, who *C.git_signature, message *C.char) C.int {
	state := backendStateFromHandle(handle)
	ref, err := state.backend.Rename(C.GoString(oldName), C.GoString(newName), force != 0, newSignatureFromC(who), C.GoString(message))
	if err != nil {
		return setCallbackError(errorMessage, err)
	}
	*out = ref.ptr
	runtime.SetFinalizer(ref, nil)
	return C.int(ErrorCodeOK)
}

//export refdbBackendDeleteCallback
func refdbBackendDeleteCallback(errorMessage **C.char, handle unsafe.Pointer, refName *C.char, oldID *C.git_oid, oldTarget *C.char) C.int {
	state := backendStateFromHandle(handle)
	err := state.backend.Delete(C.GoString(refName), newOidFromC(oldID), goStringOrEmpty(oldTarget))
	if err != nil {
		return setCallbackError(errorMessage, err)
	}
	return C.int(ErrorCodeOK)
}

//export refdbBackendHasLogCallback
func refdbBackendHasLogCallback(errorMessage **C.char, handle unsafe.Pointer, refName *C.char) C.int {
	state := backendStateFromHandle(handle)
	ok, err := state.backend.HasLog(C.GoString(refName))
	if err != nil {
		return setCallbackError(errorMessage, err)
	}
	if ok {
		return 1
	}
	return C.int(ErrorCodeOK)
}

//export refdbBackendEnsureLogCallback
func refdbBackendEnsureLogCallback(errorMessage **C.char, handle unsafe.Pointer, refName *C.char) C.int {
	state := backendStateFromHandle(handle)
	if err := state.backend.EnsureLog(C.GoString(refName)); err != nil {
		return setCallbackError(errorMessage, err)
	}
	return C.int(ErrorCodeOK)
}

//export refdbBackendFreeCallback
func refdbBackendFreeCallback(handle unsafe.Pointer) {
	state := backendStateFromHandle(handle)
	if state.iterator != nil {
		state.iterator.Free()
		state.iterator = nil
	}
	state.backend.Free()
	pointerHandles.Untrack(handle)
}

//export refdbBackendReflogReadCallback
func refdbBackendReflogReadCallback(errorMessage **C.char, out **C.git_reflog, handle unsafe.Pointer, name *C.char) C.int {
	state := backendStateFromHandle(handle)
	reflog, err := state.backend.ReflogRead(C.GoString(name))
	if err != nil {
		return setCallbackError(errorMessage, err)
	}
	*out = reflog.ptr
	runtime.SetFinalizer(reflog, nil)
	return C.int(ErrorCodeOK)
}

//export refdbBackendReflogWriteCallback
func refdbBackendReflogWriteCallback(errorMessage **C.char, handle unsafe.Pointer, reflog *C.git_reflog) C.int {
	state := backendStateFromHandle(handle)
	goReflog := newReflogFromC(reflog, nil)
	runtime.SetFinalizer(goReflog, nil)
	if err := state.backend.ReflogWrite(goReflog); err != nil {
		return setCallbackError(errorMessage, err)
	}
	return C.int(ErrorCodeOK)
}

//export refdbBackendReflogRenameCallback
func refdbBackendReflogRenameCallback(errorMessage **C.char, handle unsafe.Pointer, oldName *C.char, newName *C.char) C.int {
	state := backendStateFromHandle(handle)
	if err := state.backend.ReflogRename(C.GoString(oldName), C.GoString(newName)); err != nil {
		return setCallbackError(errorMessage, err)
	}
	return C.int(ErrorCodeOK)
}

//export refdbBackendReflogDeleteCallback
func refdbBackendReflogDeleteCallback(errorMessage **C.char, handle unsafe.Pointer, name *C.char) C.int {
	state := backendStateFromHandle(handle)
	if err := state.backend.ReflogDelete(C.GoString(name)); err != nil {
		return setCallbackError(errorMessage, err)
	}
	return C.int(ErrorCodeOK)
}

//export refdbBackendIteratorCallback
func refdbBackendIteratorCallback(errorMessage **C.char, handle unsafe.Pointer, glob *C.char) C.int {
	state := backendStateFromHandle(handle)
	iter, err := state.backend.Iterator(goStringOrEmpty(glob))
	if err != nil {
		return setCallbackError(errorMessage, err)
	}
	state.iterator = iter
	return C.int(ErrorCodeOK)
}

//export refdbBackendIteratorNextCallback
func refdbBackendIteratorNextCallback(errorMessage **C.char, out **C.git_reference, handle unsafe.Pointer) C.int {
	state := backendStateFromHandle(handle)
	if state.iterator == nil {
		return C.int(ErrorCodeIterOver)
	}
	ref, err := state.iterator.Next()
	if err != nil {
		if IsErrorCode(err, ErrorCodeIterOver) {
			return C.int(ErrorCodeIterOver)
		}
		return setCallbackError(errorMessage, err)
	}
	*out = ref.ptr
	runtime.SetFinalizer(ref, nil)
	return C.int(ErrorCodeOK)
}

// goStringOrEmpty converts a possibly-NULL C string to a Go string.
func goStringOrEmpty(s *C.char) string {
	if s == nil {
		return ""
	}
	return C.GoString(s)
}
