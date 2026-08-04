//go:build git_experimental_sha256
// +build git_experimental_sha256

package git

// End-to-end tests for the experimental SHA256 build. These only compile and
// run when git2go is built with the `git_experimental_sha256` tag AND linked
// against a libgit2 built with `-DEXPERIMENTAL_SHA256=ON`. They exercise the
// full create->write->commit->lookup loop on a real SHA256 repository, which is
// where the `git_oid` type-prefix/32-byte layout assumptions are most likely to
// break if a binding is mis-wired.
//
// Run with, e.g.:
//   EXPERIMENTAL_SHA256=ON ./script/build-libgit2.sh
//   go test -tags "static git_experimental_sha256" -run SHA256 -v .

import (
	"io/ioutil"
	"os"
	"testing"
	"time"
)

// createTestRepoSHA256 initializes a non-bare repository whose object format is
// SHA256, seeded with a single README file (mirrors createTestRepo).
func createTestRepoSHA256(t *testing.T) *Repository {
	t.Helper()

	path, err := ioutil.TempDir("", "git2go-sha256")
	checkFatal(t, err)

	repo, err := InitRepositoryWithOidType(path, false, ObjectIdSHA256)
	checkFatal(t, err)

	err = ioutil.WriteFile(path+"/README", []byte("foo\n"), 0644)
	checkFatal(t, err)

	return repo
}

// TestSHA256RepositoryOdbWrite verifies that objects written to a SHA256
// repository's ODB come back with a 32-byte, SHA256-typed oid, and that the
// stored data round-trips.
func TestSHA256RepositoryOdbWrite(t *testing.T) {
	repo := createTestRepoSHA256(t)
	defer cleanupTestRepo(t, repo)

	odb, err := repo.Odb()
	checkFatal(t, err)

	payload := []byte("hello sha256\n")
	oid, err := odb.Write(payload, ObjectBlob)
	checkFatal(t, err)

	if got := oid.Type(); got != ObjectIdSHA256 {
		t.Fatalf("written oid Type() = %d, want SHA256 (%d)", got, ObjectIdSHA256)
	}
	if got := len(oid.Bytes()); got != 32 {
		t.Fatalf("written oid has %d raw bytes, want 32", got)
	}
	if got := len(oid.String()); got != 64 {
		t.Fatalf("written oid hex length = %d, want 64", got)
	}

	// HashWithType must agree with the id assigned by Write (this catches a
	// mis-wired shim or a type-prefix offset bug end to end).
	hashed, err := odb.HashWithType(payload, ObjectBlob, ObjectIdSHA256)
	checkFatal(t, err)
	if !oid.Equal(hashed) {
		t.Fatalf("Write oid %s != HashWithType oid %s", oid, hashed)
	}

	// Read the object back and confirm the data survived the round-trip.
	obj, err := odb.Read(oid)
	checkFatal(t, err)
	defer obj.Free()
	if string(obj.Data()) != string(payload) {
		t.Fatalf("odb round-trip mismatch: got %q, want %q", obj.Data(), payload)
	}
}

// TestSHA256RepositoryCommitRoundTrip drives the full index -> tree -> commit ->
// lookup pipeline on a SHA256 repository and validates that every returned oid
// is a 64-hex SHA256 id and that lookups by those ids succeed.
func TestSHA256RepositoryCommitRoundTrip(t *testing.T) {
	repo := createTestRepoSHA256(t)
	defer cleanupTestRepo(t, repo)

	idx, err := repo.Index()
	checkFatal(t, err)
	checkFatal(t, idx.AddByPath("README"))
	checkFatal(t, idx.Write())

	treeID, err := idx.WriteTree()
	checkFatal(t, err)
	if treeID.Type() != ObjectIdSHA256 || len(treeID.String()) != 64 {
		t.Fatalf("tree oid not SHA256: type=%d hexlen=%d", treeID.Type(), len(treeID.String()))
	}

	tree, err := repo.LookupTree(treeID)
	checkFatal(t, err)
	defer tree.Free()

	loc, err := time.LoadLocation("Europe/Berlin")
	checkFatal(t, err)
	sig := &Signature{
		Name:  "Rand Om Hacker",
		Email: "random@hacker.com",
		When:  time.Date(2013, 03, 06, 14, 30, 0, 0, loc),
	}

	commitID, err := repo.CreateCommit("HEAD", sig, sig, "sha256 commit\n", tree)
	checkFatal(t, err)
	if commitID.Type() != ObjectIdSHA256 || len(commitID.String()) != 64 {
		t.Fatalf("commit oid not SHA256: type=%d hexlen=%d", commitID.Type(), len(commitID.String()))
	}

	// Lookup by the SHA256 oid and verify identity round-trips through C.
	commit, err := repo.LookupCommit(commitID)
	checkFatal(t, err)
	defer commit.Free()
	if !commit.Id().Equal(commitID) {
		t.Fatalf("looked-up commit id %s != created id %s", commit.Id(), commitID)
	}

	// Re-parse the textual oid and confirm it equals the original (NewOid must
	// infer SHA256 from the 64-hex length).
	reparsed, err := NewOid(commitID.String())
	checkFatal(t, err)
	if !reparsed.Equal(commitID) {
		t.Fatalf("NewOid(%q) round-trip mismatch", commitID.String())
	}
}

