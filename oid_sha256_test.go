//go:build git_experimental_sha256
// +build git_experimental_sha256

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

	// Different oid types must never compare equal.
	if sha1.Equal(sha256) {
		t.Error("SHA1 and SHA256 oids must not be equal")
	}
}

func TestNewOidFromBytesWithTypeRoundTrip(t *testing.T) {
	t.Parallel()

	orig, err := NewOid(testSHA256Hex)
	if err != nil {
		t.Fatal(err)
	}
	rebuilt := NewOidFromBytesWithType(orig.Bytes(), ObjectIdSHA256)
	if !orig.Equal(rebuilt) {
		t.Errorf("round-trip mismatch: %s != %s", orig, rebuilt)
	}
	if rebuilt.Type() != ObjectIdSHA256 {
		t.Errorf("rebuilt type = %d, want SHA256", rebuilt.Type())
	}
}

func TestSHA256OidIsZero(t *testing.T) {
	t.Parallel()

	zero := NewOidFromBytesWithType(make([]byte, 32), ObjectIdSHA256)
	if !zero.IsZero() {
		t.Error("all-zero SHA256 oid should report IsZero")
	}

	nonZero, _ := NewOid(testSHA256Hex)
	if nonZero.IsZero() {
		t.Error("non-zero SHA256 oid reported as zero")
	}
}
