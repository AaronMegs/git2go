/*
 * git2go v36-pre / libgit2 main compatibility guard.
 *
 * The v36 line intentionally targets libgit2 main with promoted typed object
 * ids and reftable support. It is not ABI-compatible with released libgit2
 * v1.9.x, whose git_oid is the legacy 20-byte SHA1-only layout.
 */

#ifndef GIT2GO_VERSION_CHECK_H
#define GIT2GO_VERSION_CHECK_H

#include <stddef.h>
#include <git2/version.h>
#include <git2/oid.h>

/* Keep a useful version floor, while capability checks define the real ABI. */
#if LIBGIT2_VER_MAJOR < 1
# error "Unsupported libgit2 version; git2go v36-pre requires libgit2 main with promoted typed object ids and reftable support."
#endif

/* Promoted SHA256 ABI: one type byte plus a 32-byte maximum oid. */
#if GIT_OID_MAX_SIZE != 32
# error "Incompatible libgit2: git2go v36-pre requires promoted typed object ids (GIT_OID_MAX_SIZE == 32). Released v1.9.x is supported by the v35 line."
#endif

typedef char git2go_git_oid_sha1_value_must_be_1[(GIT_OID_SHA1 == 1) ? 1 : -1];
typedef char git2go_git_oid_sha256_value_must_be_2[(GIT_OID_SHA256 == 2) ? 1 : -1];
typedef char git2go_git_oid_size_must_be_33[(sizeof(git_oid) == 33) ? 1 : -1];
typedef char git2go_git_oid_type_offset_must_be_0[(offsetof(git_oid, type) == 0) ? 1 : -1];
typedef char git2go_git_oid_id_offset_must_be_1[(offsetof(git_oid, id) == 1) ? 1 : -1];

#endif /* GIT2GO_VERSION_CHECK_H */
