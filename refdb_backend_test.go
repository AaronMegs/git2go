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

func (b *countingRefdbBackend) Lookup(refName string) (*Reference, error) {
	b.lookupCalls++
	b.lastRefName = refName
	return nil, MakeGitError2(int(ErrorCodeNotFound))
}

func (b *countingRefdbBackend) Iterator(glob string) (RefdbBackendIterator, error) {
	return nil, MakeGitError2(int(ErrorCodeNotFound))
}

func (b *countingRefdbBackend) Write(ref *Reference, force bool, who *Signature, message string, old *Oid, oldTarget string) error {
	return nil
}

func (b *countingRefdbBackend) Rename(oldName, newName string, force bool, who *Signature, message string) (*Reference, error) {
	return nil, MakeGitError2(int(ErrorCodeNotFound))
}

func (b *countingRefdbBackend) Delete(refName string, oldID *Oid, oldTarget string) error {
	return nil
}

func (b *countingRefdbBackend) HasLog(refName string) (bool, error) { return false, nil }
func (b *countingRefdbBackend) EnsureLog(refName string) error      { return nil }
func (b *countingRefdbBackend) Free()                               { b.freeCalls++ }

func (b *countingRefdbBackend) ReflogRead(name string) (*Reflog, error) {
	return nil, MakeGitError2(int(ErrorCodeNotFound))
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
	return nil, MakeGitError2(int(ErrorCodeIterOver))
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

func TestSetRefdbRejectsNil(t *testing.T) {
	repo := createTestRepo(t)
	defer cleanupTestRepo(t, repo)
	if err := repo.SetRefdb(nil); !IsErrorCode(err, ErrorCodeInvalid) {
		t.Fatalf("SetRefdb(nil) error = %v, want ErrorCodeInvalid", err)
	}
}
