package git

// Verifies Repository.OidType() against libgit2's git_repository_oid_type.

import (
	"os"
	"path/filepath"
	"testing"
)

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
func TestSHA256StandaloneOdbWithLooseBackend(t *testing.T) {
	objectsDir := filepath.Join(t.TempDir(), "objects")
	if err := os.MkdirAll(objectsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	odb, err := NewOdbWithOidType(ObjectIdSHA256)
	checkFatal(t, err)
	defer odb.Free()

	backend, err := NewOdbBackendLooseWithOidType(objectsDir, -1, false, 0, 0, ObjectIdSHA256)
	checkFatal(t, err)
	checkFatal(t, odb.AddBackend(backend, 1))

	data := []byte("standalone sha256 odb\n")
	written, err := odb.Write(data, ObjectBlob)
	checkFatal(t, err)
	if written.Type() != ObjectIdSHA256 || len(written.String()) != 64 {
		t.Fatalf("standalone ODB Write produced type=%d hexlen=%d, want SHA256/64", written.Type(), len(written.String()))
	}

	hashed, err := odb.Hash(data, ObjectBlob)
	checkFatal(t, err)
	if !hashed.Equal(written) {
		t.Errorf("standalone ODB Hash = %s, Write = %s", hashed, written)
	}

	obj, err := odb.Read(written)
	checkFatal(t, err)
	defer obj.Free()
	if got := obj.Data(); string(got) != string(data) {
		t.Errorf("standalone ODB Read = %q, want %q", got, data)
	}
}

func TestHashFileWithOidType(t *testing.T) {
	data := []byte{'r', 'a', 'w', 0, 'f', 'i', 'l', 'e', '\n'}
	path := filepath.Join(t.TempDir(), "blob with 'quote'.bin")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	repo := createTestRepoSHA256(t)
	defer cleanupTestRepo(t, repo)
	odb, err := repo.Odb()
	checkFatal(t, err)

	// HashFile follows the repository ODB's SHA256 format.
	fromFile, err := odb.HashFile(path, ObjectBlob)
	checkFatal(t, err)
	if fromFile.Type() != ObjectIdSHA256 || len(fromFile.String()) != 64 {
		t.Fatalf("HashFile type=%d hexlen=%d, want SHA256/64", fromFile.Type(), len(fromFile.String()))
	}

	explicit, err := odb.HashFileWithType(path, ObjectBlob, ObjectIdSHA256)
	checkFatal(t, err)
	fromBuffer, err := odb.HashWithType(data, ObjectBlob, ObjectIdSHA256)
	checkFatal(t, err)
	if !fromFile.Equal(explicit) || !fromFile.Equal(fromBuffer) {
		t.Fatalf("file hashes disagree: inferred=%s explicit=%s buffer=%s", fromFile, explicit, fromBuffer)
	}

	sha1File, err := odb.HashFileWithType(path, ObjectBlob, ObjectIdSHA1)
	checkFatal(t, err)
	sha1Buffer, err := odb.HashWithType(data, ObjectBlob, ObjectIdSHA1)
	checkFatal(t, err)
	if sha1File.Type() != ObjectIdSHA1 || len(sha1File.String()) != 40 || !sha1File.Equal(sha1Buffer) {
		t.Fatalf("SHA1 file hash=%s type=%d, want matching 40-hex SHA1", sha1File, sha1File.Type())
	}
	if sha1File.Equal(fromFile) {
		t.Fatal("SHA1 and SHA256 file hashes unexpectedly compare equal")
	}

	standalone, err := NewOdb()
	checkFatal(t, err)
	defer standalone.Free()
	standaloneHash, err := standalone.HashFile(path, ObjectBlob)
	checkFatal(t, err)
	if !standaloneHash.Equal(sha1File) {
		t.Errorf("standalone HashFile=%s, want default SHA1 %s", standaloneHash, sha1File)
	}

	if _, err := odb.HashFile(filepath.Join(t.TempDir(), "missing"), ObjectBlob); err == nil {
		t.Error("HashFile accepted a missing path")
	}
	if _, err := odb.HashFileWithType(path+"\x00suffix", ObjectBlob, ObjectIdSHA256); err == nil {
		t.Error("HashFileWithType accepted a path containing NUL")
	}
}

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
