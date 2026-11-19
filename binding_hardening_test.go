package git

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"unsafe"
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

func TestFreeNilAndZeroValues(t *testing.T) {
	cases := []struct {
		name  string
		value interface{ Free() error }
	}{
		{"nil diff", (*Diff)(nil)}, {"zero diff", &Diff{}},
		{"nil stats", (*DiffStats)(nil)}, {"zero stats", &DiffStats{}},
		{"nil patch", (*Patch)(nil)}, {"zero patch", &Patch{}},
		{"nil blame", (*Blame)(nil)}, {"zero blame", &Blame{}},
		{"nil note", (*Note)(nil)}, {"zero note", &Note{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.value.Free(); err != ErrInvalid {
				t.Fatalf("Free = %v, want ErrInvalid", err)
			}
		})
	}
	for _, status := range []*StatusList{nil, {}} {
		status.Free()
		status.Free()
	}
	var remote *Remote
	remote.free()
}

func TestFreeOwnedResources(t *testing.T) {
	repo := createTestRepo(t)
	defer cleanupTestRepo(t, repo)
	oldTree, newTree := createTestTrees(t, repo)
	defer oldTree.Free()
	defer newTree.Free()
	diff, err := repo.DiffTreeToTree(oldTree, newTree, nil)
	checkFatal(t, err)
	defer diff.Free()

	borrowed := &Diff{ptr: diff.ptr, repo: repo}
	checkFatal(t, borrowed.Free())
	if borrowed.ptr != nil || borrowed.Free() != ErrInvalid {
		t.Fatal("borrowed diff must invalidate only its own wrapper")
	}
	stats, err := diff.Stats()
	checkFatal(t, err)
	defer stats.Free()
	patch, err := diff.Patch(0)
	checkFatal(t, err)
	defer patch.Free()
	checkFatal(t, diff.Free())
	if diff.ptr != nil || diff.Free() != ErrInvalid {
		t.Fatal("diff Free must clear the pointer and preserve ErrInvalid on repetition")
	}
	if stats.FilesChanged() != 1 {
		t.Fatal("stats must remain usable after Diff.Free")
	}
	_, err = patch.String()
	checkFatal(t, err)
	checkFatal(t, stats.Free())
	checkFatal(t, patch.Free())
	if stats.ptr != nil || patch.ptr != nil || stats.Free() != ErrInvalid || patch.Free() != ErrInvalid {
		t.Fatal("stats and patch must be released exactly once")
	}

	blame, err := repo.BlameFile("README", nil)
	checkFatal(t, err)
	defer blame.Free()
	checkFatal(t, blame.Free())
	if blame.ptr != nil || blame.Free() != ErrInvalid {
		t.Fatal("blame must be released exactly once")
	}

	head, err := repo.Head()
	checkFatal(t, err)
	defer head.Free()
	commit, err := repo.LookupCommit(head.Target())
	checkFatal(t, err)
	defer commit.Free()
	note, _ := createTestNote(t, repo, commit)
	defer note.Free()
	checkFatal(t, note.Free())
	if note.ptr != nil || note.Free() != ErrInvalid {
		t.Fatal("note must be released exactly once")
	}

	status, err := repo.StatusList(&StatusOptions{Show: StatusShowIndexAndWorkdir})
	checkFatal(t, err)
	defer status.Free()
	status.Free()
	status.Free()
	if status.ptr != nil {
		t.Fatal("status Free must clear the pointer")
	}
}

