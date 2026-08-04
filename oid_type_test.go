package git

import (
	"os"
	"testing"
)

func TestOidTypeString(t *testing.T) {
	cases := map[OidType]string{
		OidTypeSha1:   "sha1",
		OidTypeSha256: "sha256",
		OidType(99):   "unknown",
	}
	for in, want := range cases {
		if got := in.String(); got != want {
			t.Errorf("OidType(%d).String() = %q, want %q", int(in), got, want)
		}
	}
}

func TestOidTypeSizes(t *testing.T) {
	cases := []struct {
		typ     OidType
		size    int
		hexSize int
	}{
		{OidTypeSha1, 20, 40},
		{OidTypeSha256, 32, 64},
		{OidType(99), 0, 0},
	}
	for _, c := range cases {
		if got := c.typ.Size(); got != c.size {
			t.Errorf("OidType(%d).Size() = %d, want %d", int(c.typ), got, c.size)
		}
		if got := c.typ.HexSize(); got != c.hexSize {
			t.Errorf("OidType(%d).HexSize() = %d, want %d", int(c.typ), got, c.hexSize)
		}
	}
}

// TestOidTypeMatchesOidWidth guards the invariant enforced at compile time by
// git2go_version_check.h: git2go's Oid is a 20-byte array, so it can only
// represent SHA1 object ids.
func TestOidTypeMatchesOidWidth(t *testing.T) {
	var oid Oid
	if len(oid) != OidTypeSha1.Size() {
		t.Fatalf("len(Oid) = %d but OidTypeSha1.Size() = %d; the Oid type and OidType are out of sync", len(oid), OidTypeSha1.Size())
	}
}

func TestRepositoryOidType(t *testing.T) {
	repo := createTestRepo(t)
	defer cleanupTestRepo(t, repo)

	// A repository created without the objectFormat extension uses SHA1.
	if got := repo.OidType(); got != OidTypeSha1 {
		t.Fatalf("repo.OidType() = %v (%d), want OidTypeSha1", got, int(got))
	}
}

// TestRepositoryOidTypeReftable verifies the object id type is reported
// correctly on a reftable repository too (reftable is orthogonal to the hash
// algorithm).
func TestRepositoryOidTypeReftable(t *testing.T) {
	repo, dir := newReftableRepo(t)
	defer os.RemoveAll(dir)
	defer repo.Free()

	if got := repo.OidType(); got != OidTypeSha1 {
		t.Fatalf("reftable repo OidType() = %v (%d), want OidTypeSha1", got, int(got))
	}
}

func TestIsSha256Supported(t *testing.T) {
	// Must agree with the feature flag and be stable across calls.
	want := Features()&FeatureSHA256 != 0
	if got := IsSha256Supported(); got != want {
		t.Fatalf("IsSha256Supported() = %v, want %v (from Features())", got, want)
	}
	if IsSha256Supported() != want {
		t.Fatal("IsSha256Supported() is not stable across calls")
	}
}
