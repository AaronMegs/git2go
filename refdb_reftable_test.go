package git

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newReftableRepo initializes a bare reftable repository in a temp dir, or
// skips the test if the linked libgit2 build has no reftable support.
func newReftableRepo(t *testing.T) (*Repository, string) {
	t.Helper()

	dir, err := ioutil.TempDir("", "git2go-reftable")
	checkFatal(t, err)

	repoPath := filepath.Join(dir, "repo")
	repo, err := InitRepositoryExt(repoPath, &RepositoryInitOptions{
		Flags:     RepositoryInitMkpath | RepositoryInitBare,
		RefdbType: RefdbReftable,
	})
	if err != nil {
		os.RemoveAll(dir)
		t.Skipf("reftable backend not supported by this libgit2 build: %v", err)
	}
	return repo, dir
}

// TestIsReftableSupported exercises the runtime probe. On this build it must
// report true (we vendor a reftable-capable libgit2 master).
func TestIsReftableSupported(t *testing.T) {
	if !IsReftableSupported() {
		t.Skip("reftable not supported by this libgit2 build")
	}
	// If supported, the probe must be stable across repeated calls.
	if !IsReftableSupported() {
		t.Fatal("IsReftableSupported flipped from true to false")
	}
}

// TestRefdbTypeString checks the canonical config token mapping.
func TestRefdbTypeString(t *testing.T) {
	cases := map[RefdbType]string{
		RefdbDefault:  "files",
		RefdbFiles:    "files",
		RefdbReftable: "reftable",
		RefdbType(99): "unknown",
	}
	for in, want := range cases {
		if got := in.String(); got != want {
			t.Errorf("RefdbType(%d).String() = %q, want %q", int(in), got, want)
		}
	}
}

// TestRefStorageFormatFiles verifies detection on a default (files) repo.
func TestRefStorageFormatFiles(t *testing.T) {
	repo := createTestRepo(t)
	defer cleanupTestRepo(t, repo)

	format, err := repo.RefStorageFormat()
	checkFatal(t, err)
	if format != RefdbFiles {
		t.Fatalf("RefStorageFormat() = %v, want RefdbFiles", format)
	}
}

// TestRefStorageFormatReftable verifies detection on a reftable repo.
func TestRefStorageFormatReftable(t *testing.T) {
	repo, dir := newReftableRepo(t)
	defer os.RemoveAll(dir)
	defer repo.Free()

	format, err := repo.RefStorageFormat()
	checkFatal(t, err)
	if format != RefdbReftable {
		t.Fatalf("RefStorageFormat() = %v, want RefdbReftable", format)
	}
}

// TestRepositoryRefdb exercises Repository.Refdb() on both backends.
// TestRepositoryRefdb covers Repository.Refdb() on both backends.
//
// The two backends are separate subtests on purpose: newReftableRepo skips when
// reftable is unavailable, and skipping the parent test would also discard the
// files-backend result that had already passed.
func TestRepositoryRefdb(t *testing.T) {
	t.Run("files", func(t *testing.T) {
		filesRepo := createTestRepo(t)
		defer cleanupTestRepo(t, filesRepo)

		refdb, err := filesRepo.Refdb()
		checkFatal(t, err)
		if refdb == nil {
			t.Fatal("Refdb() returned nil on files repo")
		}
		refdb.Free()
	})

	t.Run("reftable", func(t *testing.T) {
		rtRepo, dir := newReftableRepo(t)
		defer os.RemoveAll(dir)
		defer rtRepo.Free()

		rtRefdb, err := rtRepo.Refdb()
		checkFatal(t, err)
		if rtRefdb == nil {
			t.Fatal("Refdb() returned nil on reftable repo")
		}
		rtRefdb.Free()
	})
}

// TestOpenRefdb exercises Repository.OpenRefdb(), which returns a ready-to-use
// refdb with the default backend already attached.
func TestOpenRefdb(t *testing.T) {
	repo := createTestRepo(t)
	defer cleanupTestRepo(t, repo)

	refdb, err := repo.OpenRefdb()
	checkFatal(t, err)
	if refdb == nil {
		t.Fatal("OpenRefdb() returned nil")
	}
	refdb.Free()
}

// TestRefdbCompressFiles verifies Compress() on the files backend (packs refs).
func TestRefdbCompressFiles(t *testing.T) {
	repo := createTestRepo(t)
	defer cleanupTestRepo(t, repo)

	seedCommit(t, repo)

	refdb, err := repo.OpenRefdb()
	checkFatal(t, err)
	defer refdb.Free()

	if err := refdb.Compress(); err != nil {
		t.Fatalf("Compress() on files backend failed: %v", err)
	}
}

