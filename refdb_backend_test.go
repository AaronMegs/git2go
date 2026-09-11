package git

import (
	"os"
	"sync"
	"testing"
)

// countingRefdbBackend is a minimal RefdbBackendInterface implementation used
// to verify that the C<->Go refdb backend bridge dispatches calls into Go.
// It is intentionally not a fully functional backend; it only records that its
// callbacks were invoked.
type countingRefdbBackend struct {
	existsCalls int
	lookupCalls int
	freeCalls   int
	lastRefName string
}

func (b *countingRefdbBackend) Exists(refName string) (bool, error) {
	b.existsCalls++
	b.lastRefName = refName
	return false, nil
}

// refdbBackendTestError builds an error that the C bridge maps back to the
// given libgit2 error code.
//
// Go-defined backend callbacks are the *producers* of these errors, so they
// must not consult libgit2's thread-local last error (as MakeGitError does);
// they construct the GitError directly instead. setCallbackError reads
// GitError.Code, so the bridge reports the intended code either way.
func refdbBackendTestError(code ErrorCode) error {
	return &GitError{Message: code.String(), Class: ErrorClassReference, Code: code}
}

func (b *countingRefdbBackend) Lookup(refName string) (*Reference, error) {
	b.lookupCalls++
	b.lastRefName = refName
	return nil, refdbBackendTestError(ErrorCodeNotFound)
}

func (b *countingRefdbBackend) Iterator(glob string) (RefdbBackendIterator, error) {
	return nil, refdbBackendTestError(ErrorCodeNotFound)
}

func (b *countingRefdbBackend) Write(ref *Reference, force bool, who *Signature, message string, old *Oid, oldTarget string) error {
	return nil
}

func (b *countingRefdbBackend) Rename(oldName, newName string, force bool, who *Signature, message string) (*Reference, error) {
	return nil, refdbBackendTestError(ErrorCodeNotFound)
}

func (b *countingRefdbBackend) Delete(refName string, oldID *Oid, oldTarget string) error {
	return nil
}

func (b *countingRefdbBackend) HasLog(refName string) (bool, error) { return false, nil }
func (b *countingRefdbBackend) EnsureLog(refName string) error      { return nil }
func (b *countingRefdbBackend) Free()                               { b.freeCalls++ }

func (b *countingRefdbBackend) ReflogRead(name string) (*Reflog, error) {
	return nil, refdbBackendTestError(ErrorCodeNotFound)
}
func (b *countingRefdbBackend) ReflogWrite(reflog *Reflog) error           { return nil }
func (b *countingRefdbBackend) ReflogRename(oldName, newName string) error { return nil }
func (b *countingRefdbBackend) ReflogDelete(name string) error             { return nil }

type iteratorRefdbBackend struct {
	*countingRefdbBackend
	mu      sync.Mutex
	created int
	freed   int
}

type emptyRefdbBackendIterator struct {
	owner *iteratorRefdbBackend
}

func (b *iteratorRefdbBackend) Iterator(glob string) (RefdbBackendIterator, error) {
	b.mu.Lock()
	b.created++
	b.mu.Unlock()
	return &emptyRefdbBackendIterator{owner: b}, nil
}

func (i *emptyRefdbBackendIterator) Next() (*Reference, error) {
	return nil, refdbBackendTestError(ErrorCodeIterOver)
}

func (i *emptyRefdbBackendIterator) Free() {
	i.owner.mu.Lock()
	i.owner.freed++
	i.owner.mu.Unlock()
}

func (b *iteratorRefdbBackend) counts() (created, freed int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.created, b.freed
}

type compressingRefdbBackend struct {
	*countingRefdbBackend
	compressCalls int
}

func (b *compressingRefdbBackend) Compress() error {
	b.compressCalls++
	return nil
}

type panickingRefdbBackend struct {
	*countingRefdbBackend
}

func (b *panickingRefdbBackend) Lookup(refName string) (*Reference, error) {
	panic("backend panic should be recovered")
}

type transactionRefdbBackend struct {
	*countingRefdbBackend
	lockCalls       int
	unlockCalls     int
	lastStatus      RefdbBackendUnlockStatus
	lastUpdateLog   bool
	lastTarget      *Oid
	lastSymbolic    string
	lastMessage     *string
	lastLockPayload RefdbBackendLock
}