// TestOdbBackendTypeMismatchRejected verifies GG-02: attaching an OdbBackend
// whose ObjectIdType does not match the target Odb must fail with
// ErrorCodeInvalid instead of silently writing truncated or corrupt objects.
func TestOdbBackendTypeMismatchRejected(t *testing.T) {
	if !IsSha256Supported() {
		t.Skip("libgit2 built without SHA256 support")
	}

	objectsDir := t.TempDir()
	packDir := filepath.Join(objectsDir, "pack")
	checkFatal(t, os.MkdirAll(packDir, 0o755))

	// Seed one SHA1 packfile so OnePack/Pack constructors succeed cleanly.
	idx, err := NewIndexer(packDir, nil, nil)
	checkFatal(t, err)
	defer idx.Free()
	_, err = idx.Write(outOfOrderPack)
	checkFatal(t, err)
	packOid, err := idx.Commit()
	checkFatal(t, err)
	idxPath := filepath.Join(packDir, "pack-"+packOid.String()+".idx")

	sha256Odb, err := NewOdbWithOidType(ObjectIdSHA256)
	checkFatal(t, err)
	defer sha256Odb.Free()

	sha1Backends := []struct {
		name string
		make func() (*OdbBackend, error)
	}{
		{"loose", func() (*OdbBackend, error) { return NewOdbBackendLoose(objectsDir, -1, false, 0, 0) }},
		{"pack", func() (*OdbBackend, error) { return NewOdbBackendPack(objectsDir) }},
		{"one-pack", func() (*OdbBackend, error) { return NewOdbBackendOnePack(idxPath) }},
		{"loose-default", func() (*OdbBackend, error) {
			return NewOdbBackendLooseWithOidType(objectsDir, -1, false, 0, 0, ObjectIdTypeDefault)
		}},
	}

	for _, tc := range sha1Backends {
		t.Run("sha1-"+tc.name+"-into-sha256-odb", func(t *testing.T) {
			backend, err := tc.make()
			checkFatal(t, err)
			defer backend.Free()

			if err := sha256Odb.AddBackend(backend, 1); !IsErrorCode(err, ErrorCodeInvalid) {
				t.Fatalf("AddBackend mismatch error = %v, want ErrorCodeInvalid", err)
			}
			if backend.ptr == nil {
				t.Fatal("failed AddBackend must leave ownership with caller")
			}
			if err := sha256Odb.AddAlternate(backend, 1); !IsErrorCode(err, ErrorCodeInvalid) {
				t.Fatalf("AddAlternate mismatch error = %v, want ErrorCodeInvalid", err)
			}
			if backend.ptr == nil {
				t.Fatal("failed AddAlternate must leave ownership with caller")
			}

			// The rejected backend is still valid and can be attached to a matching SHA1 Odb.
			sha1Odb, err := NewOdb()
			checkFatal(t, err)
			defer sha1Odb.Free()
			checkFatal(t, sha1Odb.AddBackend(backend, 1))
			if backend.ptr != nil {
				t.Fatal("successful AddBackend must clear wrapper pointer")
			}
		})
	}

	// Reverse direction: SHA256 backend must be rejected by both NewOdb() and
	// NewOdbWithOidType(ObjectIdTypeDefault).
	for _, tc := range []struct {
		name string
		make func() (*Odb, error)
	}{
		{"NewOdb", NewOdb},
		{"NewOdbWithOidType(Default)", func() (*Odb, error) { return NewOdbWithOidType(ObjectIdTypeDefault) }},
	} {
		t.Run("sha256-loose-into-"+tc.name, func(t *testing.T) {
			odb, err := tc.make()
			checkFatal(t, err)
			defer odb.Free()
			if odb.OidType() != ObjectIdSHA1 {
				t.Fatalf("odb.OidType() = %v, want ObjectIdSHA1", odb.OidType())
			}

			backend, err := NewOdbBackendLooseWithOidType(t.TempDir(), -1, false, 0, 0, ObjectIdSHA256)
			checkFatal(t, err)
			defer backend.Free()

			if err := odb.AddBackend(backend, 1); !IsErrorCode(err, ErrorCodeInvalid) {
				t.Fatalf("AddBackend mismatch error = %v, want ErrorCodeInvalid", err)
			}
			if err := odb.AddAlternate(backend, 1); !IsErrorCode(err, ErrorCodeInvalid) {
				t.Fatalf("AddAlternate mismatch error = %v, want ErrorCodeInvalid", err)
			}
		})
	}
}

