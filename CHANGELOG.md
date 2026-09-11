# Changelog

## v36.0.0-pre.1 (unreleased)

First prerelease of the git2go v36 line. This line targets libgit2's promoted,
typed object-id ABI and supports SHA1 and SHA256 in the same build, together
with the reftable reference-storage backend. SHA1 and the `files` reference
backend remain the defaults.

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
- `NewOidFromBytes` now returns `nil` for an input shorter than 20 bytes instead
  of panicking. Callers that relied on the panic must check for `nil`.
- `Oid.Cmp` orders by object-id type and then raw bytes. `Oid.NCmp` follows
  libgit2's nibble-count semantics. `Oid{}` is normalized to the all-zero SHA1 id.
- `Repository.SetRefdb` now returns `error`. It previously discarded libgit2's
  return code, so a failed swap looked like success. Existing calls need a
  return-value check; this is a compile-time break, not a silent one.
- `ShortenOids` is reimplemented in Go and now returns an error for a nil oid,
  mixed object-id types, duplicate ids, or an out-of-range `minlen`. libgit2's
  `git_oid_shorten` only inspects the first 40 hex characters and therefore
  cannot distinguish SHA256 ids that diverge later; the previous binding could
  return a prefix length that does not actually disambiguate SHA256 input.
- `Refdb.SetBackend` no longer frees the backend when libgit2 rejects it, and
  clears the wrapper on success. Previously the failure path freed memory that
  libgit2 had not taken ownership of, and the success path left a Go finalizer
  attached to a pointer libgit2 owned — a double free on either branch. Code
  that called `Free` after a failed `SetBackend` must drop that call.
- The typed constructors (`NewOdbWithOidType`, `NewIndexWithOidType`,
  `OpenIndexWithOidType`, `DiffFromBufferWithOidType`, `NewIndexerForOidType`,
  `Odb.HashWithType`, `Odb.HashFileWithType`, `NewOdbBackendLooseWithOidType`,
  `NewOdbBackendOnePackWithOidType`) reject an unknown `ObjectIdType` with
  `ErrorCodeInvalid` instead of forwarding the raw value to C.
- The module import path changes from `/v35` to `/v36`.
- Removed `ConfigFindProgramdata`. Upstream marks `git_config_find_programdata`
  hard-deprecated because Git 2.24 and newer no longer support ProgramData
  configuration; it is omitted when libgit2 builds with `DEPRECATE_HARD=ON`.

See `docs/sha256-breaking-changes.md` for the complete compatibility and
migration guide, and `docs/sha256-reftable-integration-report.md` for the
integration and verification record.

### Added

- `ObjectIdType`, `ObjectIdSHA1`, `ObjectIdSHA256`, `Oid.Type()` and
  `Oid.Bytes()`.
- `ObjectIdTypeDefault`, plus `ObjectIdType.String()`, `.Size()` and
  `.HexSize()`.
- `InitRepositoryWithOidType`, `Repository.OidType` and `IsSha256Supported`.
- `Odb.HashWithType`, `Odb.HashFile`, `Odb.HashFileWithType`,
  `NewOdbWithOidType`, `NewOdbBackendLooseWithOidType`, and
  `NewOdbBackendOnePackWithOidType`.
- `NewIndexWithOidType`, `OpenIndexWithOidType`,
  `DiffFromBufferWithOidType`, and `NewIndexerForOidType`.
- **Reftable reference storage.** `RefdbType` (`RefdbDefault`, `RefdbFiles`,
  `RefdbReftable`), `Repository.NewRefdbBackendReftable`,
  `Repository.RefStorageFormat` and `IsReftableSupported`.
- **Extended repository initialization.** `InitRepositoryExt` with
  `RepositoryInitOptions`, `RepositoryInitFlag` and `RepositoryInitMode`,
  exposing init flags, mode, workdir/template overrides, the initial HEAD
  branch, an origin URL, and both `OidType` and `RefdbType`.
  `InitRepositoryWithOidType` is now a thin wrapper over it.
- **Reference database access.** `Repository.Refdb`, `Repository.OpenRefdb`,
  `Repository.NewRefdbBackendFs` and `Refdb.Compress`.
