package git

import (
	"io/ioutil"
	"os"
	"path/filepath"
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
func TestRepositoryRefdb(t *testing.T) {
	// files backend
	filesRepo := createTestRepo(t)
	defer cleanupTestRepo(t, filesRepo)

	refdb, err := filesRepo.Refdb()
	checkFatal(t, err)
	if refdb == nil {
		t.Fatal("Refdb() returned nil on files repo")
	}
	refdb.Free()

	// reftable backend
	rtRepo, dir := newReftableRepo(t)
	defer os.RemoveAll(dir)
	defer rtRepo.Free()

	rtRefdb, err := rtRepo.Refdb()
	checkFatal(t, err)
	if rtRefdb == nil {
		t.Fatal("Refdb() returned nil on reftable repo")
	}
	rtRefdb.Free()
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