func (b *transactionRefdbBackend) Lock(refName string) (RefdbBackendLock, error) {
	b.lockCalls++
	payload := "lock:" + refName
	b.lastLockPayload = payload
	return payload, nil
}

func (b *transactionRefdbBackend) Unlock(lock RefdbBackendLock, status RefdbBackendUnlockStatus, updateReflog bool, ref *Reference, sig *Signature, message *string) error {
	b.unlockCalls++
	b.lastLockPayload = lock
	b.lastStatus = status
	b.lastUpdateLog = updateReflog
	if ref != nil {
		b.lastTarget = ref.Target()
		b.lastSymbolic = ref.SymbolicTarget()
	}
	b.lastMessage = message
	return nil
}

func installTestRefdbBackend(t *testing.T, repo *Repository, impl RefdbBackendInterface) *Refdb {
	t.Helper()
	backend, err := NewRefdbBackendFromInterface(impl)
	checkFatal(t, err)
	refdb, err := repo.NewRefdb()
	checkFatal(t, err)
	if err := refdb.SetBackend(backend); err != nil {
		t.Fatalf("SetBackend failed: %v", err)
	}
	if err := repo.SetRefdb(refdb); err != nil {
		t.Fatalf("SetRefdb failed: %v", err)
	}
	return refdb
}

// TestRefdbBackendBridge verifies the custom refdb backend bridge: a Go
// implementation is attached to a repository and libgit2 dispatches an
// existence check into the Go Exists callback.
func TestRefdbBackendBridge(t *testing.T) {
	repo := createTestRepo(t)
	repoWorkdir := repo.Workdir()

	impl := &countingRefdbBackend{}
	refdb := installTestRefdbBackend(t, repo, impl)

	// Looking up a reference must route through our backend's Exists/Lookup.
	_, err := repo.References.Lookup("refs/heads/does-not-exist")
	if err == nil {
		t.Fatal("expected lookup of a non-existent ref to fail")
	}

	if impl.existsCalls == 0 && impl.lookupCalls == 0 {
		t.Fatalf("custom backend callbacks were not invoked (exists=%d lookup=%d)", impl.existsCalls, impl.lookupCalls)
	}
	if impl.lastRefName != "refs/heads/does-not-exist" {
		t.Fatalf("backend saw ref name %q, want refs/heads/does-not-exist", impl.lastRefName)
	}

	// Release the Go-side refdb handle and the repository. Freeing the
	// repository releases libgit2's reference on the refdb, which cascades to
	// the backend's free callback (untracking the cgo handle). This exercises
	// the full lifecycle and must leave no leaked pointer handles.
	refdb.Free()
	repo.Free()

	if impl.freeCalls == 0 {
		t.Fatalf("backend Free callback was not invoked")
	}

	_ = os.RemoveAll(repoWorkdir)
}

func TestRefdbBackendMultipleIteratorLifecycle(t *testing.T) {
	repo := createTestRepo(t)
	repoWorkdir := repo.Workdir()
	impl := &iteratorRefdbBackend{countingRefdbBackend: &countingRefdbBackend{}}
	refdb := installTestRefdbBackend(t, repo, impl)

	first, err := repo.NewReferenceIterator()
	checkFatal(t, err)
	second, err := repo.NewReferenceIterator()
	checkFatal(t, err)

	if _, err := first.Next(); !IsErrorCode(err, ErrorCodeIterOver) {
		t.Fatalf("first iterator Next() error = %v, want ErrorCodeIterOver", err)
	}
	if _, err := second.Next(); !IsErrorCode(err, ErrorCodeIterOver) {
		t.Fatalf("second iterator Next() error = %v, want ErrorCodeIterOver", err)
	}

	first.Free()
	second.Free()
	created, freed := impl.counts()
	if created != 2 || freed != 2 {
		t.Fatalf("iterator lifecycle created=%d freed=%d, want 2/2", created, freed)
	}

	refdb.Free()
	repo.Free()
	_ = os.RemoveAll(repoWorkdir)
}

