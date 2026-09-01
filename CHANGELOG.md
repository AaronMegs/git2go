# Changelog

## v36.0.0-pre.1 (unreleased)

First prerelease of the git2go v36 line. This line targets libgit2's promoted,
typed object-id ABI and supports SHA1 and SHA256 in the same build. SHA1 remains
the default object format.

The module path is now:

```text
github.com/libgit2/git2go/v36
```

### Breaking changes

- `Oid` is no longer a public `[20]byte` array. It is a comparable typed value
  that stores either a SHA1 or SHA256 id. Replace direct indexing/slicing and
  fixed-layout assumptions with `Type()`, `Bytes()` and `String()`.
- Old libgit2 1.9.x packages using the 20-byte `git_oid` ABI are not compatible
  with v36. Continue using git2go v35 for those libraries.
- Removed the `git_experimental_sha256` and `libgit2_next` build tags and the
  `EXPERIMENTAL_SHA256=ON` build mode. Regular `libgit2` / `libgit2.pc` now
  provides both formats.
- `NewOidFromBytesWithType` returns `(*Oid, error)` and validates type and input
  length.
- `Oid.Cmp` orders by object-id type and then raw bytes. `Oid.NCmp` follows
  libgit2's nibble-count semantics. `Oid{}` is normalized to the all-zero SHA1 id.
- The module import path changes from `/v35` to `/v36`.
- Removed `ConfigFindProgramdata`. Upstream marks `git_config_find_programdata`
  hard-deprecated because Git 2.24 and newer no longer support ProgramData
  configuration; it is omitted when libgit2 builds with `DEPRECATE_HARD=ON`.

See `docs/sha256-breaking-changes.md` for the complete compatibility and
migration guide.

### Added

- `ObjectIdType`, `ObjectIdSHA1`, `ObjectIdSHA256`, `Oid.Type()` and
  `Oid.Bytes()`.
- `InitRepositoryWithOidType` and `Repository.OidType`.
- `Odb.HashWithType`, `Odb.HashFile`, `Odb.HashFileWithType`,
  `NewOdbWithOidType`, `NewOdbBackendLooseWithOidType`, and
  `NewOdbBackendOnePackWithOidType`.
- `NewIndexWithOidType`, `OpenIndexWithOidType`,
  `DiffFromBufferWithOidType`, and `NewIndexerForOidType`.
- End-to-end SHA256 coverage for repository creation, ODB read/write/hash,
  loose and one-pack backends, index/tree/commit lookup, patch parsing,
  packbuilder/indexer, and clone/fetch negotiation.

### Changed

- Pinned vendored libgit2 to promoted-SHA256 main commit
  `0551dfd4ad989b6a3d5683c0d4cf326c6efef929`, which includes the
  CVE-2026-5917 SSH path-escaping fix and the zlib truncated-stream fix.
- Collapsed all experimental and legacy-overload C shims to libgit2's promoted
  typed/options/`_ext` API.
- Build-time capability guards now reject incompatible libgit2 headers even
  though the current upstream main version header still reports 1.9.0. They read
  `LIBGIT2_VERSION_MAJOR`/`LIBGIT2_VERSION_MINOR` rather than the hard-deprecated
  `LIBGIT2_VER_*` aliases, and fail loudly if neither macro is defined.
- Replaced every remaining hard-deprecated libgit2 alias with its promoted name:
  `git_credential_userpass_plaintext`, `git_credential_ssh_key`,
  `git_indexer_progress`, `git_remote_completion_t`, `GIT_REFERENCE_DIRECT`,
  `GIT_REFERENCE_SYMBOLIC` and `GIT_REVSPEC_*`.
- `ConfigLevelProgramdata` no longer binds a C enum value. Upstream removed this
  level from `git_config_level_t` and ignores it; the Go constant is retained for
  source compatibility and marked deprecated.
- Hardened bundled builds so in-tree libgit2 headers take priority over stale
  system headers for every CMake target.
- CI now tests Go 1.18 and stable Go, uses current GitHub actions/runners, and
  includes a macOS bundled-static job. The hard-deprecation job additionally
  compiles with `CGO_CFLAGS=-DGIT_DEPRECATE_HARD`, proving git2go references no
  deprecated declaration rather than merely not linking one.

### Security

- Fixed command injection in the Go-managed SSH transport. Repository paths are
  now parsed and validated before connecting, quoted as one POSIX shell argument,
  and rejected if they contain control characters or option-like forms.
- Added URL/SCP-like parsing and quoting tests for apostrophes, command
  substitution, shell metacharacters, percent encoding, IPv6 and control bytes.

### Fixed

- `UpdateTipsCallback` never firing. git2go always installs libgit2's
  `update_refs` callback, and libgit2 only falls back to the deprecated
  `update_tips` slot when `update_refs` is unset, so the Go callback was
  unreachable. It is now dispatched from `update_refs`, and
  `UpdateRefsCallback` still takes precedence when both are set.
- `git_apply` SIGBUS in self-built libgit2 caused by stale system libgit2 headers
  contaminating the bundled xdiff target.
- Rebase tests that assumed the initial branch was named `master`.
- Semantic comparison between `Oid{}` and libgit2's all-zero SHA1 id.
- Parallel config tests sharing and deleting the same temporary config file.
- Standalone SHA256 ODB/backend constructors now pass the object-id type through
  all corresponding libgit2 option structures.

### Prerelease policy

Until libgit2 publishes a formal release containing the promoted typed object-id
ABI, v36 releases use tags `v36.0.0-pre.N`. The stable `v36.0.0` release remains
blocked on the upstream version, final version guards and official package
validation on supported platforms.
