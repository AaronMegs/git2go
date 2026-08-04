//go:build git_experimental_sha256
// +build git_experimental_sha256

package git

/*
#include <git2.h>
*/
import "C"
import (
	"encoding/hex"
	"errors"
	"unsafe"
)

// Oid represents the id for a Git object.
//
// In the experimental SHA256 build the layout mirrors libgit2's git_oid struct
// (a leading type byte followed by a GIT_OID_MAX_SIZE-byte id), so it can still
// be compared with == and used as a map key, and toC()/newOidFromC() remain
// zero-copy.
//
// NOTE: unlike the default build, Oid is NOT a [20]byte array here; do not index
// or slice it directly. Use Bytes(), String() and Type() instead.
type Oid struct {
	kind uint8    // mirrors C git_oid.type (git_oid_t)
	id   [32]byte // GIT_OID_MAX_SIZE under EXPERIMENTAL_SHA256
}

func init() {
	// Guard against any future divergence between the Go and C struct layouts:
	// the zero-copy toC()/newOidFromC() conversions rely on them matching.
	if unsafe.Sizeof(Oid{}) != unsafe.Sizeof(C.git_oid{}) {
		panic("git2go: Oid layout does not match C git_oid; rebuild libgit2 with -DEXPERIMENTAL_SHA256=ON")
	}
	if len(Oid{}.id) != int(C.GIT_OID_MAX_SIZE) {
		panic("git2go: unexpected GIT_OID_MAX_SIZE")
	}
}

func newOidFromC(coid *C.git_oid) *Oid {
	if coid == nil {
		return nil
	}

	oid := new(Oid)
	// The Go Oid layout matches the C git_oid layout (type byte + id), so a
	// single struct copy carries both the type and the raw id correctly.
	*oid = *(*Oid)(unsafe.Pointer(coid))
	return oid
}

// NewOidFromBytes creates a SHA1 Oid from raw (binary) bytes. To build a SHA256
// Oid use NewOidFromBytesWithType.
func NewOidFromBytes(b []byte) *Oid {
	oid := new(Oid)
	oid.kind = uint8(ObjectIdSHA1)
	copy(oid.id[:C.GIT_OID_SHA1_SIZE], b[:C.GIT_OID_SHA1_SIZE])
	return oid
}

// NewOidFromBytesWithType creates an Oid of the given type from raw (binary)
// bytes (20 bytes for SHA1, 32 bytes for SHA256).
func NewOidFromBytesWithType(b []byte, t ObjectIdType) (*Oid, error) {
	if t != ObjectIdSHA1 && t != ObjectIdSHA256 {
		return nil, &GitError{
			Message: "unknown object id type",
			Class:   ErrorClassInvalid,
			Code:    ErrorCodeInvalid,
		}
	}
	n := rawLenForType(t)
	if len(b) < n {
		return nil, &GitError{
			Message: "not enough bytes for the requested object id type",
			Class:   ErrorClassInvalid,
			Code:    ErrorCodeInvalid,
		}
	}

	oid := new(Oid)
	oid.kind = uint8(t)
	copy(oid.id[:n], b[:n])
	return oid, nil
}

func (oid *Oid) toC() *C.git_oid {
	return (*C.git_oid)(unsafe.Pointer(oid))
}

// Type returns the object id type (ObjectIdSHA1 or ObjectIdSHA256).
//
// A zero-valued Oid has no type byte set; it is reported as ObjectIdSHA1, which
// keeps `Oid{}` comparable with the all-zeroes SHA1 id that libgit2 returns (for
// example for a not-yet-known remote head).
func (oid *Oid) Type() ObjectIdType {
	if oid.kind == 0 {
		return ObjectIdSHA1
	}
	return ObjectIdType(oid.kind)
}

func rawLenForType(t ObjectIdType) int {
	if t == ObjectIdSHA256 {
		return int(C.GIT_OID_SHA256_SIZE)
	}
	return int(C.GIT_OID_SHA1_SIZE)
}

func (oid *Oid) rawLen() int {
	return rawLenForType(oid.Type())
}

// Bytes returns a copy of the raw (binary) bytes of the object id (20 bytes for
// SHA1, 32 bytes for SHA256).
func (oid *Oid) Bytes() []byte {
	n := oid.rawLen()
	b := make([]byte, n)
	copy(b, oid.id[:n])
	return b
}

// hexLen returns the length of the hex string representation of the object id.
func (oid *Oid) hexLen() int {
	return oid.rawLen() * 2
}

// NewOid parses a hex string into an Oid. Both 40-character SHA1 and
// 64-character SHA256 hex strings are accepted; the type is inferred from the
// string length.
func NewOid(s string) (*Oid, error) {
	if len(s) > int(C.GIT_OID_MAX_HEXSIZE) {
		return nil, errors.New("string is too long for oid")
	}

	slice, err := hex.DecodeString(s)
	if err != nil {
		return nil, err
	}

	var t ObjectIdType
	switch len(slice) {
	case int(C.GIT_OID_SHA1_SIZE):
		t = ObjectIdSHA1
	case int(C.GIT_OID_SHA256_SIZE):
		t = ObjectIdSHA256
	default:
		return nil, &GitError{"invalid oid", ErrorClassNone, ErrorCodeGeneric}
	}

	return NewOidFromBytesWithType(slice, t)
}

func (oid *Oid) String() string {
	return hex.EncodeToString(oid.Bytes())
}

// IsZero reports whether the raw id bytes are all zero (ignoring the type).
func (oid *Oid) IsZero() bool {
	for _, b := range oid.id[:oid.rawLen()] {
		if b != 0 {
			return false
		}
	}
	return true
}
