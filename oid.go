package git

/*
#include <git2.h>
*/
import "C"
import (
	"bytes"
	"runtime"
	"unsafe"
)

// ObjectIdType identifies the hash algorithm used to compute a Git object id.
// It mirrors libgit2's git_oid_t enum.
type ObjectIdType uint8

const (
	// ObjectIdSHA1 is the SHA1 object id type (20-byte / 40-hex). It is always
	// available.
	ObjectIdSHA1 ObjectIdType = 1

	// ObjectIdSHA256 is the SHA256 object id type (32-byte / 64-hex).
	ObjectIdSHA256 ObjectIdType = 2
)

// Cmp compares oid to oid2, ordering first by object id type and then by raw
// bytes. The return value follows the bytes.Compare convention (-1, 0 or +1)
// and mirrors libgit2's git_oid_cmp semantics.
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

// Equal reports whether the two object ids are identical.
//
// Two ids are equal when they have the same type and the same raw bytes. Note
// that this is deliberately not a plain struct comparison: in the SHA256-capable
// build an Oid carries a type byte, and a zero-valued Oid{} (type byte unset) is
// treated as SHA1 so that it still compares equal to the all-zeroes SHA1 id that
// libgit2 hands out.
func (oid *Oid) Equal(oid2 *Oid) bool {
	return oid.Type() == oid2.Type() && bytes.Equal(oid.Bytes(), oid2.Bytes())
}

// NCmp compares the first n hexadecimal characters (nibbles) of the two object
// ids, mirroring libgit2's git_oid_ncmp semantics. It returns 0 for a match and a
// non-zero value otherwise.
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

// ShortenOids returns the minimum length of the given object ids' hex
// representations that uniquely identifies all of them (clamped to at least
// minlen).
func ShortenOids(ids []*Oid, minlen int) (int, error) {
	shorten := C.git_oid_shorten_new(C.size_t(minlen))
	if shorten == nil {
		panic("Out of memory")
	}
	defer C.git_oid_shorten_free(shorten)

	var ret C.int

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	for _, id := range ids {
		hexLen := id.hexLen()
		buf := make([]byte, hexLen+1)
		C.git_oid_fmt((*C.char)(unsafe.Pointer(&buf[0])), id.toC())
		buf[hexLen] = 0
		ret = C.git_oid_shorten_add(shorten, (*C.char)(unsafe.Pointer(&buf[0])))
		if ret < 0 {
			return int(ret), MakeGitError(ret)
		}
	}
	runtime.KeepAlive(ids)
	return int(ret), nil
}
