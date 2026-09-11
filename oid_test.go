package git

import (
	"testing"
)

const (
	testSHA1Hex  = "1385f264afb75a56a5bec74243be9b367ba4ca08"
	testSHA1Hex2 = "1385f264afb75a56a5bec74243be9b367ba4ca09"
)

func TestTypedAPIsRejectInvalidObjectIdType(t *testing.T) {
	invalid := ObjectIdType(255)
	if _, err := NewOdbWithOidType(invalid); !IsErrorCode(err, ErrorCodeInvalid) {
		t.Fatalf("NewOdbWithOidType error = %v, want ErrorCodeInvalid", err)
	}
	if _, err := NewIndexWithOidType(invalid); !IsErrorCode(err, ErrorCodeInvalid) {
		t.Fatalf("NewIndexWithOidType error = %v, want ErrorCodeInvalid", err)
	}
	if _, err := DiffFromBufferWithOidType(nil, nil, invalid); !IsErrorCode(err, ErrorCodeInvalid) {
		t.Fatalf("DiffFromBufferWithOidType error = %v, want ErrorCodeInvalid", err)
	}
}

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

func TestNewOidFromBytesRejectsShortInput(t *testing.T) {
	t.Parallel()
	if oid := NewOidFromBytes(make([]byte, 19)); oid != nil {
		t.Fatalf("NewOidFromBytes(short) = %v, want nil", oid)
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

func TestOidZeroValueNormalizesForC(t *testing.T) {
	t.Parallel()
	zero := &Oid{}
	if got := zero.String(); got != "0000000000000000000000000000000000000000" {
		t.Fatalf("zero oid String() = %q", got)
	}
	if zero.Type() != ObjectIdSHA1 {
		t.Fatalf("zero oid Type() = %v, want SHA1", zero.Type())
	}
	if n, err := ShortenOids([]*Oid{zero}, 7); err != nil || n != 7 {
		t.Fatalf("ShortenOids(zero) = %d, %v; want 7, nil", n, err)
	}
	if zero.kind != 0 {
		t.Fatalf("ShortenOids mutated zero-value oid: kind=%d", zero.kind)
	}
}

func TestOidZeroValueInputConversionDoesNotMutateMapKey(t *testing.T) {
	t.Parallel()
	zero := Oid{}
	keys := map[Oid]string{zero: "zero"}
	_ = zero.toC()
	if zero.kind != 0 {
		t.Fatalf("toC mutated zero-value oid kind=%d", zero.kind)
	}
	if got := keys[zero]; got != "zero" {
		t.Fatalf("zero-value oid map key became unstable: %q", got)
	}
}

func TestShortenOidsRejectsNil(t *testing.T) {
	t.Parallel()
	if _, err := ShortenOids([]*Oid{nil}, 7); !IsErrorCode(err, ErrorCodeInvalid) {
		t.Fatalf("ShortenOids(nil) error = %v, want ErrorCodeInvalid", err)
	}
}

func TestShortenOidsSHA256UsesAllHexDigits(t *testing.T) {
	t.Parallel()
	prefix := "0123456789abcdef0123456789abcdef01234567" // 40 hex chars
	a, err := NewOid(prefix + "000000000000000000000001")
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewOid(prefix + "000000000000000000000002")
	if err != nil {
		t.Fatal(err)
	}
	n, err := ShortenOids([]*Oid{a, b}, 7)
	if err != nil {
		t.Fatal(err)
	}
	if n != 64 {
		t.Fatalf("SHA256 ShortenOids() = %d, want 64", n)
	}
}

func TestShortenOidsRejectsInvalidInputs(t *testing.T) {
	t.Parallel()
	sha1, _ := NewOid(testSHA1Hex)
	sha256, _ := NewOid("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	for name, tc := range map[string]struct {
		ids    []*Oid
		minlen int
	}{
		"negative":  {[]*Oid{sha1}, -1},
		"too-long":  {[]*Oid{sha1}, 41},
		"mixed":     {[]*Oid{sha1, sha256}, 7},
		"duplicate": {[]*Oid{sha1, sha1.Copy()}, 7},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ShortenOids(tc.ids, tc.minlen); !IsErrorCode(err, ErrorCodeInvalid) {
				t.Fatalf("ShortenOids error = %v, want ErrorCodeInvalid", err)
			}
		})
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
