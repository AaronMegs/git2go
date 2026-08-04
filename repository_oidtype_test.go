package git

// Verifies Repository.OidType() against libgit2's git_repository_oid_type.

import "testing"

func TestSHA256RepositoryOidType(t *testing.T) {
	repo := createTestRepoSHA256(t)
	defer cleanupTestRepo(t, repo)

	if got := repo.OidType(); got != ObjectIdSHA256 {
		t.Fatalf("Repository.OidType() = %d, want SHA256 (%d)", got, ObjectIdSHA256)
	}

	// A default (SHA1) repository must report SHA1.
	sha1Repo := createTestRepo(t)
	defer cleanupTestRepo(t, sha1Repo)
	if got := sha1Repo.OidType(); got != ObjectIdSHA1 {
		t.Fatalf("default repo OidType() = %d, want SHA1 (%d)", got, ObjectIdSHA1)
	}
}

// TestSHA256OdbHashFollowsRepository verifies that an Odb obtained from a
// repository hashes with that repository's object format, i.e. Odb.Hash on a
// SHA256 repository yields a SHA256 id matching what Write() stores, while a
// standalone Odb keeps hashing as SHA1.
func TestSHA256OdbHashFollowsRepository(t *testing.T) {
	repo := createTestRepoSHA256(t)
	defer cleanupTestRepo(t, repo)

	odb, err := repo.Odb()
	checkFatal(t, err)

	data := []byte("hash me\n")

	hashed, err := odb.Hash(data, ObjectBlob)
	checkFatal(t, err)
	if hashed.Type() != ObjectIdSHA256 {
		t.Errorf("Odb.Hash type = %d, want SHA256 (%d)", hashed.Type(), ObjectIdSHA256)
	}
	if got := len(hashed.String()); got != 64 {
		t.Errorf("Odb.Hash produced %d hex chars, want 64", got)
	}

	written, err := odb.Write(data, ObjectBlob)
	checkFatal(t, err)
	if !hashed.Equal(written) {
		t.Errorf("Odb.Hash = %s but Write = %s", hashed, written)
	}

	// A standalone Odb has no repository to follow and must stay on the libgit2
	// default (SHA1).
	standalone, err := NewOdb()
	checkFatal(t, err)
	defer standalone.Free()

	sha1Hashed, err := standalone.Hash(data, ObjectBlob)
	checkFatal(t, err)
	if sha1Hashed.Type() != ObjectIdSHA1 {
		t.Errorf("standalone Odb.Hash type = %d, want SHA1 (%d)", sha1Hashed.Type(), ObjectIdSHA1)
	}
}