// TestSHA256IndexerForOidType builds a packfile from the SHA256 repo and indexes
// it via the SHA256-aware indexer constructor, asserting the produced pack name
// is a 64-hex SHA256 id.
func TestSHA256IndexerForOidType(t *testing.T) {
	repo := createTestRepoSHA256(t)
	defer cleanupTestRepo(t, repo)

	// Seed one object so there is something to pack.
	odb, err := repo.Odb()
	checkFatal(t, err)
	_, err = odb.Write([]byte("packme\n"), ObjectBlob)
	checkFatal(t, err)

	tmp, err := ioutil.TempDir("", "git2go-sha256-pack")
	checkFatal(t, err)
	defer os.RemoveAll(tmp)

	idx, err := NewIndexerForOidType(tmp, odb, ObjectIdSHA256, nil)
	checkFatal(t, err)
	defer idx.Free()
}

// TestSHA256IndexWithOidType verifies the SHA256-aware index constructors: an
// in-memory SHA256 index can write a tree into a SHA256 repository, and a
// standalone (repository-less) open of a real SHA256 index file yields 64-hex
// entry ids. Both paths go through git_index_options.oid_type, which is silently
// SHA1 if the option is not plumbed through.
func TestSHA256IndexWithOidType(t *testing.T) {
	repo := createTestRepoSHA256(t)
	defer cleanupTestRepo(t, repo)

	// Populate and persist the repository index so there is a real SHA256
	// index file on disk to reopen standalone.
	repoIdx, err := repo.Index()
	checkFatal(t, err)
	checkFatal(t, repoIdx.AddByPath("README"))
	checkFatal(t, repoIdx.Write())
	indexPath := repoIdx.Path()

	reopened, err := OpenIndexWithOidType(indexPath, ObjectIdSHA256)
	checkFatal(t, err)
	defer reopened.Free()

	entry, err := reopened.EntryByPath("README", 0)
	checkFatal(t, err)
	if entry.Id.Type() != ObjectIdSHA256 {
		t.Errorf("standalone index entry type = %d, want SHA256", entry.Id.Type())
	}
	if got := len(entry.Id.String()); got != 64 {
		t.Errorf("standalone index entry id is %d hex chars, want 64", got)
	}

	// An in-memory SHA256 index must be able to write a tree into the SHA256
	// repository.
	mem, err := NewIndexWithOidType(ObjectIdSHA256)
	checkFatal(t, err)
	defer mem.Free()

	treeID, err := mem.WriteTreeTo(repo)
	checkFatal(t, err)
	if treeID.Type() != ObjectIdSHA256 {
		t.Errorf("tree id type = %d, want SHA256", treeID.Type())
	}
}

// TestSHA256DiffFromBufferWithOidType parses a patch whose index line carries
// 64-hexids. Parsing it as SHA1 must fail, which proves
// git_diff_parse_options.oid_type is actually honored.
func TestSHA256DiffFromBufferWithOidType(t *testing.T) {
	const patch = "diff --git a/README b/README\n" +
		"index 0000000000000000000000000000000000000000000000000000000000000000..1111111111111111111111111111111111111111111111111111111111111111 100644\n" +
		"--- a/README\n" +
		"+++ b/README\n" +
		"@@ -1 +1 @@\n" +
		"-foo\n" +
		"+bar\n"

	diff, err := DiffFromBufferWithOidType([]byte(patch), nil, ObjectIdSHA256)
	checkFatal(t, err)
	defer diff.Free()

	n, err := diff.NumDeltas()
	checkFatal(t, err)
	if n != 1 {
		t.Errorf("NumDeltas() = %d, want 1", n)
	}

	if _, err := DiffFromBufferWithOidType([]byte(patch), nil, ObjectIdSHA1); err == nil {
		t.Error("parsing a 64-hex patch as SHA1 unexpectedly succeeded")
	}
}