- **Go-defined refdb backends.** `NewRefdbBackendFromInterface` with
  `RefdbBackendInterface` and the optional `RefdbBackendInitializer`,
  `RefdbBackendCompressor` and `RefdbBackendLocker` capability interfaces,
  `RefdbBackendIterator`, `RefdbBackendInitFlag` and
  `RefdbBackendUnlockStatus`. A panic inside a Go callback is converted into a
  libgit2 error instead of unwinding across the cgo boundary.
- **Reflog API.** `Reflog`, `ReflogEntry`, `Repository.ReadReflog`,
  `Repository.RenameReflog` and `Repository.DeleteReflog`.
- **Reference transactions.** `Transaction`, `Repository.NewTransaction`,
  `LockRef`, `SetTarget`, `SetSymbolicTarget`, `SetReflog`, `Remove`, `Commit`
  and `Free`.
- `Version`, `Prerelease` and `VersionString` for runtime libgit2 version
  reporting, complementing the compile-time capability guard.
- `HandleList.GetOk`, so teardown callbacks can tolerate an already-released
  handle instead of panicking.
- End-to-end SHA256 coverage for repository creation, ODB read/write/hash,
  loose and one-pack backends, index/tree/commit lookup, patch parsing,
  packbuilder/indexer, and clone/fetch negotiation.
- A SHA1/SHA256 x files/reftable initialization matrix, reftable branch,
  symbolic-reference, reflog and reopen-persistence coverage, and refdb bridge
  lifecycle/concurrency/transaction coverage.
- `TestFilesTransactionMultipleRefsAtomic`, pinning down the multi-reference
  locking contract that reference transactions depend on.

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
- The compile-time version and ABI checks are centralized in
  `git2go_version_check.h`, included by all three `Build_*.go` variants instead
  of being duplicated in each. It additionally asserts the exact `git_oid`
  layout (`sizeof == 33`, `offsetof(type) == 0`, `offsetof(id) == 1`) and the
  `GIT_OID_SHA1`/`GIT_OID_SHA256` enum values, because `Oid` is converted to
  `git_oid` by an unchecked pointer cast.
- Updated the bundled libgit2 CMake invocation to upstream's current option
  names: `USE_THREADS`, `USE_REGEX`, `USE_AUTH_NTLM` and `USE_AUTH_NEGOTIATE`.
  The previous `THREADSAFE`, `REGEX_BACKEND`, `USE_NTLMCLIENT` and `USE_GSSAPI`
  names are no longer read by libgit2, so those settings were silently ignored.
  `BUILD_CLI=OFF` avoids building the unused command-line tool.
- `Oid.toC()` no longer mutates the receiver when normalizing a zero-valued
  `Oid`, so an `Oid` used as a map key or shared across goroutines stays stable.
  Output parameters now go through a separate internal `outC()`.
- All `Free` methods are idempotent, enforced by a build check.
- Added `script/check-binding-invariants.go`, a build-time check that enforces
  two conventions the compiler cannot express: every `//export`ed callback must
  defer a panic guard, and every `Free` that releases a C pointer must nil-check
  it first. Both rules support an explicit opt-out comment so exceptions are
  visible in review. It runs from `make lint` and every `make test-*` target,
  alongside the existing thread-lock check.
- Documented why reference transactions are unavailable on the reftable backend.
  It is an architectural mismatch, not a missing upstream feature: libgit2's
  refdb API locks one reference at a time, while reftable's atomicity unit is a
  single addition holding one lock on the whole reference database. Two
  workarounds were evaluated and rejected (a git2go-side batched commit cannot
  reliably detect the final unlock and would fake atomicity; calling reftable
  primitives directly is impossible because those symbols are hidden in the
  shared library). See `docs/reftable-transaction-research.md`.
- Replaced `reflect.SliceHeader` and manual `uintptr` pointer arithmetic with
  `unsafe.Slice` in `merge.go`, `rebase.go`, `remote.go` and `message.go`.
  `go vet` reports no `unsafe.Pointer`/`SliceHeader` misuse.
