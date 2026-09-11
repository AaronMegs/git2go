package git

/*
#include <git2.h>
*/
import "C"
import (
	"runtime"
	"unsafe"
)

// Reflog is a binding of libgit2's git_reflog. Obtain one with
// Repository.ReadReflog (or through custom refdb backend callbacks) and
// release it with Free when done.
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
	if r == nil || r.ptr == nil {
		return
	}
	ptr := r.ptr
	r.ptr = nil
	runtime.SetFinalizer(r, nil)
	C.git_reflog_free(ptr)
}

// ReflogEntry is a single entry within a Reflog.
//
// libgit2 returns reflog entries as pointers owned by the parent git_reflog, so
// the values are copied out at construction time. Holding the C pointer instead
// would leave it dangling once the parent Reflog is freed, and nothing in the Go
// type system would prevent a caller from doing exactly that. This mirrors the
// advice libgit2 gives for git_index_entry ("make your own permanent copy").
type ReflogEntry struct {
	doNotCompare
	idOld     *Oid
	idNew     *Oid
	committer *Signature
	message   string
}

func newReflogEntryFromC(ptr *C.git_reflog_entry, reflog *Reflog) *ReflogEntry {
	if ptr == nil {
		return nil
	}

	entry := &ReflogEntry{
		idOld:     newOidFromC(C.git_reflog_entry_id_old(ptr)),
		idNew:     newOidFromC(C.git_reflog_entry_id_new(ptr)),
		committer: newSignatureFromC(C.git_reflog_entry_committer(ptr)),
	}
	if cmsg := C.git_reflog_entry_message(ptr); cmsg != nil {
		entry.message = C.GoString(cmsg)
	}
	// The parent must outlive the reads above, not the returned entry.
	runtime.KeepAlive(reflog)
	return entry
}

// ReadReflog reads the reflog for the reference with the given name.
//
// Wraps `git_reflog_read`.
func (v *Repository) ReadReflog(name string) (*Reflog, error) {
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	var ptr *C.git_reflog
	ret := C.git_reflog_read(&ptr, v.ptr, cname)
	runtime.KeepAlive(v)
	if ret < 0 {
		return nil, MakeGitError(ret)
	}

	return newReflogFromC(ptr, v), nil
}

// RenameReflog renames the reflog for oldName to name.
//
// Wraps `git_reflog_rename`.
func (v *Repository) RenameReflog(oldName, name string) error {
	cOldName := C.CString(oldName)
	defer C.free(unsafe.Pointer(cOldName))
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	ret := C.git_reflog_rename(v.ptr, cOldName, cName)
	runtime.KeepAlive(v)
	if ret < 0 {
		return MakeGitError(ret)
	}
	return nil
}

// DeleteReflog deletes the reflog for the reference with the given name.
//
// Wraps `git_reflog_delete`.
func (v *Repository) DeleteReflog(name string) error {
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	ret := C.git_reflog_delete(v.ptr, cname)
	runtime.KeepAlive(v)
	if ret < 0 {
		return MakeGitError(ret)
	}
	return nil
}

// Write writes the reflog back to disk, persisting any appended or dropped
// entries.
//
// Wraps `git_reflog_write`.
func (r *Reflog) Write() error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	ret := C.git_reflog_write(r.ptr)
	runtime.KeepAlive(r)
	if ret < 0 {
		return MakeGitError(ret)
	}
	return nil
}

// Append adds a new entry to the in-memory reflog. Call Write to persist it.
// The msg argument may be empty.
//
// Wraps `git_reflog_append`.
func (r *Reflog) Append(id *Oid, committer *Signature, msg string) error {
	cCommitter, err := committer.toC()
	if err != nil {
		return err
	}
	if cCommitter != nil {
		defer C.git_signature_free(cCommitter)
	}

	var cmsg *C.char
	if msg != "" {
		cmsg = C.CString(msg)
		defer C.free(unsafe.Pointer(cmsg))
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	ret := C.git_reflog_append(r.ptr, id.toC(), cCommitter, cmsg)
	runtime.KeepAlive(r)
	runtime.KeepAlive(id)
	if ret < 0 {
		return MakeGitError(ret)
	}
	return nil
}

// EntryCount returns the number of entries in the reflog.
//
// Wraps `git_reflog_entrycount`.
func (r *Reflog) EntryCount() int {
	count := C.git_reflog_entrycount(r.ptr)
	runtime.KeepAlive(r)
	return int(count)
}

// EntryByIndex returns the entry at the given index. Index 0 is the most
// recent entry. Returns nil if the index is out of range.
//
// Wraps `git_reflog_entry_byindex`.
func (r *Reflog) EntryByIndex(index int) *ReflogEntry {
	ptr := C.git_reflog_entry_byindex(r.ptr, C.size_t(index))
	runtime.KeepAlive(r)
	return newReflogEntryFromC(ptr, r)
}

// Drop removes an entry from the in-memory reflog. If rewritePreviousEntry is
// true, the surrounding entries are rewritten to keep the history gap-free.
// Call Write to persist the change.
//
// Wraps `git_reflog_drop`.
func (r *Reflog) Drop(index int, rewritePreviousEntry bool) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	ret := C.git_reflog_drop(r.ptr, C.size_t(index), cbool(rewritePreviousEntry))
	runtime.KeepAlive(r)
	if ret < 0 {
		return MakeGitError(ret)
	}
	return nil
}

// IdOld returns the OID the reference pointed at before this entry.
//
// Wraps `git_reflog_entry_id_old`.
func (e *ReflogEntry) IdOld() *Oid {
	return e.idOld
}

// IdNew returns the OID the reference points at after this entry.
//
// Wraps `git_reflog_entry_id_new`.
func (e *ReflogEntry) IdNew() *Oid {
	return e.idNew
}

// Committer returns the signature of the committer for this entry.
//
// Wraps `git_reflog_entry_committer`.
func (e *ReflogEntry) Committer() *Signature {
	return e.committer
}

// Message returns the log message for this entry (may be empty).
//
// Wraps `git_reflog_entry_message`.
func (e *ReflogEntry) Message() string {
	return e.message
}
