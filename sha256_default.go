//go:build !git_experimental_sha256
// +build !git_experimental_sha256

package git

/*
#include <git2.h>

// ABI guard (the mirror image of the one in Build_bundled_static_sha256.go).
//
// Without the `git_experimental_sha256` build tag, git2go represents an Oid as a
// bare 20-byte array, matching libgit2's default `git_oid`. A libgit2 built with
// -DEXPERIMENTAL_SHA256=ON however defines `git_oid` as { type byte; id[32] }.
// Linking that library into the default build would make newOidFromC() copy the
// type byte plus the first 19 id bytes (silently producing wrong object ids) and
// make toC() hand a 20-byte buffer to code that reads 33 bytes.
//
// Fail loudly at compile time instead: this combination must be built with the
// `git_experimental_sha256` tag.
#ifdef GIT_EXPERIMENTAL_SHA256
# error "this libgit2 was built with -DEXPERIMENTAL_SHA256=ON; rebuild git2go with -tags git_experimental_sha256 (see docs/sha256-compat-design.md)"
#endif
*/
import "C"
import "unsafe"

// This file mirrors the SHA256-aware API surface of sha256_api.go in the default
// (SHA1-only) build, so that the same source can be compiled with or without the
// `git_experimental_sha256` tag. Requests for SHA256 return an explicit error
// here rather than silently degrading to SHA1.

func init() {
	// Second line of defence behind the #error above, for setups where the
	// headers and the linked library could disagree.
	if unsafe.Sizeof(Oid{}) != unsafe.Sizeof(C.git_oid{}) {
		panic("git2go: Oid layout does not match C git_oid; if libgit2 was built with -DEXPERIMENTAL_SHA256=ON, rebuild git2go with -tags git_experimental_sha256")
	}
}

func errSHA256Unsupported(what string) error {
	return &GitError{
		Message: what + " requires SHA256 support; rebuild git2go with -tags git_experimental_sha256 against a libgit2 built with -DEXPERIMENTAL_SHA256=ON",
		Class:   ErrorClassInvalid,
		Code:    ErrorCodeInvalid,
	}
}

// OidType reports the object id type that this repository uses for its objects.
// In the default build this is always ObjectIdSHA1.
func (v *Repository) OidType() ObjectIdType {
	return ObjectIdSHA1
}

// NewOidFromBytesWithType creates an Oid of the given type from raw (binary)
// bytes. In the default build only ObjectIdSHA1 (20 bytes) is representable;
// ObjectIdSHA256 returns an error.
func NewOidFromBytesWithType(b []byte, t ObjectIdType) (*Oid, error) {
	if t != ObjectIdSHA1 {
		return nil, errSHA256Unsupported("NewOidFromBytesWithType")
	}
	if len(b) < int(C.GIT_OID_SHA1_SIZE) {
		return nil, &GitError{
			Message: "not enough bytes for a SHA1 object id",
			Class:   ErrorClassInvalid,
			Code:    ErrorCodeInvalid,
		}
	}
	return NewOidFromBytes(b), nil
}

// InitRepositoryWithOidType creates a new repository that stores objects using
// the given object id type. In the default build only ObjectIdSHA1 is supported.
func InitRepositoryWithOidType(path string, isBare bool, oidType ObjectIdType) (*Repository, error) {
	if oidType != ObjectIdSHA1 {
		return nil, errSHA256Unsupported("InitRepositoryWithOidType")
	}
	return InitRepository(path, isBare)
}

// HashWithType determines the object id of a data buffer using the given object
// id type. In the default build only ObjectIdSHA1 is supported.
func (v *Odb) HashWithType(data []byte, otype ObjectType, oidType ObjectIdType) (*Oid, error) {
	if oidType != ObjectIdSHA1 {
		return nil, errSHA256Unsupported("Odb.HashWithType")
	}
	return v.Hash(data, otype)
}

// NewIndexerForOidType creates a new indexer instance for a packfile of the
// given object id type. In the default build only ObjectIdSHA1 is supported.
func NewIndexerForOidType(packfilePath string, odb *Odb, oidType ObjectIdType, callback TransferProgressCallback) (*Indexer, error) {
	if oidType != ObjectIdSHA1 {
		return nil, errSHA256Unsupported("NewIndexerForOidType")
	}
	return newIndexerWithOidType(packfilePath, odb, 0, callback)
}