// TestRefdbCompressReftable verifies Compress() on the reftable backend
// (compacts the reftable stack).
func TestRefdbCompressReftable(t *testing.T) {
	repo, dir := newReftableRepo(t)
	defer os.RemoveAll(dir)
	defer repo.Free()

	refdb, err := repo.OpenRefdb()
	checkFatal(t, err)
	defer refdb.Free()

	if err := refdb.Compress(); err != nil {
		t.Fatalf("Compress() on reftable backend failed: %v", err)
	}
}

// TestNewRefdbBackendReftable verifies the explicit reftable backend
// constructor produces a usable backend on a reftable repo.
func TestNewRefdbBackendReftable(t *testing.T) {
	repo, dir := newReftableRepo(t)
	defer os.RemoveAll(dir)
	defer repo.Free()

	backend, err := repo.NewRefdbBackendReftable()
	if err != nil {
		t.Skipf("NewRefdbBackendReftable not usable on this build: %v", err)
	}
	if backend == nil || backend.ptr == nil {
		t.Fatal("NewRefdbBackendReftable returned nil backend")
	}
	// Ownership: the backend is not yet attached to a refdb, so we free it.
	backend.Free()
}

// TestNewRefdbBackendFs verifies the explicit files backend constructor.
func TestNewRefdbBackendFs(t *testing.T) {
	repo := createTestRepo(t)
	defer cleanupTestRepo(t, repo)

	backend, err := repo.NewRefdbBackendFs()
	checkFatal(t, err)
	if backend == nil || backend.ptr == nil {
		t.Fatal("NewRefdbBackendFs returned nil backend")
	}
	backend.Free()
}

// TestReftableBranchLifecycle exercises a full create/lookup/delete branch
// cycle on a reftable repository, ensuring ref CRUD works end-to-end through
// the reftable backend.
func TestReftableBranchLifecycle(t *testing.T) {
	repo, dir := newReftableRepo(t)
	defer os.RemoveAll(dir)
	defer repo.Free()

	commitID := seedCommit(t, repo)

	commit, err := repo.LookupCommit(commitID)
	checkFatal(t, err)
	defer commit.Free()

	branch, err := repo.CreateBranch("feature/reftable", commit, false)
	checkFatal(t, err)
	defer branch.Free()

	looked, err := repo.LookupBranch("feature/reftable", BranchLocal)
	checkFatal(t, err)
	defer looked.Free()

	if looked.Target() == nil || !looked.Target().Equal(commitID) {
		t.Fatalf("branch target mismatch: got %v, want %v", looked.Target(), commitID)
	}

	if err := looked.Delete(); err != nil {
		t.Fatalf("failed to delete branch on reftable repo: %v", err)
	}

	// After deletion the branch must no longer resolve.
	if _, err := repo.LookupBranch("feature/reftable", BranchLocal); err == nil {
		t.Fatal("expected lookup of deleted branch to fail")
	}
}

// seedCommit creates an empty-tree initial commit on HEAD and returns its OID.
// Works on both files and reftable repositories.
func TestReftableReopenPersistence(t *testing.T) {
	repo, dir := newReftableRepo(t)
	defer os.RemoveAll(dir)

	commitID := seedCommit(t, repo)
	commit, err := repo.LookupCommit(commitID)
	checkFatal(t, err)
	branch, err := repo.CreateBranch("persisted", commit, false)
	checkFatal(t, err)
	branch.Free()
	commit.Free()
	repoPath := filepath.Join(dir, "repo")
	repo.Free()

	reopened, err := OpenRepository(repoPath)
	checkFatal(t, err)
	defer reopened.Free()
	format, err := reopened.RefStorageFormat()
	checkFatal(t, err)
	if format != RefdbReftable {
		t.Fatalf("reopened RefStorageFormat() = %v, want RefdbReftable", format)
	}
	persisted, err := reopened.LookupBranch("persisted", BranchLocal)
	checkFatal(t, err)
	defer persisted.Free()
	if target := persisted.Target(); target == nil || !target.Equal(commitID) {
		t.Fatalf("reopened branch target = %v, want %v", target, commitID)
	}
}

