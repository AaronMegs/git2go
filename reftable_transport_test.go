package git

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

// The other reftable tests drive the reference database directly. These cover
// the transport paths a caller actually uses — fetch and push — because a
// reference backend that only works through the local API is not usable in
// practice. They also cover SHA256 and reftable together, since that is the
// combination with no precedent in the released libgit2.

// newReftableRepoAt initializes a reftable repository under dir.
func newReftableRepoAt(t *testing.T, dir string, bare bool) *Repository {
	t.Helper()

	flags := RepositoryInitMkpath
	if bare {
		flags |= RepositoryInitBare
	}
	repo, err := InitRepositoryExt(dir, &RepositoryInitOptions{
		Flags:     flags,
		RefdbType: RefdbReftable,
	})
	checkFatal(t, err)
	return repo
}

// TestReftableFetchFromFilesRepo fetches from a files repository into a
// reftable one. CloneOptions cannot select a reference format, so
// init-then-fetch is the only way to populate a reftable repository from a
// remote, which makes this the path callers have to take.
func TestReftableFetchFromFilesRepo(t *testing.T) {
	if !IsReftableSupported() {
		t.Skip("linked libgit2 does not support reftable")
	}

	src := createTestRepo(t)
	defer cleanupTestRepo(t, src)
	seedTestRepo(t, src)
	branch := defaultBranchName(t, src)

	dir, err := ioutil.TempDir("", "git2go-reftable-fetch")
	checkFatal(t, err)
	defer os.RemoveAll(dir)

	dst := newReftableRepoAt(t, filepath.Join(dir, "dst"), false)
	defer dst.Free()

	remote, err := dst.Remotes.Create("origin", src.Path())
	checkFatal(t, err)
	defer remote.Free()

	if err := remote.Fetch([]string{"refs/heads/*:refs/remotes/origin/*"}, nil, ""); err != nil {
		t.Fatalf("fetch into a reftable repository failed: %v", err)
	}

	// The fetched reference must be readable, and the repository must still
	// report reftable rather than having silently fallen back to files.
	ref, err := dst.References.Lookup("refs/remotes/origin/" + branch)
	if err != nil {
		t.Fatalf("fetched reference not found: %v", err)
	}
	defer ref.Free()

	format, err := dst.RefStorageFormat()
	checkFatal(t, err)
	if format != RefdbReftable {
		t.Fatalf("reference format after fetch = %v, want reftable", format)
	}
}

// TestReftablePushToReftableBare pushes into a bare reftable repository, which
// exercises the backend's write path from the receive side.
func TestReftablePushToReftableBare(t *testing.T) {
	if !IsReftableSupported() {
		t.Skip("linked libgit2 does not support reftable")
	}

	dir, err := ioutil.TempDir("", "git2go-reftable-push")
	checkFatal(t, err)
	defer os.RemoveAll(dir)

	dst := newReftableRepoAt(t, filepath.Join(dir, "dst.git"), true)
	defer dst.Free()

	src := createTestRepo(t)
	defer cleanupTestRepo(t, src)
	seedTestRepo(t, src)
	branch := defaultBranchName(t, src)

	remote, err := src.Remotes.Create("reftable", dst.Path())
	checkFatal(t, err)
	defer remote.Free()

	if err := remote.Push([]string{"refs/heads/" + branch}, nil); err != nil {
		t.Fatalf("push into a reftable repository failed: %v", err)
	}

	ref, err := dst.References.Lookup("refs/heads/" + branch)
	if err != nil {
		t.Fatalf("pushed reference not found in the reftable repository: %v", err)
	}
	defer ref.Free()

	head, err := src.References.Lookup("refs/heads/" + branch)
	checkFatal(t, err)
	defer head.Free()
	if !ref.Target().Equal(head.Target()) {
		t.Fatalf("pushed target = %v, want %v", ref.Target(), head.Target())
	}
}

// TestSHA256ReftableFetch covers the combination that has no precedent in a
// released libgit2: SHA256 objects stored against a reftable reference
// backend, moved over the transport layer.
func TestSHA256ReftableFetch(t *testing.T) {
	if !IsReftableSupported() {
		t.Skip("linked libgit2 does not support reftable")
	}
	if !IsSha256Supported() {
		t.Skip("linked libgit2 does not support SHA256 object ids")
	}

	dir, err := ioutil.TempDir("", "git2go-sha256-reftable")
	checkFatal(t, err)
	defer os.RemoveAll(dir)

	src, err := InitRepositoryExt(filepath.Join(dir, "src"), &RepositoryInitOptions{
		Flags:     RepositoryInitMkpath,
		OidType:   ObjectIdSHA256,
		RefdbType: RefdbReftable,
	})
	checkFatal(t, err)
	defer src.Free()

	commitID := seedCommit(t, src)
	if got := len(commitID.String()); got != 64 {
		t.Fatalf("source commit hex size = %d, want 64", got)
	}

	dst, err := InitRepositoryExt(filepath.Join(dir, "dst.git"), &RepositoryInitOptions{
		Flags:     RepositoryInitMkpath | RepositoryInitBare,
		OidType:   ObjectIdSHA256,
		RefdbType: RefdbReftable,
	})
	checkFatal(t, err)
	defer dst.Free()

	remote, err := dst.Remotes.Create("origin", src.Path())
	checkFatal(t, err)
	defer remote.Free()

	if err := remote.Fetch([]string{"refs/heads/*:refs/remotes/origin/*"}, nil, ""); err != nil {
		t.Fatalf("fetch between SHA256 reftable repositories failed: %v", err)
	}

	// The object must arrive intact, and both formats must still be reported.
	commit, err := dst.LookupCommit(commitID)
	if err != nil {
		t.Fatalf("fetched SHA256 commit not found: %v", err)
	}
	defer commit.Free()

	if got := dst.OidType(); got != ObjectIdSHA256 {
		t.Errorf("destination oid type = %v, want SHA256", got)
	}
	format, err := dst.RefStorageFormat()
	checkFatal(t, err)
	if format != RefdbReftable {
		t.Errorf("destination reference format = %v, want reftable", format)
	}
}
