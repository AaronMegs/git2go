package git

import "testing"

// The refdb backend bridge already has panic coverage in
// TestRefdbBackendCallbackPanicBecomesError. These tests cover the callbacks
// that gained a panic boundary later, which use the other two guard flavours.
//
// A panic unwinding across the cgo boundary is undefined behaviour: the
// intervening libgit2 C frames carry no unwind information. Every //export'ed
// callback therefore has to contain its own panics.

// TestPanickingTreeWalkCallbackAborts covers recoverCallbackCode: a callback
// that reports failure through its return code alone, because its original
// error travels back via an errorTarget field rather than an errorMessage
// out-param. Swallowing the panic there would return 0, i.e. success, and let
// libgit2 continue on top of a callback that never ran to completion.
func TestPanickingTreeWalkCallbackAborts(t *testing.T) {
	repo := createTestRepo(t)
	defer cleanupTestRepo(t, repo)

	_, treeID := seedTestRepo(t, repo)
	tree, err := repo.LookupTree(treeID)
	checkFatal(t, err)
	defer tree.Free()

	err = tree.Walk(func(root string, entry *TreeEntry) error {
		panic("treewalk panic for testing")
	})
	if err == nil {
		t.Fatal("expected Walk to fail when the callback panics, got nil")
	}
}

// TestPanickingDiffCallbackAborts covers the same guard on the diff callbacks,
// which libgit2 drives in a tighter loop.
func TestPanickingDiffCallbackAborts(t *testing.T) {
	repo := createTestRepo(t)
	defer cleanupTestRepo(t, repo)

	seedTestRepo(t, repo)
	_, _ = updateReadme(t, repo, "diff panic probe\n")

	diff, err := repo.DiffTreeToWorkdirWithIndex(nil, nil)
	checkFatal(t, err)
	defer diff.Free()

	err = diff.ForEach(func(file DiffDelta, progress float64) (DiffForEachHunkCallback, error) {
		panic("diff panic for testing")
	}, DiffDetailFiles)
	if err == nil {
		t.Fatal("expected ForEach to fail when the callback panics, got nil")
	}
}
