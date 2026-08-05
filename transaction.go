package git

/*
#include <git2.h>
#include <git2/transaction.h>
*/
import "C"
import (
	"runtime"
	"strings"
	"unsafe"
)

// Transaction represents a group of reference updates. References must be
// locked before they are updated. Free rolls back and unlocks any locks that
// were not committed.
type Transaction struct {
	doNotCompare
	ptr  *C.git_transaction
	repo *Repository
}

// NewTransaction creates an empty reference transaction for the repository.
func (v *Repository) NewTransaction() (*Transaction, error) {
	var ptr *C.git_transaction

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	ret := C.git_transaction_new(&ptr, v.ptr)
	runtime.KeepAlive(v)
	if ret < 0 {
		return nil, MakeGitError(ret)
	}

	tx := &Transaction{ptr: ptr, repo: v}
	runtime.SetFinalizer(tx, (*Transaction).Free)
	return tx, nil
}

func (tx *Transaction) requireOpen() error {
	if tx == nil || tx.ptr == nil {
		return &GitError{Message: "transaction is closed", Class: ErrorClassInvalid, Code: ErrorCodeInvalid}
	}
	return nil
}

func validateTransactionString(name, value string) error {
	if strings.IndexByte(value, 0) >= 0 {
		return &GitError{Message: name + " contains a NUL byte", Class: ErrorClassInvalid, Code: ErrorCodeInvalid}
	}
	return nil
}

// LockRef locks refName for a subsequent update in this transaction.
func (tx *Transaction) LockRef(refName string) error {
	if err := tx.requireOpen(); err != nil {
		return err
	}
	if err := validateTransactionString("reference name", refName); err != nil {
		return err
	}
	cRefName := C.CString(refName)
	defer C.free(unsafe.Pointer(cRefName))

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	ret := C.git_transaction_lock_ref(tx.ptr, cRefName)
	runtime.KeepAlive(tx)
	if ret < 0 {
		return MakeGitError(ret)
	}
	return nil
}

// SetTarget queues a direct-reference update. refName must already be locked.
func (tx *Transaction) SetTarget(refName string, target *Oid, sig *Signature, message string) error {
	if err := tx.requireOpen(); err != nil {
		return err
	}
	if target == nil {
		return &GitError{Message: "transaction target is nil", Class: ErrorClassInvalid, Code: ErrorCodeInvalid}
	}
	if err := validateTransactionString("reference name", refName); err != nil {
		return err
	}
	if err := validateTransactionString("reflog message", message); err != nil {
		return err
	}
	cRefName := C.CString(refName)
	defer C.free(unsafe.Pointer(cRefName))
	cMessage := C.CString(message)
	defer C.free(unsafe.Pointer(cMessage))
	cSig, err := sig.toC()
	if err != nil {
		return err
	}
	if cSig != nil {
		defer C.git_signature_free(cSig)
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	ret := C.git_transaction_set_target(tx.ptr, cRefName, target.toC(), cSig, cMessage)
	runtime.KeepAlive(tx)
	runtime.KeepAlive(target)
	if ret < 0 {
		return MakeGitError(ret)
	}
	return nil
}

// SetSymbolicTarget queues a symbolic-reference update. refName must already
// be locked.
func (tx *Transaction) SetSymbolicTarget(refName, target string, sig *Signature, message string) error {
	if err := tx.requireOpen(); err != nil {
		return err
	}
	for name, value := range map[string]string{"reference name": refName, "symbolic target": target, "reflog message": message} {
		if err := validateTransactionString(name, value); err != nil {
			return err
		}
	}
	cRefName := C.CString(refName)
	defer C.free(unsafe.Pointer(cRefName))
	cTarget := C.CString(target)
	defer C.free(unsafe.Pointer(cTarget))
	cMessage := C.CString(message)
	defer C.free(unsafe.Pointer(cMessage))
	cSig, err := sig.toC()
	if err != nil {
		return err
	}
	if cSig != nil {
		defer C.git_signature_free(cSig)
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	ret := C.git_transaction_set_symbolic_target(tx.ptr, cRefName, cTarget, cSig, cMessage)
	runtime.KeepAlive(tx)
	if ret < 0 {
		return MakeGitError(ret)
	}
	return nil
}

// SetReflog queues a complete reflog replacement. refName must already be
// locked.
func (tx *Transaction) SetReflog(refName string, reflog *Reflog) error {
	if err := tx.requireOpen(); err != nil {
		return err
	}
	if err := validateTransactionString("reference name", refName); err != nil {
		return err
	}
	if reflog == nil || reflog.ptr == nil {
		return &GitError{Message: "transaction reflog is nil or already freed", Class: ErrorClassInvalid, Code: ErrorCodeInvalid}
	}
	cRefName := C.CString(refName)
	defer C.free(unsafe.Pointer(cRefName))

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	ret := C.git_transaction_set_reflog(tx.ptr, cRefName, reflog.ptr)
	runtime.KeepAlive(tx)
	runtime.KeepAlive(reflog)
	if ret < 0 {
		return MakeGitError(ret)
	}
	return nil
}

// Remove queues deletion of a locked reference.
func (tx *Transaction) Remove(refName string) error {
	if err := tx.requireOpen(); err != nil {
		return err
	}
	if err := validateTransactionString("reference name", refName); err != nil {
		return err
	}
	cRefName := C.CString(refName)
	defer C.free(unsafe.Pointer(cRefName))

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	ret := C.git_transaction_remove(tx.ptr, cRefName)
	runtime.KeepAlive(tx)
	if ret < 0 {
		return MakeGitError(ret)
	}
	return nil
}

// Commit applies all queued updates and releases their locks.
func (tx *Transaction) Commit() error {
	if err := tx.requireOpen(); err != nil {
		return err
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	ptr := tx.ptr
	ret := C.git_transaction_commit(ptr)
	runtime.KeepAlive(tx)
	if ret < 0 {
		return MakeGitError(ret)
	}
	// A committed transaction must never be reused: upstream retains entries
	// whose lock payloads have already been released.
	tx.ptr = nil
	runtime.SetFinalizer(tx, nil)
	C.git_transaction_free(ptr)
	runtime.KeepAlive(tx.repo)
	return nil
}

// Free releases the transaction. Any locks that were not committed are
// released with RefdbBackendUnlockCancel.
func (tx *Transaction) Free() {
	if tx == nil || tx.ptr == nil {
		return
	}
	ptr := tx.ptr
	tx.ptr = nil
	runtime.SetFinalizer(tx, nil)
	C.git_transaction_free(ptr)
	runtime.KeepAlive(tx.repo)
}
