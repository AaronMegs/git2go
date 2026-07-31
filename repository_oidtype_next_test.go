//go:build git_experimental_sha256 && libgit2_next
// +build git_experimental_sha256,libgit2_next

package git

// Verifies Repository.OidType() against libgit2 main's git_repository_oid_type.
// Gated by the libgit2_next tag because that getter only exists on a main-based
// libgit2 (the pinned 1.9.x lacks the symbol; without this tag the shim reports
// SHA1 unconditionally, so the assertion would not be meaningful there).

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
