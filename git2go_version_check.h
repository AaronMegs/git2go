/*
 * git2go libgit2 version guard (shared by all Build_*.go link configs).
 *
 * Rationale
 * ---------
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

/* Minimum supported libgit2 version for this git2go module line. */
#define GIT2GO_MIN_VER_MAJOR 1
#define GIT2GO_MIN_VER_MINOR 9

#if (LIBGIT2_VER_MAJOR < GIT2GO_MIN_VER_MAJOR) || \
    (LIBGIT2_VER_MAJOR == GIT2GO_MIN_VER_MAJOR && LIBGIT2_VER_MINOR < GIT2GO_MIN_VER_MINOR)
# error "Unsupported libgit2 version; this git2go (v35) requires libgit2 v1.9.0 or newer."
#endif

#endif /* GIT2GO_VERSION_CHECK_H */
