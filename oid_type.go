package git

/*
#include <git2.h>
*/
import "C"
import "runtime"

// OidType identifies the hash algorithm used for object ids in a repository.
//
// This mirrors the upstream libgit2 `git_oid_t` enumeration. The values match
// the C enum, but are declared as Go literals rather than referencing
// C.GIT_OID_* directly: on a libgit2 built without SHA256 support
// GIT_OID_SHA256 is not defined at all (it sits behind
// GIT_EXPERIMENTAL_SHA256 there), so referencing it would break the build.
//
// Note that git2go itself currently only supports SHA1 repositories: its Oid
// type is a 20-byte array, which cannot represent a 32-byte SHA256 id. A
// compile-time guard in git2go_version_check.h rejects any libgit2 whose
// git_oid is wider than that. OidType therefore exists to *report* and
// *detect* the repository format, not yet to create SHA256 repositories.
// See docs/reftable-longterm-research.md section 3 for the staged plan.
type OidType int

const (
	// OidTypeSha1 is the traditional 20-byte SHA1 object id.
	// Maps to GIT_OID_SHA1 (= 1) upstream, and is libgit2's default.
	OidTypeSha1 OidType = 1
	// OidTypeSha256 is the 32-byte SHA256 object id.
	// Maps to GIT_OID_SHA256 (= 2) upstream. Requires a libgit2 with SHA256
	// support; git2go cannot yet operate on such repositories (see OidType).
	OidTypeSha256 OidType = 2
)

// String returns the canonical `extensions.objectFormat` config value for the
// object id type, matching the tokens used by both git and libgit2
// ("sha1" / "sha256").
func (t OidType) String() string {
	switch t {
	case OidTypeSha1:
		return "sha1"
	case OidTypeSha256:
		return "sha256"
	default:
		return "unknown"
	}
}

// Size returns the length in bytes of a raw object id of this type,
// i.e. 20 for SHA1 and 32 for SHA256. It returns 0 for an unknown type.
func (t OidType) Size() int {
	switch t {
	case OidTypeSha1:
		return 20
	case OidTypeSha256:
		return 32
	default:
		return 0
	}
}

// HexSize returns the length in characters of a hex-formatted object id of
// this type, i.e. 40 for SHA1 and 64 for SHA256. It returns 0 for an unknown
// type.
func (t OidType) HexSize() int {
	return t.Size() * 2
}

// OidType reports the object id type (hash algorithm) used by the repository.
//
// A repository created without the `objectFormat` extension uses SHA1, so this
// normally returns OidTypeSha1.
//
// Wraps `git_repository_oid_type`.
func (v *Repository) OidType() OidType {
	ret := C.git_repository_oid_type(v.ptr)
	runtime.KeepAlive(v)
	return OidType(ret)
}

// IsSha256Supported reports whether the linked libgit2 was built with SHA256
// object id support.
//
// Unlike reftable, SHA256 does have a feature flag upstream, so this is a
// cheap check with no probing required.
//
// Note that even when this returns true, git2go cannot yet operate on SHA256
// repositories: its Oid type is 20 bytes wide. See OidType.
func IsSha256Supported() bool {
	return Features()&FeatureSHA256 != 0
}
