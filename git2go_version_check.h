/*
 * git2go libgit2 compatibility guards (shared by all Build_*.go link configs).
 *
 * Two independent checks live here:
 *
 *   1. Version guard - enforces the minimum supported libgit2 version.
 *   2. git_oid ABI guard - rejects a libgit2 whose git_oid is wider than
 *      git2go's Oid type (see the comment on that check for details).
 *
 * Version guard rationale
 * -----------------------
 * git2go's v35 module line targets the libgit2 1.9 API and the vendored
 * `main` (which reports LIBGIT2_VERSION "1.9.0" but carries newer features
 * such as reftable). The previous guard hard-pinned the minor version to
 * exactly 9 (`LIBGIT2_VER_MINOR > 9` was an error), which would break the
 * build the moment libgit2 tagged 1.10 or 2.0 even when the API stayed
 * compatible.
 *
 * This guard instead enforces a single lower bound (>= the minimum supported
 * version) and leaves the upper bound open, so newer libgit2 releases build
 * without touching three separate files. Bumping support to a new minimum is
 * now a one-line change here.
 *
 * If a future libgit2 makes a breaking API change, tighten this guard (e.g.
 * add an explicit upper bound for the affected range) in this one place.
 */

#ifndef GIT2GO_VERSION_CHECK_H
#define GIT2GO_VERSION_CHECK_H

#include <git2/version.h>
#include <git2/oid.h>

/* Minimum supported libgit2 version for this git2go module line. */
#define GIT2GO_MIN_VER_MAJOR 1
#define GIT2GO_MIN_VER_MINOR 9

#if (LIBGIT2_VER_MAJOR < GIT2GO_MIN_VER_MAJOR) || \
    (LIBGIT2_VER_MAJOR == GIT2GO_MIN_VER_MAJOR && LIBGIT2_VER_MINOR < GIT2GO_MIN_VER_MINOR)
# error "Unsupported libgit2 version; this git2go (v35) requires libgit2 v1.9.0 or newer."
#endif

/*
 * git_oid ABI guard.
 *
 * git2go represents an object id as a bare Go `type Oid [20]byte` and hands it
 * to libgit2 by casting the Go array straight to `*C.git_oid`
 * (see Oid.toC / newOidFromC in git.go). That cast is only sound while
 * libgit2's git_oid is exactly 20 bytes of raw id with no leading fields.
 *
 * Upstream libgit2 main has promoted SHA256 out of GIT_EXPERIMENTAL_SHA256,
 * which unconditionally changes git_oid to:
 *
 *     typedef struct git_oid {
 *             unsigned char type;                  // new leading field
 *             unsigned char id[GIT_OID_MAX_SIZE];  // 32, was 20
 *     } git_oid;
 *
 * Against such a libgit2 the cast would make libgit2 read and write past the
 * end of a 20-byte Go allocation (corrupting the Go heap) and would misalign
 * every id read back, because newOidFromC copies 20 bytes from offset 0 and
 * would capture the `type` byte plus a truncated id.
 *
 * GIT_OID_MAX_SIZE is a reliable discriminator: it expands to
 * GIT_OID_SHA1_SIZE (20) on a SHA1-only libgit2, and to GIT_OID_SHA256_SIZE
 * (32) once SHA256 is promoted.
 *
 * Fail loudly at compile time rather than corrupting memory at run time.
 * Lifting this guard requires reworking the Oid type first; the analysis and
 * the staged plan live in docs/reftable-longterm-research.md (section 3).
 */
#if GIT_OID_MAX_SIZE != 20
# error "Incompatible libgit2: git_oid is wider than git2go's Oid ([20]byte). This libgit2 enables SHA256 object ids, so git2go's Oid type must be reworked before it can be linked. See docs/reftable-longterm-research.md section 3."
#endif

#endif /* GIT2GO_VERSION_CHECK_H */
