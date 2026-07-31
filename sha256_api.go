//go:build git_experimental_sha256
// +build git_experimental_sha256

package git

/*
#include <git2.h>

extern int _go_git_repository_init(git_repository **out, const char *path, unsigned is_bare, int oid_type);
extern int _go_git_odb_hash(git_oid *out, const void *data, size_t len, git_object_t obj_type, int oid_type);
extern int _go_git_repository_oid_type(git_repository *repo);
*/
import "C"
import (
	"runtime"
	"unsafe"
)

// This file collects the public, SHA256-aware helpers that are only meaningful
// (and only compile) in the experimental SHA256 build. They are intentionally
// kept out of the default build so the default API surface does not expose
// object-id-type knobs that cannot actually take effect there.

// OidType reports the object id type (ObjectIdSHA1 or ObjectIdSHA256) that this
// repository uses for its objects.
//
// This maps to libgit2's git_repository_oid_type(), which exists on libgit2
// main. When built against a libgit2 that predates that getter (e.g. the pinned
// 1.9.x without the `libgit2_next` tag), the underlying shim reports SHA1, which
// is the only type such a libgit2 supports.
func (v *Repository) OidType() ObjectIdType {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	t := C._go_git_repository_oid_type(v.ptr)
	runtime.KeepAlive(v)
	return ObjectIdType(t)
}

// InitRepositoryWithOidType creates a new repository that stores objects using
// the given object id type (ObjectIdSHA1 or ObjectIdSHA256).
func InitRepositoryWithOidType(path string, isBare bool, oidType ObjectIdType) (*Repository, error) {
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	var ptr *C.git_repository
	ret := C._go_git_repository_init(&ptr, cpath, ucbool(isBare), C.int(oidType))
	if ret < 0 {
		return nil, MakeGitError(ret)
	}

	return newRepositoryFromC(ptr), nil
}

// HashWithType determines the object id of a data buffer using the given object
// id type (ObjectIdSHA1 or ObjectIdSHA256).
func (v *Odb) HashWithType(data []byte, otype ObjectType, oidType ObjectIdType) (*Oid, error) {
	oid := new(Oid)

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	var size C.size_t
	if len(data) > 0 {
		size = C.size_t(len(data))
	} else {
		data = []byte{0}
		size = C.size_t(0)
	}

	ret := C._go_git_odb_hash(oid.toC(), unsafe.Pointer(&data[0]), size, C.git_object_t(otype), C.int(oidType))
	runtime.KeepAlive(data)
	runtime.KeepAlive(v)
	if ret < 0 {
		return nil, MakeGitError(ret)
	}
	return oid, nil
}

// NewIndexerForOidType creates a new indexer instance for a packfile of the
// given object id type (ObjectIdSHA1 or ObjectIdSHA256).
func NewIndexerForOidType(packfilePath string, odb *Odb, oidType ObjectIdType, callback TransferProgressCallback) (*Indexer, error) {
	return newIndexerWithOidType(packfilePath, odb, C.int(oidType), callback)
}