func TestRefdbBackendConcurrentIterators(t *testing.T) {
	repo := createTestRepo(t)
	repoWorkdir := repo.Workdir()
	impl := &iteratorRefdbBackend{countingRefdbBackend: &countingRefdbBackend{}}
	refdb := installTestRefdbBackend(t, repo, impl)

	const iteratorCount = 8
	var wg sync.WaitGroup
	errors := make(chan error, iteratorCount)
	for i := 0; i < iteratorCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			iter, err := repo.NewReferenceIterator()
			if err != nil {
				errors <- err
				return
			}
			defer iter.Free()
			if _, err := iter.Next(); !IsErrorCode(err, ErrorCodeIterOver) {
				errors <- err
			}
		}()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatalf("concurrent iterator failed: %v", err)
		}
	}

	created, freed := impl.counts()
	if created != iteratorCount || freed != iteratorCount {
		t.Fatalf("concurrent iterator lifecycle created=%d freed=%d, want %d/%d", created, freed, iteratorCount, iteratorCount)
	}

	refdb.Free()
	repo.Free()
	_ = os.RemoveAll(repoWorkdir)
}

func TestRefdbBackendCompressCapability(t *testing.T) {
	repo := createTestRepo(t)
	repoWorkdir := repo.Workdir()
	impl := &compressingRefdbBackend{countingRefdbBackend: &countingRefdbBackend{}}
	refdb := installTestRefdbBackend(t, repo, impl)

	if err := refdb.Compress(); err != nil {
		t.Fatalf("Compress failed: %v", err)
	}
	if impl.compressCalls != 1 {
		t.Fatalf("Compress callback count = %d, want 1", impl.compressCalls)
	}

	refdb.Free()
	repo.Free()
	_ = os.RemoveAll(repoWorkdir)
}

func TestRefdbBackendCallbackPanicBecomesError(t *testing.T) {
	repo := createTestRepo(t)
	repoWorkdir := repo.Workdir()
	impl := &panickingRefdbBackend{countingRefdbBackend: &countingRefdbBackend{}}
	refdb := installTestRefdbBackend(t, repo, impl)

	if _, err := repo.References.Lookup("refs/heads/panic"); !IsErrorCode(err, ErrorCodeUser) {
		t.Fatalf("panicking callback error = %v, want ErrorCodeUser", err)
	}

	refdb.Free()
	repo.Free()
	_ = os.RemoveAll(repoWorkdir)
}