// TestOdbAddBackendOwnershipAndGuards verifies GG-12: nil/freed guards,
// ownership transfer clearing the wrapper so later backend.Free() is a safe
// no-op, rejection of re-attaching an already-transferred backend, and
// compatibility with NewOdbBackendFromC (unknown oidType).
func TestOdbAddBackendOwnershipAndGuards(t *testing.T) {
	var nilOdb *Odb
	var nilBackend *OdbBackend

	if err := nilOdb.AddBackend(nilBackend, 1); !IsErrorCode(err, ErrorCodeInvalid) {
		t.Fatalf("nil odb AddBackend = %v, want ErrorCodeInvalid", err)
	}
	if err := nilOdb.AddAlternate(nilBackend, 1); !IsErrorCode(err, ErrorCodeInvalid) {
		t.Fatalf("nil odb AddAlternate = %v, want ErrorCodeInvalid", err)
	}

	odb, err := NewOdb()
	checkFatal(t, err)
	defer odb.Free()

	if err := odb.AddBackend(nilBackend, 1); !IsErrorCode(err, ErrorCodeInvalid) {
		t.Fatalf("nil backend AddBackend = %v, want ErrorCodeInvalid", err)
	}

	freedBackend, err := NewOdbBackendLoose(t.TempDir(), -1, false, 0, 0)
	checkFatal(t, err)
	freedBackend.Free()
	freedBackend.Free()
	if err := odb.AddBackend(freedBackend, 1); !IsErrorCode(err, ErrorCodeInvalid) {
		t.Fatalf("freed backend AddBackend = %v, want ErrorCodeInvalid", err)
	}

	// Normal attach: ownership transfers, wrapper is cleared, and a subsequent
	// Free() must be a safe no-op while the odb remains usable.
	looseDir := t.TempDir()
	backend, err := NewOdbBackendLoose(looseDir, -1, false, 0, 0)
	checkFatal(t, err)
	checkFatal(t, odb.AddBackend(backend, 1))
	if backend.ptr != nil {
		t.Fatal("AddBackend must clear backend.ptr after ownership transfer")
	}
	backend.Free()
	if err := odb.AddAlternate(backend, 2); !IsErrorCode(err, ErrorCodeInvalid) {
		t.Fatalf("re-attaching transferred backend = %v, want ErrorCodeInvalid", err)
	}

	written, err := odb.Write([]byte("hello ownership"), ObjectBlob)
	checkFatal(t, err)
	if !odb.Exists(written) {
		t.Fatal("odb must remain functional after backend.Free() no-op")
	}

	// Unknown-type backend wrapped via NewOdbBackendFromC skips the type check
	// and transfers cleanly.
	rawBackend, err := NewOdbBackendLoose(t.TempDir(), -1, false, 0, 0)
	checkFatal(t, err)
	cptr := unsafe.Pointer(rawBackend.ptr)
	rawBackend.ptr = nil
	runtime.SetFinalizer(rawBackend, nil)

	fromC := NewOdbBackendFromC(cptr)
	checkFatal(t, odb.AddAlternate(fromC, 2))
	if fromC.ptr != nil {
		t.Fatal("AddAlternate must clear NewOdbBackendFromC wrapper pointer")
	}
	fromC.Free()

	// Freed Odb must reject new backends without touching C.
	odb.Free()
	other, err := NewOdbBackendLoose(t.TempDir(), -1, false, 0, 0)
	checkFatal(t, err)
	defer other.Free()
	if err := odb.AddBackend(other, 1); !IsErrorCode(err, ErrorCodeInvalid) {
		t.Fatalf("freed odb AddBackend = %v, want ErrorCodeInvalid", err)
	}
}

// TestRepositoryDerivedOdbHelpers verifies GG-03: repository-derived ODB and
// backend constructors inherit the repository's ObjectIdType automatically.
func TestRepositoryDerivedOdbHelpers(t *testing.T) {
	var nilRepo *Repository
	if _, err := NewOdbForRepository(nilRepo); !IsErrorCode(err, ErrorCodeInvalid) {
		t.Fatalf("NewOdbForRepository(nil) = %v, want ErrorCodeInvalid", err)
	}
	if _, err := nilRepo.NewOdbBackendLoose(t.TempDir(), -1, false, 0, 0); !IsErrorCode(err, ErrorCodeInvalid) {
		t.Fatalf("nil repo NewOdbBackendLoose = %v, want ErrorCodeInvalid", err)
	}
	if _, err := nilRepo.NewOdbBackendPack(t.TempDir()); !IsErrorCode(err, ErrorCodeInvalid) {
		t.Fatalf("nil repo NewOdbBackendPack = %v, want ErrorCodeInvalid", err)
	}
	if _, err := nilRepo.NewOdbBackendOnePack("/nonexistent.idx"); !IsErrorCode(err, ErrorCodeInvalid) {
		t.Fatalf("nil repo NewOdbBackendOnePack = %v, want ErrorCodeInvalid", err)
	}

	formats := []ObjectIdType{ObjectIdSHA1}
	if IsSha256Supported() {
		formats = append(formats, ObjectIdSHA256)
	}

	for _, oidType := range formats {
		t.Run(oidType.String(), func(t *testing.T) {
			repo, err := InitRepositoryWithOidType(t.TempDir(), true, oidType)
			checkFatal(t, err)
			defer repo.Free()

			odb, err := NewOdbForRepository(repo)
			checkFatal(t, err)
			defer odb.Free()
			if odb.OidType() != oidType {
				t.Fatalf("NewOdbForRepository().OidType() = %v, want %v", odb.OidType(), oidType)
			}

			looseDir := t.TempDir()
			looseBackend, err := repo.NewOdbBackendLoose(looseDir, -1, false, 0, 0)
			checkFatal(t, err)
			defer looseBackend.Free()
			checkFatal(t, odb.AddBackend(looseBackend, 1))

			packBackend, err := repo.NewOdbBackendPack(looseDir)
			checkFatal(t, err)
			defer packBackend.Free()
			checkFatal(t, odb.AddAlternate(packBackend, 2))

			id, err := odb.Write([]byte("repo-derived-"+oidType.String()), ObjectBlob)
			checkFatal(t, err)
			if id.Type() != oidType || len(id.String()) != oidType.HexSize() {
				t.Fatalf("written oid = %s (type %v), want type %v len %d", id, id.Type(), oidType, oidType.HexSize())
			}
			if !odb.Exists(id) {
				t.Fatalf("written object %s not found in derived odb", id)
			}
		})
	}
}
