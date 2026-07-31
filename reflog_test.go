package git

import (
	"testing"
	"time"
)

// reflogSeedCommit creates an initial commit on HEAD so the repository has a
// non-empty HEAD reflog to read, and returns the commit OID.
func reflogSeedCommit(t *testing.T, repo *Repository) *Oid {
	t.Helper()

	sig := &Signature{
		Name:  "Reflog Tester",
		Email: "reflog@example.com",
		When:  time.Date(2026, 07, 31, 10, 0, 0, 0, time.UTC),
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

func TestReflogReadAndEntries(t *testing.T) {
	repo := createTestRepo(t)
	defer cleanupTestRepo(t, repo)

	commitID := reflogSeedCommit(t, repo)

	reflog, err := repo.ReadReflog("HEAD")
	checkFatal(t, err)
	defer reflog.Free()

	if reflog.EntryCount() < 1 {
		t.Fatalf("expected at least one HEAD reflog entry, got %d", reflog.EntryCount())
	}

	// Entry 0 is the most recent (the commit we just made).
	entry := reflog.EntryByIndex(0)
	if entry == nil {
		t.Fatal("EntryByIndex(0) returned nil")
	}
	if entry.IdNew() == nil || !entry.IdNew().Equal(commitID) {
		t.Fatalf("entry IdNew = %v, want %v", entry.IdNew(), commitID)
	}
	if c := entry.Committer(); c == nil || c.Email != "reflog@example.com" {
		t.Fatalf("unexpected committer on reflog entry: %+v", c)
	}
	_ = entry.Message() // may be empty depending on libgit2 defaults
	_ = entry.IdOld()   // zero oid for the first entry

	// Out-of-range index yields nil.
	if reflog.EntryByIndex(9999) != nil {
		t.Fatal("EntryByIndex(out-of-range) should be nil")
	}
}

func TestReflogAppendWriteDrop(t *testing.T) {
	repo := createTestRepo(t)
	defer cleanupTestRepo(t, repo)

	commitID := reflogSeedCommit(t, repo)

	reflog, err := repo.ReadReflog("HEAD")
	checkFatal(t, err)

	before := reflog.EntryCount()

	sig := &Signature{
		Name:  "Appender",
		Email: "appender@example.com",
		When:  time.Date(2026, 07, 31, 11, 0, 0, 0, time.UTC),
	}
	if err := reflog.Append(commitID, sig, "manual reflog entry"); err != nil {
		t.Fatalf("Append failed: %v", err)
	}
	if err := reflog.Write(); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	reflog.Free()

	// Re-read and confirm the appended entry persisted.
	reflog2, err := repo.ReadReflog("HEAD")
	checkFatal(t, err)
	defer reflog2.Free()

	if reflog2.EntryCount() != before+1 {
		t.Fatalf("entry count = %d, want %d after append", reflog2.EntryCount(), before+1)
	}
	// The appended entry is the most recent (index 0).
	if msg := reflog2.EntryByIndex(0).Message(); msg != "manual reflog entry" {
		t.Fatalf("appended entry message = %q, want %q", msg, "manual reflog entry")
	}

	// Drop the appended entry and persist.
	if err := reflog2.Drop(0, false); err != nil {
		t.Fatalf("Drop failed: %v", err)
	}
	if err := reflog2.Write(); err != nil {
		t.Fatalf("Write after drop failed: %v", err)
	}
	if reflog2.EntryCount() != before {
		t.Fatalf("entry count after drop = %d, want %d", reflog2.EntryCount(), before)
	}
}

func TestReflogRenameDelete(t *testing.T) {
	repo := createTestRepo(t)
	defer cleanupTestRepo(t, repo)

	commitID := reflogSeedCommit(t, repo)

	// Create a branch so it has its own reflog to rename/delete.
	commit, err := repo.LookupCommit(commitID)
	checkFatal(t, err)
	defer commit.Free()

	branch, err := repo.CreateBranch("reflog-branch", commit, false)
	checkFatal(t, err)
	defer branch.Free()

	// Force a reflog to exist for the branch by reading it (it may be empty on
	// creation depending on config); rename should still succeed.
	if err := repo.RenameReflog("refs/heads/reflog-branch", "refs/heads/reflog-renamed"); err != nil {
		t.Fatalf("RenameReflog failed: %v", err)
	}

	if err := repo.DeleteReflog("refs/heads/reflog-renamed"); err != nil {
		t.Fatalf("DeleteReflog failed: %v", err)
	}
}