func TestRefdbBackendTransactionUpdate(t *testing.T) {
	repo := createTestRepo(t)
	repoWorkdir := repo.Workdir()
	commitID := seedCommit(t, repo)
	impl := &transactionRefdbBackend{countingRefdbBackend: &countingRefdbBackend{}}
	refdb := installTestRefdbBackend(t, repo, impl)

	tx, err := repo.NewTransaction()
	checkFatal(t, err)
	defer tx.Free()
	const refName = "refs/heads/transaction-test"
	if err := tx.LockRef(refName); err != nil {
		t.Fatalf("LockRef failed: %v", err)
	}
	if err := tx.SetTarget(refName, commitID, nil, "transaction update"); err != nil {
		t.Fatalf("SetTarget failed: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	if impl.lockCalls != 1 || impl.unlockCalls != 1 {
		t.Fatalf("transaction callback counts lock=%d unlock=%d, want 1/1", impl.lockCalls, impl.unlockCalls)
	}
	if impl.lastStatus != RefdbBackendUnlockUpdate {
		t.Fatalf("unlock status = %v, want RefdbBackendUnlockUpdate", impl.lastStatus)
	}
	if impl.lastTarget == nil || !impl.lastTarget.Equal(commitID) {
		t.Fatalf("unlock target = %v, want %v", impl.lastTarget, commitID)
	}
	if impl.lastMessage == nil || *impl.lastMessage != "transaction update" {
		t.Fatalf("unlock message = %v, want transaction update", impl.lastMessage)
	}
	if impl.lastLockPayload != "lock:"+refName {
		t.Fatalf("unlock payload = %v, want %q", impl.lastLockPayload, "lock:"+refName)
	}

	refdb.Free()
	repo.Free()
	_ = os.RemoveAll(repoWorkdir)
}

func TestRefdbBackendTransactionCannotBeReusedAfterCommit(t *testing.T) {
	repo := createTestRepo(t)
	defer cleanupTestRepo(t, repo)
	commitID := seedCommit(t, repo)
	impl := &transactionRefdbBackend{countingRefdbBackend: &countingRefdbBackend{}}
	refdb := installTestRefdbBackend(t, repo, impl)
	defer refdb.Free()

	tx, err := repo.NewTransaction()
	checkFatal(t, err)
	if err := tx.LockRef("refs/heads/once"); err != nil {
		t.Fatal(err)
	}
	if err := tx.SetTarget("refs/heads/once", commitID, nil, "once"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); !IsErrorCode(err, ErrorCodeInvalid) {
		t.Fatalf("second Commit error = %v, want ErrorCodeInvalid", err)
	}
	if err := tx.LockRef("refs/heads/twice"); !IsErrorCode(err, ErrorCodeInvalid) {
		t.Fatalf("LockRef after Commit error = %v, want ErrorCodeInvalid", err)
	}
	tx.Free() // idempotent after successful commit
	if impl.unlockCalls != 1 {
		t.Fatalf("unlock callbacks = %d, want 1", impl.unlockCalls)
	}
}

func TestTransactionRejectsNULBeforeLocking(t *testing.T) {
	repo := createTestRepo(t)
	defer cleanupTestRepo(t, repo)
	tx, err := repo.NewTransaction()
	checkFatal(t, err)
	defer tx.Free()
	for _, call := range []func() error{
		func() error { return tx.LockRef("refs/heads/a\x00b") },
		func() error { return tx.Remove("refs/heads/a\x00b") },
		func() error { return tx.SetSymbolicTarget("refs/heads/a", "refs/heads/b\x00c", nil, "msg") },
	} {
		if err := call(); !IsErrorCode(err, ErrorCodeInvalid) {
			t.Fatalf("NUL validation error = %v, want ErrorCodeInvalid", err)
		}
	}
}

func TestRefdbBackendTransactionCancel(t *testing.T) {
	repo := createTestRepo(t)
	repoWorkdir := repo.Workdir()
	impl := &transactionRefdbBackend{countingRefdbBackend: &countingRefdbBackend{}}
	refdb := installTestRefdbBackend(t, repo, impl)

	tx, err := repo.NewTransaction()
	checkFatal(t, err)
	if err := tx.LockRef("refs/heads/cancel-test"); err != nil {
		t.Fatalf("LockRef failed: %v", err)
	}
	tx.Free()

	if impl.lockCalls != 1 || impl.unlockCalls != 1 {
		t.Fatalf("cancel callback counts lock=%d unlock=%d, want 1/1", impl.lockCalls, impl.unlockCalls)
	}
	if impl.lastStatus != RefdbBackendUnlockCancel {
		t.Fatalf("cancel status = %v, want RefdbBackendUnlockCancel", impl.lastStatus)
	}
	if impl.lastMessage != nil {
		t.Fatalf("cancel message = %v, want nil", impl.lastMessage)
	}

	refdb.Free()
	repo.Free()
	_ = os.RemoveAll(repoWorkdir)
}

func TestRepositoryBackendOwnerLifecycle(t *testing.T) {
	repo := createTestRepo(t)
	defer cleanupTestRepo(t, repo)
	backend, err := repo.NewRefdbBackendFs()
	checkFatal(t, err)
	if backend.owner != repo {
		t.Fatal("repository backend does not retain its owner")
	}
	refdb, err := repo.NewRefdb()
	checkFatal(t, err)
	defer refdb.Free()
	if err := refdb.SetBackend(backend); err != nil {
		t.Fatal(err)
	}
	if backend.owner != nil || backend.ptr != nil {
		t.Fatal("SetBackend did not consume backend owner and pointer")
	}
}

func TestRefdbBackendOwnershipTransferIsIdempotent(t *testing.T) {
	repo := createTestRepo(t)
	repoWorkdir := repo.Workdir()
	impl := &countingRefdbBackend{}
	backend, err := NewRefdbBackendFromInterface(impl)
	checkFatal(t, err)
	refdb, err := repo.NewRefdb()
	checkFatal(t, err)

	if err := refdb.SetBackend(backend); err != nil {
		t.Fatalf("SetBackend failed: %v", err)
	}
	if backend.ptr != nil {
		t.Fatal("SetBackend did not consume backend wrapper")
	}
	backend.Free() // must be a no-op after ownership transfer
	backend.Free()
	if err := repo.SetRefdb(refdb); err != nil {
		t.Fatalf("SetRefdb failed: %v", err)
	}

	refdb.Free()
	repo.Free()
	if impl.freeCalls != 1 {
		t.Fatalf("backend Free callback count = %d, want 1", impl.freeCalls)
	}
	_ = os.RemoveAll(repoWorkdir)
}

func TestRefdbFreeIsIdempotent(t *testing.T) {
	repo := createTestRepo(t)
	defer cleanupTestRepo(t, repo)
	refdb, err := repo.NewRefdb()
	checkFatal(t, err)
	refdb.Free()
	refdb.Free()
	if err := refdb.Compress(); err == nil {
		t.Fatal("Compress on freed Refdb unexpectedly succeeded")
	}
}

func TestSetBackendRejectsNilAndFreed(t *testing.T) {
	repo := createTestRepo(t)
	defer cleanupTestRepo(t, repo)
	refdb, err := repo.NewRefdb()
	checkFatal(t, err)
	defer refdb.Free()

	if err := refdb.SetBackend(nil); !IsErrorCode(err, ErrorCodeInvalid) {
		t.Fatalf("SetBackend(nil) error = %v, want ErrorCodeInvalid", err)
	}
	impl := &countingRefdbBackend{}
	backend, err := NewRefdbBackendFromInterface(impl)
	checkFatal(t, err)
	backend.Free()
	if err := refdb.SetBackend(backend); !IsErrorCode(err, ErrorCodeInvalid) {
		t.Fatalf("SetBackend(freed) error = %v, want ErrorCodeInvalid", err)
	}
}

func TestSetRefdbRejectsNil(t *testing.T) {
	repo := createTestRepo(t)
	defer cleanupTestRepo(t, repo)
	if err := repo.SetRefdb(nil); !IsErrorCode(err, ErrorCodeInvalid) {
		t.Fatalf("SetRefdb(nil) error = %v, want ErrorCodeInvalid", err)
	}
}

type initializingRefdbBackend struct {
	*countingRefdbBackend
	initCalls   int
	initialHead *string
	mode        RepositoryInitMode
	flags       RefdbBackendInitFlag
}

func (b *initializingRefdbBackend) Init(initialHead *string, mode RepositoryInitMode, flags RefdbBackendInitFlag) error {
	b.initCalls++
	b.initialHead = initialHead
	b.mode = mode
	b.flags = flags
	return nil
}

func TestRefdbBackendOptionalCapabilityPointers(t *testing.T) {
	base := &countingRefdbBackend{}
	backend, err := NewRefdbBackendFromInterface(base)
	checkFatal(t, err)
	if got := refdbBackendCapabilities(backend); got != 0 {
		t.Fatalf("base backend capabilities = %#x, want 0", got)
	}
	backend.Free()

	initializer := &initializingRefdbBackend{countingRefdbBackend: &countingRefdbBackend{}}
	backend, err = NewRefdbBackendFromInterface(initializer)
	checkFatal(t, err)
	if got := refdbBackendCapabilities(backend); got != refdbBackendCapabilityInit {
		t.Fatalf("initializer capabilities = %#x, want %#x", got, refdbBackendCapabilityInit)
	}
	initialHead := "refs/heads/main"
	flags := RefdbBackendInitIsWorktree | RefdbBackendInitForceHead
	if err := invokeRefdbBackendInit(backend, &initialHead, RepositoryInitMode(0750), flags); err != nil {
		t.Fatalf("invokeRefdbBackendInit failed: %v", err)
	}
	if initializer.initCalls != 1 || initializer.initialHead == nil || *initializer.initialHead != initialHead {
		t.Fatalf("init callback calls=%d head=%v, want 1/%q", initializer.initCalls, initializer.initialHead, initialHead)
	}
	if initializer.mode != RepositoryInitMode(0750) || initializer.flags != flags {
		t.Fatalf("init callback mode=%#o flags=%#x, want %#o/%#x", initializer.mode, initializer.flags, RepositoryInitMode(0750), flags)
	}
	backend.Free()

	compressor := &compressingRefdbBackend{countingRefdbBackend: &countingRefdbBackend{}}
	backend, err = NewRefdbBackendFromInterface(compressor)
	checkFatal(t, err)
	if got := refdbBackendCapabilities(backend); got != refdbBackendCapabilityCompress {
		t.Fatalf("compressor capabilities = %#x, want %#x", got, refdbBackendCapabilityCompress)
	}
	backend.Free()

	locker := &transactionRefdbBackend{countingRefdbBackend: &countingRefdbBackend{}}
	backend, err = NewRefdbBackendFromInterface(locker)
	checkFatal(t, err)
	if got := refdbBackendCapabilities(backend); got != refdbBackendCapabilityLock {
		t.Fatalf("locker capabilities = %#x, want %#x", got, refdbBackendCapabilityLock)
	}
	backend.Free()
}
