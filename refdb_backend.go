package git

/*
#include <git2.h>
#include <git2/sys/refdb_backend.h>

extern int _go_git_refdb_backend_alloc(git_refdb_backend **out, void *handle, uint32_t capabilities);
extern uint32_t _go_git_refdb_backend_capabilities(git_refdb_backend *backend);
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
	// Free releases resources held by the iterator. The bridge invokes it
	// exactly once when libgit2 frees that iterator instance.
	Free()
}

// RefdbBackendInitFlag controls initialization of a custom refdb backend.
// Values mirror git_refdb_backend_init_flag_t on libgit2 main. The type remains
// available in stable-compatible builds so callers can implement one backend
// API across both build tracks; the init callback itself is installed only by
// a libgit2_reftable/latest-main build.
type RefdbBackendInitFlag uint32

const (
	RefdbBackendInitIsWorktree RefdbBackendInitFlag = 1 << iota
	RefdbBackendInitForceHead
)

// RefdbBackendInitializer is an optional capability implemented by backends
// that can initialize storage for a newly-created repository. initialHead is
// nil when libgit2 supplied no HEAD target.
type RefdbBackendInitializer interface {
	Init(initialHead *string, mode RepositoryInitMode, flags RefdbBackendInitFlag) error
}

// RefdbBackendCompressor is an optional capability implemented by backends
// that can compact or otherwise optimize their reference storage.
type RefdbBackendCompressor interface {
	Compress() error
}

// RefdbBackendLock is an opaque lock payload owned by a custom backend. The
// bridge retains it between Lock and Unlock without interpreting it.
type RefdbBackendLock interface{}

// RefdbBackendUnlockStatus describes what Unlock should do with a lock.
type RefdbBackendUnlockStatus int

const (
	RefdbBackendUnlockCancel RefdbBackendUnlockStatus = iota
	RefdbBackendUnlockUpdate
	RefdbBackendUnlockDelete
)

// RefdbBackendLocker is an optional transactional locking capability. A
// backend either implements both methods through this interface or neither.
// message is nil when libgit2 supplied no reflog message.
type RefdbBackendLocker interface {
	Lock(refName string) (RefdbBackendLock, error)
	Unlock(lock RefdbBackendLock, status RefdbBackendUnlockStatus, updateReflog bool, ref *Reference, sig *Signature, message *string) error
}

const (
	refdbBackendCapabilityInit uint32 = 1 << iota
	refdbBackendCapabilityCompress
	refdbBackendCapabilityLock
)

// refdbBackendState keeps one Go backend alive while libgit2 owns its C
// wrapper. Iterators and transaction locks have independent handles so they
// can coexist and be freed independently.
type refdbBackendState struct {
	backend RefdbBackendInterface
}

type refdbBackendIteratorState struct {
	iterator RefdbBackendIterator
}

type refdbBackendLockState struct {
	lock RefdbBackendLock
}

// NewRefdbBackendFromInterface wraps a Go RefdbBackendInterface implementation
// into a RefdbBackend that can be attached to a Refdb via SetBackend.
// Optional callback pointers are installed only when impl also satisfies the
// corresponding capability interface.
//
// The returned backend takes ownership semantics matching libgit2: once passed
// to Refdb.SetBackend, libgit2 owns it and will call Free when done.
func NewRefdbBackendFromInterface(impl RefdbBackendInterface) (*RefdbBackend, error) {
	if impl == nil {
		return nil, &GitError{Message: "refdb backend implementation is nil", Class: ErrorClassInvalid, Code: ErrorCodeInvalid}
	}

	var capabilities uint32
	if _, ok := impl.(RefdbBackendInitializer); ok {
		if !reftableSupported {
			return nil, &GitError{
				Message: "refdb backend Init requires a latest-main libgit2 build with the libgit2_reftable tag",
				Class:   ErrorClassInvalid,
				Code:    ErrorCodeInvalid,
			}
		}
		capabilities |= refdbBackendCapabilityInit
	}
	if _, ok := impl.(RefdbBackendCompressor); ok {
		capabilities |= refdbBackendCapabilityCompress
	}
	if _, ok := impl.(RefdbBackendLocker); ok {
		capabilities |= refdbBackendCapabilityLock
	}

	state := &refdbBackendState{backend: impl}
	handle := pointerHandles.Track(state)

	var ptr *C.git_refdb_backend
	ret := C._go_git_refdb_backend_alloc(&ptr, handle, C.uint32_t(capabilities))
	if ret < 0 {
		pointerHandles.Untrack(handle)
		return nil, MakeGitError(ret)
	}

	return &RefdbBackend{ptr: ptr}, nil
}

func refdbBackendCapabilities(backend *RefdbBackend) uint32 {
	if backend == nil || backend.ptr == nil {
		return 0
	}
	capabilities := uint32(C._go_git_refdb_backend_capabilities(backend.ptr))
	runtime.KeepAlive(backend)
	return capabilities
}

func backendStateFromHandle(handle unsafe.Pointer) *refdbBackendState {
	return pointerHandles.Get(handle).(*refdbBackendState)
}

func backendIteratorStateFromHandle(handle unsafe.Pointer) *refdbBackendIteratorState {
	return pointerHandles.Get(handle).(*refdbBackendIteratorState)
}

func backendLockStateFromHandle(handle unsafe.Pointer) *refdbBackendLockState {
	return pointerHandles.Get(handle).(*refdbBackendLockState)
}

//export refdbBackendInitCallback
func refdbBackendInitCallback(errorMessage **C.char, handle unsafe.Pointer, initialHead *C.char, mode C.uint32_t, flags C.uint32_t) C.int {
	initializer, ok := backendStateFromHandle(handle).backend.(RefdbBackendInitializer)
	if !ok {
		return setCallbackError(errorMessage, &GitError{Message: "refdb backend does not implement Init", Class: ErrorClassInvalid, Code: ErrorCodeInvalid})
	}
	if err := initializer.Init(goStringOrNil(initialHead), RepositoryInitMode(mode), RefdbBackendInitFlag(flags)); err != nil {
		return setCallbackError(errorMessage, err)
	}
	return C.int(ErrorCodeOK)
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
	if ref == nil || ref.ptr == nil {
		return setCallbackError(errorMessage, &GitError{Message: "refdb backend Lookup returned a nil reference", Class: ErrorClassInvalid, Code: ErrorCodeInvalid})
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
		goStringOrEmpty(message),
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
	ref, err := state.backend.Rename(C.GoString(oldName), C.GoString(newName), force != 0, newSignatureFromC(who), goStringOrEmpty(message))
	if err != nil {
		return setCallbackError(errorMessage, err)
	}
	if ref == nil || ref.ptr == nil {
		return setCallbackError(errorMessage, &GitError{Message: "refdb backend Rename returned a nil reference", Class: ErrorClassInvalid, Code: ErrorCodeInvalid})
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

//export refdbBackendCompressCallback
func refdbBackendCompressCallback(errorMessage **C.char, handle unsafe.Pointer) C.int {
	compressor, ok := backendStateFromHandle(handle).backend.(RefdbBackendCompressor)
	if !ok {
		return setCallbackError(errorMessage, &GitError{Message: "refdb backend does not implement Compress", Class: ErrorClassInvalid, Code: ErrorCodeInvalid})
	}
	if err := compressor.Compress(); err != nil {
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
	if reflog == nil || reflog.ptr == nil {
		return setCallbackError(errorMessage, &GitError{Message: "refdb backend ReflogRead returned a nil reflog", Class: ErrorClassInvalid, Code: ErrorCodeInvalid})
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
func refdbBackendIteratorCallback(errorMessage **C.char, iteratorHandle *unsafe.Pointer, backendHandle unsafe.Pointer, glob *C.char) C.int {
	iter, err := backendStateFromHandle(backendHandle).backend.Iterator(goStringOrEmpty(glob))
	if err != nil {
		return setCallbackError(errorMessage, err)
	}
	if iter == nil {
		return setCallbackError(errorMessage, &GitError{Message: "refdb backend returned a nil iterator", Class: ErrorClassInvalid, Code: ErrorCodeInvalid})
	}
	*iteratorHandle = pointerHandles.Track(&refdbBackendIteratorState{iterator: iter})
	return C.int(ErrorCodeOK)
}

//export refdbBackendIteratorNextCallback
func refdbBackendIteratorNextCallback(errorMessage **C.char, out **C.git_reference, iteratorHandle unsafe.Pointer) C.int {
	ref, err := backendIteratorStateFromHandle(iteratorHandle).iterator.Next()
	if err != nil {
		if IsErrorCode(err, ErrorCodeIterOver) {
			return C.int(ErrorCodeIterOver)
		}
		return setCallbackError(errorMessage, err)
	}
	if ref == nil || ref.ptr == nil {
		return setCallbackError(errorMessage, &GitError{Message: "refdb iterator returned a nil reference", Class: ErrorClassInvalid, Code: ErrorCodeInvalid})
	}
	*out = ref.ptr
	runtime.SetFinalizer(ref, nil)
	return C.int(ErrorCodeOK)
}

//export refdbBackendIteratorFreeCallback
func refdbBackendIteratorFreeCallback(iteratorHandle unsafe.Pointer) {
	state := backendIteratorStateFromHandle(iteratorHandle)
	state.iterator.Free()
	pointerHandles.Untrack(iteratorHandle)
}

//export refdbBackendLockCallback
func refdbBackendLockCallback(errorMessage **C.char, lockHandle *unsafe.Pointer, backendHandle unsafe.Pointer, refName *C.char) C.int {
	locker, ok := backendStateFromHandle(backendHandle).backend.(RefdbBackendLocker)
	if !ok {
		return setCallbackError(errorMessage, &GitError{Message: "refdb backend does not implement Lock", Class: ErrorClassInvalid, Code: ErrorCodeInvalid})
	}
	lock, err := locker.Lock(C.GoString(refName))
	if err != nil {
		return setCallbackError(errorMessage, err)
	}
	*lockHandle = pointerHandles.Track(&refdbBackendLockState{lock: lock})
	return C.int(ErrorCodeOK)
}

//export refdbBackendUnlockCallback
func refdbBackendUnlockCallback(errorMessage **C.char, backendHandle unsafe.Pointer, lockHandle unsafe.Pointer, status C.int, updateReflog C.int, ref *C.git_reference, sig *C.git_signature, message *C.char) C.int {
	locker, ok := backendStateFromHandle(backendHandle).backend.(RefdbBackendLocker)
	if !ok {
		return setCallbackError(errorMessage, &GitError{Message: "refdb backend does not implement Unlock", Class: ErrorClassInvalid, Code: ErrorCodeInvalid})
	}

	lockState := backendLockStateFromHandle(lockHandle)
	defer pointerHandles.Untrack(lockHandle)
	if status < C.int(RefdbBackendUnlockCancel) || status > C.int(RefdbBackendUnlockDelete) {
		return setCallbackError(errorMessage, &GitError{Message: "invalid refdb backend unlock status", Class: ErrorClassInvalid, Code: ErrorCodeInvalid})
	}

	var goRef *Reference
	if ref != nil {
		goRef = newReferenceFromC(ref, nil)
		runtime.SetFinalizer(goRef, nil)
	}
	if err := locker.Unlock(
		lockState.lock,
		RefdbBackendUnlockStatus(status),
		updateReflog != 0,
		goRef,
		newSignatureFromC(sig),
		goStringOrNil(message),
	); err != nil {
		return setCallbackError(errorMessage, err)
	}
	return C.int(ErrorCodeOK)
}

// goStringOrEmpty converts a possibly-NULL C string to a Go string.
func goStringOrEmpty(s *C.char) string {
	if s == nil {
		return ""
	}
	return C.GoString(s)
}

// goStringOrNil preserves the distinction between a NULL C string and an
// explicitly supplied empty string.
func goStringOrNil(s *C.char) *string {
	if s == nil {
		return nil
	}
	value := C.GoString(s)
	return &value
}
