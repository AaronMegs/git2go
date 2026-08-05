package git

/*
#include <git2.h>
*/
import "C"
import (
	"bytes"
	"sort"
)

// ObjectIdType identifies the hash algorithm used to compute a Git object id.
// It mirrors libgit2's git_oid_t enum.
type ObjectIdType uint8

const (
	// ObjectIdTypeDefault lets libgit2 choose its default object id type
	// (currently SHA1).
	ObjectIdTypeDefault ObjectIdType = 0

	// ObjectIdSHA1 is the SHA1 object id type (20-byte / 40-hex). It is always
	// available.
	ObjectIdSHA1 ObjectIdType = 1

	// ObjectIdSHA256 is the SHA256 object id type (32-byte / 64-hex).
	ObjectIdSHA256 ObjectIdType = 2
)

func validateObjectIdType(t ObjectIdType) error {
	switch t {
	case ObjectIdTypeDefault, ObjectIdSHA1, ObjectIdSHA256:
		return nil
	default:
		return &GitError{Message: "invalid object id type", Class: ErrorClassInvalid, Code: ErrorCodeInvalid}
	}
}

// String returns the canonical extensions.objectFormat token.
func (t ObjectIdType) String() string {
	switch t {
	case ObjectIdTypeDefault, ObjectIdSHA1:
		return "sha1"
	case ObjectIdSHA256:
		return "sha256"
	default:
		return "unknown"
	}
}

// Size returns the raw object id length in bytes, or zero for an unknown type.
func (t ObjectIdType) Size() int {
	switch t {
	case ObjectIdTypeDefault, ObjectIdSHA1:
		return int(C.GIT_OID_SHA1_SIZE)
	case ObjectIdSHA256:
		return int(C.GIT_OID_SHA256_SIZE)
	default:
		return 0
	}
}

// HexSize returns the hexadecimal object id length, or zero for an unknown type.
func (t ObjectIdType) HexSize() int {
	return t.Size() * 2
}

// Cmp compares oid to oid2, ordering first by object id type and then by raw
// bytes. The return value follows the bytes.Compare convention (-1, 0 or +1).
func (oid *Oid) Cmp(oid2 *Oid) int {
	if oid.Type() < oid2.Type() {
		return -1
	}
	if oid.Type() > oid2.Type() {
		return 1
	}
	return bytes.Compare(oid.Bytes(), oid2.Bytes())
}

// Copy returns a copy of the object id.
func (oid *Oid) Copy() *Oid {
	ret := *oid
	return &ret
}

// Equal reports whether the two object ids are identical. A zero-valued Oid
// is interpreted as the all-zero SHA1 id.
func (oid *Oid) Equal(oid2 *Oid) bool {
	return oid.Type() == oid2.Type() && bytes.Equal(oid.Bytes(), oid2.Bytes())
}

// NCmp compares the first n hexadecimal characters (nibbles) of two ids.
func (oid *Oid) NCmp(oid2 *Oid, n uint) int {
	if oid.Type() != oid2.Type() {
		return int(oid.Type()) - int(oid2.Type())
	}

	a := oid.Bytes()
	b := oid2.Bytes()
	maxHex := len(a) * 2
	if len(b)*2 < maxHex {
		maxHex = len(b) * 2
	}
	if int(n) > maxHex {
		n = uint(maxHex)
	}

	wholeBytes := int(n / 2)
	if !bytes.Equal(a[:wholeBytes], b[:wholeBytes]) {
		return 1
	}
	if n%2 != 0 && ((a[wholeBytes]^b[wholeBytes])&0xf0) != 0 {
		return 1
	}
	return 0
}

// ShortenOids returns the minimum hexadecimal prefix length that uniquely
// identifies all ids. All ids must use the same object-id type.
//
// This is implemented in Go because libgit2's git_oid_shorten currently only
// examines GIT_OID_SHA1_HEXSIZE (40) characters and therefore cannot safely
// distinguish SHA256 ids that differ after the 40th character.
func ShortenOids(ids []*Oid, minlen int) (int, error) {
	if minlen < 0 {
		return 0, &GitError{Message: "minimum oid prefix length is negative", Class: ErrorClassInvalid, Code: ErrorCodeInvalid}
	}
	if len(ids) == 0 {
		return minlen, nil
	}

	if ids[0] == nil {
		return 0, &GitError{Message: "cannot shorten a nil oid", Class: ErrorClassInvalid, Code: ErrorCodeInvalid}
	}
	typ := ids[0].Type()
	hexSize := typ.HexSize()
	if hexSize == 0 || minlen > hexSize {
		return 0, &GitError{Message: "invalid minimum oid prefix length", Class: ErrorClassInvalid, Code: ErrorCodeInvalid}
	}

	values := make([]string, len(ids))
	for i, id := range ids {
		if id == nil {
			return 0, &GitError{Message: "cannot shorten a nil oid", Class: ErrorClassInvalid, Code: ErrorCodeInvalid}
		}
		if id.Type() != typ {
			return 0, &GitError{Message: "cannot shorten mixed object id types", Class: ErrorClassInvalid, Code: ErrorCodeInvalid}
		}
		values[i] = id.String()
	}
	sort.Strings(values)
	for i := 1; i < len(values); i++ {
		if values[i] == values[i-1] {
			return 0, &GitError{Message: "cannot shorten duplicate object ids", Class: ErrorClassInvalid, Code: ErrorCodeInvalid}
		}
	}

	result := minlen
	if result < 1 {
		result = 1
	}
	for i := 1; i < len(values); i++ {
		common := commonHexPrefix(values[i-1], values[i]) + 1
		if common > result {
			result = common
		}
	}
	if result > hexSize {
		result = hexSize
	}
	return result, nil
}

func commonHexPrefix(a, b string) int {
	limit := len(a)
	if len(b) < limit {
		limit = len(b)
	}
	for i := 0; i < limit; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return limit
}
