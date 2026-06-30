//go:build !git_experimental_sha256
// +build !git_experimental_sha256

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
// In the default (SHA1-only) build this is a 20-byte array, preserving the
// historical representation and its value semantics (comparable with ==, usable
// as a map key, sliceable). For the SHA256-capable representation, build with
// the `git_experimental_sha256` tag (see oid_sha256.go).
type Oid [20]byte

func newOidFromC(coid *C.git_oid) *Oid {
	if coid == nil {
		return nil
	}

	oid := new(Oid)
	copy(oid[:], C.GoBytes(unsafe.Pointer(coid), C.GIT_OID_SHA1_SIZE))
	return oid
}

// NewOidFromBytes creates an Oid from a raw (binary) 20-byte SHA1 hash.
func NewOidFromBytes(b []byte) *Oid {
	oid := new(Oid)
	copy(oid[:], b[0:C.GIT_OID_SHA1_SIZE])
	return oid
}

func (oid *Oid) toC() *C.git_oid {
	return (*C.git_oid)(unsafe.Pointer(oid))
}

// Type returns the object id type. In the default build this is always
// ObjectIdSHA1.
func (oid *Oid) Type() ObjectIdType {
	return ObjectIdSHA1
}

// Bytes returns a copy of the raw (binary) bytes of the object id (20 bytes).
func (oid *Oid) Bytes() []byte {
	b := make([]byte, C.GIT_OID_SHA1_SIZE)
	copy(b, oid[:])
	return b
}

// hexLen returns the length of the hex string representation of the object id.
func (oid *Oid) hexLen() int {
	return int(C.GIT_OID_SHA1_HEXSIZE)
}

// NewOid parses a hex string into an Oid. In the default build only 40-character
// SHA1 hex strings are accepted.
func NewOid(s string) (*Oid, error) {
	if len(s) > int(C.GIT_OID_SHA1_HEXSIZE) {
		return nil, errors.New("string is too long for oid")
	}

	o := new(Oid)

	slice, err := hex.DecodeString(s)
	if err != nil {
		return nil, err
	}

	if len(slice) != int(C.GIT_OID_SHA1_SIZE) {
		return nil, &GitError{"invalid oid", ErrorClassNone, ErrorCodeGeneric}
	}

	copy(o[:], slice[:int(C.GIT_OID_SHA1_SIZE)])
	return o, nil
}

func (oid *Oid) String() string {
	return hex.EncodeToString(oid[:])
}

// IsZero reports whether the object id is the all-zeroes id.
func (oid *Oid) IsZero() bool {
	return *oid == Oid{}
}
