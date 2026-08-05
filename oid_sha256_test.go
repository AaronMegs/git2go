package git

import (
	"testing"
)

const (
	// 64-hex-character SHA256 object id.
	testSHA256Hex = "2b6d33aa83435db71d0e0e9244a0e2c8d5af5e3a9b1c2d3e4f5061728394a5b6"
)

func TestNewOidSHA256(t *testing.T) {
	t.Parallel()

	oid, err := NewOid(testSHA256Hex)
	if err != nil {
		t.Fatal(err)
	}
	if got := oid.String(); got != testSHA256Hex {
		t.Errorf("String() = %q, want %q", got, testSHA256Hex)
	}
	if got := oid.Type(); got != ObjectIdSHA256 {
		t.Errorf("Type() = %d, want %d (SHA256)", got, ObjectIdSHA256)
	}
	if got := len(oid.Bytes()); got != 32 {
		t.Errorf("len(Bytes()) = %d, want 32", got)
	}
}

func TestNewOidTypeInferenceFromLength(t *testing.T) {
	t.Parallel()

	sha1, err := NewOid(testSHA1Hex)
	if err != nil {
		t.Fatal(err)
	}
	if sha1.Type() != ObjectIdSHA1 {
		t.Errorf("40-hex string should infer SHA1, got %d", sha1.Type())
	}

	sha256, err := NewOid(testSHA256Hex)
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Type() != ObjectIdSHA256 {
		t.Errorf("64-hex string should infer SHA256, got %d", sha256.Type())
	}

	// Different oid types must never compare equal and comparison must order by
	// type before examining raw bytes, matching libgit2.
	if sha1.Equal(sha256) {
		t.Error("SHA1 and SHA256 oids must not be equal")
	}
	if got := sha1.Cmp(sha256); got >= 0 {
		t.Errorf("SHA1.Cmp(SHA256) = %d, want < 0", got)
	}
	if got := sha256.Cmp(sha1); got <= 0 {
		t.Errorf("SHA256.Cmp(SHA1) = %d, want > 0", got)
	}
	if got := sha1.NCmp(sha256, 1); got >= 0 {
		t.Errorf("SHA1.NCmp(SHA256, 1) = %d, want < 0", got)
	}

	// NCmp's length is measured in hex characters (nibbles), not bytes.
	nibbleA, err := NewOid("1200000000000000000000000000000000000000")
	if err != nil {
		t.Fatal(err)
	}
	nibbleB, err := NewOid("1f00000000000000000000000000000000000000")
	if err != nil {
		t.Fatal(err)
	}
	if got := nibbleA.NCmp(nibbleB, 1); got != 0 {
		t.Errorf("NCmp(..., 1) = %d, want 0 for matching high nibble", got)
	}
	if got := nibbleA.NCmp(nibbleB, 2); got == 0 {
		t.Error("NCmp(..., 2) = 0, want mismatch for different low nibble")
	}
}

func TestNewOidFromBytesWithTypeRoundTrip(t *testing.T) {
	t.Parallel()

	orig, err := NewOid(testSHA256Hex)
	if err != nil {
		t.Fatal(err)
	}
	rebuilt, err := NewOidFromBytesWithType(orig.Bytes(), ObjectIdSHA256)
	if err != nil {
		t.Fatal(err)
	}
	if !orig.Equal(rebuilt) {
		t.Errorf("round-trip mismatch: %s != %s", orig, rebuilt)
	}
	if rebuilt.Type() != ObjectIdSHA256 {
		t.Errorf("rebuilt type = %d, want SHA256", rebuilt.Type())
	}

	// Too few bytes for the requested type must be rejected, not silently
	// truncated or a panic.
	if _, err := NewOidFromBytesWithType(make([]byte, 20), ObjectIdSHA256); err == nil {
		t.Error("expected an error for a 20-byte SHA256 oid")
	}
}

func TestSHA256OidIsZero(t *testing.T) {
	t.Parallel()

	zero, err := NewOidFromBytesWithType(make([]byte, 32), ObjectIdSHA256)
	if err != nil {
		t.Fatal(err)
	}
	if !zero.IsZero() {
		t.Error("all-zero SHA256 oid should report IsZero")
	}

	nonZero, _ := NewOid(testSHA256Hex)
	if nonZero.IsZero() {
		t.Error("non-zero SHA256 oid reported as zero")
	}
}
