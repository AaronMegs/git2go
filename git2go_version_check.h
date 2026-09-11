/*
 * git2go v36-pre / libgit2 main compatibility guard.
 *
 * The v36 line targets libgit2 main, which has promoted typed object ids
 * (SHA1 + SHA256) out of the experimental gate and added the reftable
 * reference-storage backend. It is deliberately NOT ABI-compatible with the
 * released libgit2 v1.9.x, whose git_oid is the legacy 20-byte SHA1-only
 * layout; that library is supported by the git2go v35 line.
 *
 * This header is included by every Build_*.go variant so that the checks live
 * in exactly one place instead of being duplicated three times.
 */

#ifndef GIT2GO_VERSION_CHECK_H
#define GIT2GO_VERSION_CHECK_H

#include <stddef.h>
#include <git2/version.h>
#include <git2/oid.h>
#include <git2/repository.h>

/*
 * Use LIBGIT2_VERSION_*, never the LIBGIT2_VER_* aliases: upstream keeps the
 * latter in deprecated.h behind GIT_DEPRECATE_HARD, so a hard-deprecated build
 * leaves them undefined and the preprocessor would silently compare against 0.
 */
#if !defined(LIBGIT2_VERSION_MAJOR) || !defined(LIBGIT2_VERSION_MINOR)
# error "Cannot determine the libgit2 version; LIBGIT2_VERSION_MAJOR/LIBGIT2_VERSION_MINOR are not defined by the libgit2 headers being used"
#endif

#if LIBGIT2_VERSION_MAJOR < 1
# error "Unsupported libgit2 version; git2go v36-pre requires libgit2 main with promoted typed object ids and reftable support."
#endif

/*
 * Capability checks define the real ABI contract; the version numbers above are
 * only a floor, because libgit2 main still reports 1.9.x.
 */
#if !defined(GIT_OID_SHA256_SIZE) || !defined(GIT_OBJECT_ID_OPTIONS_VERSION) || !defined(GIT_INDEX_OPTIONS_VERSION) || !defined(GIT_DIFF_PARSE_OPTIONS_VERSION)
# error "This git2go requires libgit2's promoted typed object-id API (SHA256 plus object-id/index/diff options); use the pinned vendored baseline or a compatible release"
#endif

/* Promoted SHA256 ABI: one type byte plus a 32-byte maximum oid. */
#if GIT_OID_MAX_SIZE != 32
# error "Incompatible libgit2: git2go v36-pre requires promoted typed object ids (GIT_OID_MAX_SIZE == 32). Released v1.9.x is supported by the v35 line."
#endif

/*
 * The Go Oid struct mirrors git_oid field-for-field and is converted with an
 * unchecked pointer cast, so any drift in size, field order or enum values
 * must break the build rather than corrupt object ids at runtime.
 */
typedef char git2go_git_oid_sha1_value_must_be_1[(GIT_OID_SHA1 == 1) ? 1 : -1];
typedef char git2go_git_oid_sha256_value_must_be_2[(GIT_OID_SHA256 == 2) ? 1 : -1];
typedef char git2go_git_oid_size_must_be_33[(sizeof(git_oid) == 33) ? 1 : -1];
typedef char git2go_git_oid_type_offset_must_be_0[(offsetof(git_oid, type) == 0) ? 1 : -1];
typedef char git2go_git_oid_id_offset_must_be_1[(offsetof(git_oid, id) == 1) ? 1 : -1];

#endif /* GIT2GO_VERSION_CHECK_H */
