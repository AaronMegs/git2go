package git

/*
#include <git2.h>

extern int _go_git_odb_hash(git_oid *out, const void *data, size_t len, git_object_t obj_type, int oid_type);
extern int _go_git_repository_oid_type(git_repository *repo);
*/
import "C"
import (
	"os"
	"runtime"
	"unsafe"
)

// This file collects the public object-id-type-aware helpers. SHA1 and SHA256
// are both supported by the promoted libgit2 API; SHA1 remains the default.

// OidType reports the object id type (ObjectIdSHA1 or ObjectIdSHA256) that this
// repository uses for its objects.
//
// This maps to libgit2's git_repository_oid_type().
func (v *Repository) OidType() ObjectIdType {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	t := C._go_git_repository_oid_type(v.ptr)
	runtime.KeepAlive(v)
	return ObjectIdType(t)
}

// IsSha256Supported reports whether the linked libgit2 exposes SHA256 object id
// support.
//
// Unlike a version comparison this is a genuine capability probe: libgit2 can be
// compiled without a SHA256 provider even when the headers declare the typed
// object id API.
func IsSha256Supported() bool {
	return Features()&FeatureSHA256 != 0
}

// InitRepositoryWithOidType creates a new repository that stores objects using
// the given object id type (ObjectIdSHA1 or ObjectIdSHA256).
//
// It is a convenience wrapper around InitRepositoryExt so that object-format
// and reference-format initialization share a single code path.
func InitRepositoryWithOidType(path string, isBare bool, oidType ObjectIdType) (*Repository, error) {
	flags := RepositoryInitFlag(0)
	if isBare {
		flags |= RepositoryInitBare
	}
	return InitRepositoryExt(path, &RepositoryInitOptions{Flags: flags, OidType: oidType})
}

// NewOdbWithOidType creates a standalone object database with no backends using
// the specified object id type.
func NewOdbWithOidType(oidType ObjectIdType) (*Odb, error) {
	return newOdbWithOidType(oidType)
}

// NewOdbBackendOnePackWithOidType creates a backend for one packfile using the
// specified object id type.
func NewOdbBackendOnePackWithOidType(packfileIndexPath string, oidType ObjectIdType) (*OdbBackend, error) {
	return newOdbBackendOnePackWithOidType(packfileIndexPath, oidType)
}

// NewOdbBackendLooseWithOidType creates a loose-object backend using the
// specified object id type.
func NewOdbBackendLooseWithOidType(objectsDir string, compressionLevel int, doFsync bool, dirMode os.FileMode, fileMode os.FileMode, oidType ObjectIdType) (*OdbBackend, error) {
	return newOdbBackendLooseWithOidType(objectsDir, compressionLevel, doFsync, dirMode, fileMode, oidType)
}

// HashWithType determines the object id of a data buffer using the given object
// id type (ObjectIdSHA1 or ObjectIdSHA256).
func (v *Odb) HashWithType(data []byte, otype ObjectType, oidType ObjectIdType) (*Oid, error) {
	if err := validateObjectIdType(oidType); err != nil {
		return nil, err
	}
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

	ret := C._go_git_odb_hash(oid.outC(), unsafe.Pointer(&data[0]), size, C.git_object_t(otype), C.int(oidType))
	runtime.KeepAlive(data)
	runtime.KeepAlive(v)
	if ret < 0 {
		return nil, MakeGitError(ret)
	}
	return oid, nil
}

// HashFileWithType determines the object id of the raw contents of a file using
// the specified object id type. It does not apply repository filters (for
// example line-ending conversion); use repository-aware hashing when filters
// must be applied.
func (v *Odb) HashFileWithType(path string, otype ObjectType, oidType ObjectIdType) (*Oid, error) {
	if err := validateObjectIdType(oidType); err != nil {
		return nil, err
	}
	return v.hashFileWithOidType(path, otype, oidType)
}

// NewIndexerForOidType creates a new indexer instance for a packfile of the
// given object id type (ObjectIdSHA1 or ObjectIdSHA256).
func NewIndexerForOidType(packfilePath string, odb *Odb, oidType ObjectIdType, callback TransferProgressCallback) (*Indexer, error) {
	if err := validateObjectIdType(oidType); err != nil {
		return nil, err
	}
	return newIndexerWithOidType(packfilePath, odb, C.int(oidType), callback)
}

// NewIndexWithOidType allocates a new in-memory index holding object ids of the
// given type. It won't be associated with any file on the filesystem or
// repository.
func NewIndexWithOidType(oidType ObjectIdType) (*Index, error) {
	if err := validateObjectIdType(oidType); err != nil {
		return nil, err
	}
	return newIndexWithOidType(C.int(oidType))
}

// OpenIndexWithOidType creates a new index at the given path, holding object ids
// of the given type. If the file does not exist it will be created when Write()
// is called.
func OpenIndexWithOidType(path string, oidType ObjectIdType) (*Index, error) {
	if err := validateObjectIdType(oidType); err != nil {
		return nil, err
	}
	return openIndexWithOidType(path, C.int(oidType))
}

// DiffFromBufferWithOidType reads the contents of a git patch file that uses the
// given object id type into a Diff object.
func DiffFromBufferWithOidType(buffer []byte, repo *Repository, oidType ObjectIdType) (*Diff, error) {
	if err := validateObjectIdType(oidType); err != nil {
		return nil, err
	}
	return diffFromBufferWithOidType(buffer, repo, C.int(oidType))
}
