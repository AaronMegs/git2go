package git

import (
	"os"
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

// TestRefdbBackendBridge verifies the custom refdb backend bridge: a Go
// implementation is attached to a repository and libgit2 dispatches an
// existence check into the Go Exists callback.
func TestRefdbBackendBridge(t *testing.T) {
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
	repo.SetRefdb(refdb)

	// Looking up a reference must route through our backend's Exists/Lookup.
	_, err = repo.References.Lookup("refs/heads/does-not-exist")
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
