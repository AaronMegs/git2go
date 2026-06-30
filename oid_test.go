package git

import (
	"testing"
)

const (
	testSHA1Hex  = "1385f264afb75a56a5bec74243be9b367ba4ca08"
	testSHA1Hex2 = "1385f264afb75a56a5bec74243be9b367ba4ca09"
)

func TestNewOidSHA1(t *testing.T) {
	t.Parallel()

	oid, err := NewOid(testSHA1Hex)
	if err != nil {
		t.Fatal(err)
	}
	if got := oid.String(); got != testSHA1Hex {
		t.Errorf("String() = %q, want %q", got, testSHA1Hex)
	}
	if got := oid.Type(); got != ObjectIdSHA1 {
		t.Errorf("Type() = %d, want %d (SHA1)", got, ObjectIdSHA1)
	}
	if got := len(oid.Bytes()); got != 20 {
		t.Errorf("len(Bytes()) = %d, want 20", got)
	}
}

func TestNewOidInvalid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
	}{
		{"too long", testSHA1Hex + "00"},
		{"odd length", testSHA1Hex[:39]},
		{"non hex", "zzzz5f264afb75a56a5bec74243be9b367ba4ca08"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewOid(tc.in); err == nil {
				t.Errorf("NewOid(%q) expected error, got nil", tc.in)
			}
		})
	}
}

func TestNewOidFromBytesRoundTrip(t *testing.T) {
	t.Parallel()

	orig, err := NewOid(testSHA1Hex)
	if err != nil {
		t.Fatal(err)
	}
	rebuilt := NewOidFromBytes(orig.Bytes())
	if !orig.Equal(rebuilt) {
		t.Errorf("round-trip mismatch: %s != %s", orig, rebuilt)
	}
}

func TestOidCmpEqualZero(t *testing.T) {
	t.Parallel()

	a, _ := NewOid(testSHA1Hex)
	b, _ := NewOid(testSHA1Hex2)

	if a.Equal(b) {
		t.Error("expected distinct oids to be unequal")
	}
	if a.Cmp(b) >= 0 {
		t.Errorf("Cmp() = %d, want < 0", a.Cmp(b))
	}
	if a.Cmp(a.Copy()) != 0 {
		t.Error("Cmp of a copy should be 0")
	}
	if a.IsZero() {
		t.Error("non-zero oid reported as zero")
	}

	var zero Oid
	if !zero.IsZero() {
		t.Error("zero-value oid not reported as zero")
	}
}

func TestShortenOids(t *testing.T) {
	t.Parallel()

	a, _ := NewOid(testSHA1Hex)
	b, _ := NewOid(testSHA1Hex2)

	n, err := ShortenOids([]*Oid{a, b}, 7)
	if err != nil {
		t.Fatal(err)
	}
	// The two ids differ only in the last hex digit, so the minimum unique
	// length is the full 40 characters.
	if n != 40 {
		t.Errorf("ShortenOids() = %d, want 40", n)
	}
}
