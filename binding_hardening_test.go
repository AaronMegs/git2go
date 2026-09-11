package git

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

// TestIsErrorCodeThroughWrappedError covers the errors.As behaviour: a
// GitError must stay recognizable after a caller wraps it with %w, which is
// the normal way Go code adds context to an error.
func TestIsErrorCodeThroughWrappedError(t *testing.T) {
	repo := createTestRepo(t)
	defer cleanupTestRepo(t, repo)

	_, err := repo.References.Lookup("refs/heads/does-not-exist")
	if err == nil {
		t.Fatal("expected a lookup failure")
	}
	if !IsErrorCode(err, ErrorCodeNotFound) {
		t.Fatalf("direct error not recognized: %v", err)
	}

	wrapped := fmt.Errorf("looking up the branch: %w", err)
	if !IsErrorCode(wrapped, ErrorCodeNotFound) {
		t.Error("IsErrorCode failed to see through a %w wrapper")
	}
	if !IsErrorClass(wrapped, ErrorClassReference) {
		t.Error("IsErrorClass failed to see through a %w wrapper")
	}

	// Doubly wrapped, and errors.As must agree with our helpers.
	twice := fmt.Errorf("outer: %w", wrapped)
	if !IsErrorCode(twice, ErrorCodeNotFound) {
		t.Error("IsErrorCode failed on a doubly wrapped error")
	}
	var gitError *GitError
	if !errors.As(twice, &gitError) {
		t.Error("errors.As could not extract the GitError")
	}

	// A nil error and an unrelated error must not match.
	if IsErrorCode(nil, ErrorCodeNotFound) {
		t.Error("IsErrorCode(nil) should be false")
	}
	if IsErrorCode(errors.New("unrelated"), ErrorCodeNotFound) {
		t.Error("IsErrorCode should not match a non-GitError")
	}
}

// TestOdbBackendPackReadsPackedObjects covers the pack-directory backend,
// which was previously unbound. It is the only ODB backend whose options carry
// an object id type but had no Go entry point.
//
// The backend scans `<objects_dir>/pack/`, so the packfile is written there by
// the indexer, which produces both the .pack and the .idx the backend needs.
func TestOdbBackendPackReadsPackedObjects(t *testing.T) {
	tmpPath, err := ioutil.TempDir("", "git2go-odb-pack")
	checkFatal(t, err)
	defer os.RemoveAll(tmpPath)

	packDir := filepath.Join(tmpPath, "pack")
	checkFatal(t, os.MkdirAll(packDir, 0o755))

	idx, err := NewIndexer(packDir, nil, nil)
	checkFatal(t, err)
	defer idx.Free()

	_, err = idx.Write(outOfOrderPack)
	checkFatal(t, err)
	if _, err := idx.Commit(); err != nil {
		t.Fatalf("indexer Commit failed: %v", err)
	}

	odb, err := NewOdb()
	checkFatal(t, err)
	defer odb.Free()

	// tmpPath plays the role of the repository's objects directory.
	backend, err := NewOdbBackendPack(tmpPath)
	if err != nil {
		t.Fatalf("NewOdbBackendPack failed: %v", err)
	}
	// Ownership transfers to the odb, so the backend is not freed here.
	checkFatal(t, odb.AddBackend(backend, 1))

	count := 0
	err = odb.ForEach(func(id *Oid) error {
		count++
		return nil
	})
	checkFatal(t, err)
	if count != 3 {
		t.Fatalf("objects visible through the pack backend = %d, want 3", count)
	}
}

func TestOdbBackendPackRejectsInvalidOidType(t *testing.T) {
	if _, err := NewOdbBackendPackWithOidType("/nonexistent", ObjectIdType(255)); !IsErrorCode(err, ErrorCodeInvalid) {
		t.Fatalf("error = %v, want ErrorCodeInvalid", err)
	}
}

// TestReflogEntryOutlivesReflog pins down that a ReflogEntry copies its data
// out of the parent. Holding the C pointer instead would leave it dangling
// here, and nothing in the type system would stop a caller from doing this.
func TestReflogEntryOutlivesReflog(t *testing.T) {
	repo := createTestRepo(t)
	defer cleanupTestRepo(t, repo)

	commitID := reflogSeedCommit(t, repo)

	reflog, err := repo.ReadReflog("HEAD")
	checkFatal(t, err)
	if reflog.EntryCount() == 0 {
		t.Fatal("expected at least one HEAD reflog entry")
	}

	entry := reflog.EntryByIndex(0)
	if entry == nil {
		t.Fatal("EntryByIndex(0) returned nil")
	}

	// Read everything once while the parent is still alive, so the values can
	// be compared after it is gone.
	idNew := entry.IdNew()
	committer := entry.Committer()
	message := entry.Message()

	// Release the parent. Under the previous implementation every accessor
	// below would have been reading freed memory.
	reflog.Free()

	if got := entry.IdNew(); !got.Equal(idNew) {
		t.Errorf("IdNew after parent Free = %v, want %v", got, idNew)
	}
	if !entry.IdNew().Equal(commitID) {
		t.Errorf("IdNew = %v, want the seeded commit %v", entry.IdNew(), commitID)
	}
	if got := entry.Message(); got != message {
		t.Errorf("Message after parent Free = %q, want %q", got, message)
	}
	if got := entry.Committer(); got.Email != committer.Email {
		t.Errorf("Committer after parent Free = %q, want %q", got.Email, committer.Email)
	}
}

// TestOptionsVersionsAccepted asserts the startup check passes for the library
// git2go is actually linked against. It fails when the headers used at compile
// time are newer than the loaded library.
func TestOptionsVersionsAccepted(t *testing.T) {
	if err := checkOptionsVersions(); err != nil {
		t.Fatalf("options version check failed: %v", err)
	}
}
