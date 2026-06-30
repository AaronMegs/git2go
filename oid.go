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

	// ObjectIdSHA256 is the SHA256 object id type (32-byte / 64-hex). It is only
	// usable when git2go is built with the `git_experimental_sha256` build tag
	// against a libgit2 compiled with `-DEXPERIMENTAL_SHA256=ON`. In the default
	// build all object ids are ObjectIdSHA1.
	ObjectIdSHA256 ObjectIdType = 2
)

// Cmp compares the raw bytes of the two object ids, returning a value following
// the bytes.Compare convention (-1, 0 or +1).
func (oid *Oid) Cmp(oid2 *Oid) int {
	return bytes.Compare(oid.Bytes(), oid2.Bytes())
}

// Copy returns a copy of the object id.
func (oid *Oid) Copy() *Oid {
	ret := *oid
	return &ret
}

// Equal reports whether the two object ids are identical.
func (oid *Oid) Equal(oid2 *Oid) bool {
	return *oid == *oid2
}

// NCmp compares the first n bytes of the two object ids.
func (oid *Oid) NCmp(oid2 *Oid, n uint) int {
	a := oid.Bytes()
	b := oid2.Bytes()
	if int(n) > len(a) {
		n = uint(len(a))
	}
	if int(n) > len(b) {
		n = uint(len(b))
	}
	return bytes.Compare(a[:n], b[:n])
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