- CI now tests Go 1.18 and stable Go, uses current GitHub actions/runners, and
  includes a macOS bundled-static job. The hard-deprecation job additionally
  compiles with `CGO_CFLAGS=-DGIT_DEPRECATE_HARD`, proving git2go references no
  deprecated declaration rather than merely not linking one. New jobs cover the
  reftable/refdb suite under the race detector, the `libgit2_no_reftable`
  opt-out build, `gofmt`/`go vet`, and a negative test asserting the ABI guard
  rejects libgit2 v1.9.4.

### Security

- Fixed command injection in the Go-managed SSH transport. Repository paths are
  now parsed and validated before connecting, quoted as one POSIX shell argument,
  and rejected if they contain control characters or option-like forms.
- Added URL/SCP-like parsing and quoting tests for apostrophes, command
  substitution, shell metacharacters, percent encoding, IPv6 and control bytes.
- Hardened the prerelease tagging workflow. It previously ran the test suite in
  the same job that held a `contents: write` credential, so a compromised test
  could push to the repository. Validation and tagging are now separate jobs:
  the one that executes repository code has no write access, and the one that
  holds write access executes none. It also checks out without persisted
  credentials, refuses to tag a commit that is not an ancestor of `origin/main`,
  and re-verifies the resolved commit before creating the tag.
- Pinned every GitHub Action to a commit SHA instead of a floating major tag.

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
- Two `git_oidarray` leaks in `Repository.MergeBases` and
  `Repository.MergeBasesMany`, which never called `git_oidarray_dispose`.
- A data race in the Go-managed HTTP transport: `httpError` was written by the
  background request goroutine while `Write` read it without synchronization.
  `Write`/`Read` now access it under a mutex, and `sentRequest` is only written
  by the goroutine that initiates the request.
- `Repository.CreateCommitFromIds` no longer builds the parent array with raw
  `uintptr` arithmetic, checks `calloc` for failure, and rejects a nil parent
  instead of dereferencing it.
- Nil-input crashes in `MergeBases`, `MergeBaseMany`, `MergeBasesMany` and
  `MergeBaseOctopus`.
- `makeCStringsFromStrings` no longer ignores an allocation failure, and
  `freeStrarray` clears the array after freeing it.
- `make test-dynamic` on macOS. The bundled dylib records its install name as
  `@rpath/libgit2.*` and dyld ignores `LD_LIBRARY_PATH`, so the test binary
  failed to load the library; an explicit rpath is now passed via
  `CGO_LDFLAGS`.
- The release gate invoked the hard-deprecation build with
  `BUILD_DEPRECATED_HARD=ON`, but the build script reads `DEPRECATE_HARD`, so
  that audit was silently a no-op.
- Network-dependent tests are now gated behind `requiresNetwork`, which skips
  them under `go test -short` or when `GIT2GO_SKIP_NETWORK_TESTS` is set. Seven
  tests clone or negotiate against `github.com` and previously failed with
  connection/TLS timeouts on an offline host. `make test-static-offline` runs
  the deterministic subset.
- `Remote.Free` panicked with a nil pointer dereference when called twice,
  because it dereferenced `repo` unconditionally while the underlying `free`
  clears that field. It also panicked on the first call for the `Remote` handed
  to a `SmartSubtransportCallback`, which has no owning repository.
- Every `//export`ed callback now contains its own panics. A panic unwinding
  across the cgo boundary is undefined behaviour, since the intervening libgit2
  C frames carry no unwind information. Coverage went from 18 of 56 callbacks
  (only the refdb bridge) to all 56. Callbacks that report failure through a
  return code alone report `GIT_EUSER` rather than swallowing the panic, which
  would return 0 and let libgit2 continue on top of a callback that never
  completed.
- Every `Free` method that releases a C pointer is now idempotent: it nil-checks
  and clears the pointer before releasing it, so an explicit call combined with
  a deferred cleanup can no longer double free (28 methods fixed).

### Prerelease policy

Until libgit2 publishes a formal release containing the promoted typed object-id
ABI, v36 releases use tags `v36.0.0-pre.N`. The stable `v36.0.0` release remains
blocked on the upstream version, final version guards and official package
validation on supported platforms.