func TestReftableReflogLifecycle(t *testing.T) {
	repo, dir := newReftableRepo(t)
	defer os.RemoveAll(dir)
	defer repo.Free()
	commitID := seedCommit(t, repo)

	reflog, err := repo.ReadReflog("HEAD")
	checkFatal(t, err)
	before := reflog.EntryCount()
	sig := &Signature{Name: "Reftable Reflog", Email: "reftable-reflog@example.com", When: time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)}
	if err := reflog.Append(commitID, sig, "reftable reflog entry"); err != nil {
		t.Fatalf("Append failed: %v", err)
	}
	if err := reflog.Write(); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	reflog.Free()

	reloaded, err := repo.ReadReflog("HEAD")
	checkFatal(t, err)
	defer reloaded.Free()
	if reloaded.EntryCount() != before+1 {
		t.Fatalf("reftable reflog entry count = %d, want %d", reloaded.EntryCount(), before+1)
	}
	entry := reloaded.EntryByIndex(0)
	if entry == nil || entry.Message() != "reftable reflog entry" {
		t.Fatalf("latest reftable reflog message = %v, want reftable reflog entry", entry)
	}
	if err := reloaded.Drop(0, false); err != nil {
		t.Fatalf("Drop failed: %v", err)
	}
	if err := reloaded.Write(); err != nil {
		t.Fatalf("Write after drop failed: %v", err)
	}
}

func TestReftableSymbolicReferenceLifecycle(t *testing.T) {
	repo, dir := newReftableRepo(t)
	defer os.RemoveAll(dir)
	defer repo.Free()
	seedCommit(t, repo)

	ref, err := repo.References.CreateSymbolic("refs/test/symbolic", "refs/heads/master", true, "create symbolic")
	checkFatal(t, err)
	if ref.SymbolicTarget() != "refs/heads/master" {
		t.Fatalf("symbolic target = %q, want refs/heads/master", ref.SymbolicTarget())
	}
	renamed, err := ref.Rename("refs/test/renamed", true, "rename symbolic")
	ref.Free()
	checkFatal(t, err)
	if renamed.Name() != "refs/test/renamed" || renamed.SymbolicTarget() != "refs/heads/master" {
		t.Fatalf("renamed symbolic ref = %q -> %q", renamed.Name(), renamed.SymbolicTarget())
	}
	if err := renamed.Delete(); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	renamed.Free()
	if _, err := repo.References.Lookup("refs/test/renamed"); err == nil {
		t.Fatal("expected deleted symbolic reference lookup to fail")
	}
}

// TestReftableTransactionUnsupported pins down an upstream limitation: the
// reftable backend implements no lock/unlock callbacks ("TODO: transaction API"
// in libgit2's refdb_reftable.c), so reference transactions cannot be used on a
// reftable repository.
//
// The failure deliberately surfaces at LockRef rather than NewTransaction,
// which is why this is documented on Transaction and asserted here.
//
// If upstream implements the reftable transaction API, this test starts failing
// and is the signal to relax the Transaction documentation instead of silently
// keeping a stale caveat.
func TestReftableTransactionUnsupported(t *testing.T) {
	repo, dir := newReftableRepo(t)
	defer os.RemoveAll(dir)
	defer repo.Free()
	seedCommit(t, repo)

	tx, err := repo.NewTransaction()
	if err != nil {
		t.Fatalf("NewTransaction on a reftable repo should succeed, got %v", err)
	}
	defer tx.Free()

	err = tx.LockRef("refs/heads/transactional")
	if err == nil {
		t.Fatal("LockRef unexpectedly succeeded on a reftable repository; " +
			"upstream may have implemented the reftable transaction API — " +
			"update the Transaction docs and README if so")
	}
	if !strings.Contains(err.Error(), "does not support locking") {
		t.Fatalf("LockRef error = %v, want a backend-locking-unsupported error", err)
	}
}

// TestFilesTransactionSupported is the positive counterpart: the same sequence
// must work on the files backend, so the test above cannot pass merely because
// the transaction API is broken everywhere.
func TestFilesTransactionSupported(t *testing.T) {
	repo := createTestRepo(t)
	defer cleanupTestRepo(t, repo)
	seedCommit(t, repo)

	tx, err := repo.NewTransaction()
	checkFatal(t, err)
	defer tx.Free()

	if err := tx.LockRef("refs/heads/transactional"); err != nil {
		t.Fatalf("LockRef on a files repo failed: %v", err)
	}
}

func seedCommit(t *testing.T, repo *Repository) *Oid {
	t.Helper()

	sig := &Signature{
		Name:  "Reftable Tester",
		Email: "reftable@example.com",
		When:  time.Date(2026, 07, 29, 12, 0, 0, 0, time.UTC),
	}

	idx, err := repo.Index()
	checkFatal(t, err)
	defer idx.Free()

	treeID, err := idx.WriteTree()
	checkFatal(t, err)

	tree, err := repo.LookupTree(treeID)
	checkFatal(t, err)
	defer tree.Free()

	commitID, err := repo.CreateCommit("HEAD", sig, sig, "initial commit", tree)
	checkFatal(t, err)
	return commitID
}
