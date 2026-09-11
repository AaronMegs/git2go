git2go
======
[![GoDoc](https://godoc.org/github.com/libgit2/git2go?status.svg)](http://godoc.org/github.com/libgit2/git2go/v36) [![Build Status](https://travis-ci.org/libgit2/git2go.svg?branch=main)](https://travis-ci.org/libgit2/git2go)

Go bindings for [libgit2](http://libgit2.github.com/).

### Which Go version to use

Due to the fact that Go 1.11 module versions have semantic meaning and don't necessarily align with libgit2's release schedule, please consult the following table for a mapping between libgit2 and git2go module versions:

| libgit2 | git2go        |
|---------|---------------|
| main (SHA256 promoted; before the formal release) | v36-pre (`/v36`) |
| 1.9     | v35           |
| 1.5     | v34           |
| 1.3     | v33           |
| 1.2     | v32           |
| 1.1     | v31           |
| 1.0     | v30           |
| 0.99    | v29           |
| 0.28    | v28           |
| 0.27    | v27           |

You can import them in your project with the version's major number as a suffix. The promoted-SHA256 baseline uses the `/v36` module path. Before libgit2 publishes the corresponding formal release, git2go releases are named **v36-pre** and use valid Go prerelease tags such as `v36.0.0-pre.1`:

```sh
go get github.com/libgit2/git2go/v36@v36.0.0-pre.1
```
```go
import "github.com/libgit2/git2go/v36"
```

Users that must link libgit2 1.9.x should remain on `github.com/libgit2/git2go/v35`.

which will ensure there are no sudden changes to the API.

The `main` branch follows the tip of libgit2 itself (with some lag) and as such has no guarantees on the stability of libgit2's API. Thus this only supports statically linking against libgit2.

### Which branch to send Pull requests to

If there's something version-specific that you'd want to contribute to, you can send them to the `release-${MAJOR}.${MINOR}` branches, which follow libgit2's releases.

Installing
----------

This project wraps the functionality provided by libgit2. It thus needs it in order to perform the work.

This project wraps the functionality provided by libgit2. If you're using a versioned branch, install it to your system via your system's package manager and then install git2go.


### Versioned branch, dynamic linking

When linking dynamically against a released version of libgit2, install it via your system's package manager. CGo will take care of finding its pkg-config file and set up the linking. Import via Go modules, e.g. to work against libgit2 v1.2

```go
import "github.com/libgit2/git2go/v36"
```

### Versioned branch, static linking

Follow the instructions for [Versioned branch, dynamic linking](#versioned-branch-dynamic-linking), but pass the `-tags static,system_libgit2` flag to all `go` commands that build any binaries. For instance:

    go build -tags static,system_libgit2 github.com/my/project/...
    go test -tags static,system_libgit2 github.com/my/project/...
    go install -tags static,system_libgit2 github.com/my/project/...

### `main` branch, or vendored static linking

If using `main` or building a branch with the vendored libgit2 statically, we need to build libgit2 first. In order to build it, you need `cmake`, `pkg-config` and a C compiler. You will also need the development packages for OpenSSL (outside of Windows or macOS) and LibSSH2 installed if you want libgit2 to support HTTPS and SSH respectively. Note that even if libgit2 is included in the resulting binary, its dependencies will not be.

Run `go get -d github.com/libgit2/git2go` to download the code and go to your `$GOPATH/src/github.com/libgit2/git2go` directory. From there, we need to build the C code and put it into the resulting go binary.

    git submodule update --init # get libgit2
    make install-static

will compile libgit2, link it into git2go and install it. The `main` branch is set up to follow the specific libgit2 version that is vendored, so trying dynamic linking may or may not work depending on the exact versions involved.

In order to let Go pass the correct flags to `pkg-config`, `-tags static` needs to be passed to all `go` commands that build any binaries. For instance:

    go build -tags static github.com/my/project/...
    go test -tags static github.com/my/project/...
    go install -tags static github.com/my/project/...

One thing to take into account is that since Go expects the `pkg-config` file to be within the same directory where `make install-static` was called, so the `go.mod` file may need to have a [`replace` directive](https://github.com/golang/go/wiki/Modules#when-should-i-use-the-replace-directive) so that the correct setup is achieved. So if `git2go` is checked out at `$GOPATH/src/github.com/libgit2/git2go` and your project at `$GOPATH/src/github.com/my/project`, the `go.mod` file of `github.com/my/project` might need to have a line like

    replace github.com/libgit2/git2go/v36 => ../../libgit2/git2go

### SHA1 and SHA256 object IDs

The vendored libgit2 `main` baseline has promoted SHA256 to a regular feature.
Both SHA1 and SHA256 are supported in the same build; SHA1 remains the default.
No experimental CMake option or Go build tag is required.

`Oid` now mirrors libgit2's typed object-id layout and can hold either format.
Use `Oid.Type()` to inspect the format, `Bytes()` for the raw bytes, and
`String()` for the 40-hex (SHA1) or 64-hex (SHA256) representation. Do not index
or slice an `Oid` directly.

Repositories still default to SHA1:

    repo, err := git.InitRepository(path, false)

Create a SHA256 repository explicitly:

    repo, err := git.InitRepositoryWithOidType(path, false, git.ObjectIdSHA256)

The type-aware APIs (`NewOdbWithOidType`,
`NewOdbBackendLooseWithOidType`, `NewOdbBackendOnePackWithOidType`,
`HashWithType`, `HashFileWithType`, `NewIndexWithOidType`,
`OpenIndexWithOidType`, `DiffFromBufferWithOidType`, and
`NewIndexerForOidType`) are available in every build. `Odb.HashFile` follows the
ODB/repository object format, while `HashFileWithType` selects SHA1 or SHA256
explicitly; both hash raw file contents without repository filters. See
`docs/sha256-compat-design.md` for implementation details and
`docs/sha256-breaking-changes.md` for migration guidance.

### Reference storage: files and reftable

The same baseline also provides the reftable reference-storage backend. The
bindings for it are compiled in by default; `files` remains the default backend
for new repositories.

Create a reftable repository, optionally combined with SHA256:

    repo, err := git.InitRepositoryExt(path, &git.RepositoryInitOptions{
        Flags:     git.RepositoryInitMkpath,
        OidType:   git.ObjectIdSHA256, // optional; SHA1 is the default
        RefdbType: git.RefdbReftable,  // optional; files is the default
    })

`InitRepositoryExt` is the single entry point for both the object format and the
reference format, and also exposes the init flags, mode, workdir and template
overrides, the initial HEAD branch, and an origin URL.

Because libgit2 exposes no `GIT_FEATURE_REFTABLE` flag, support is detected by
probe rather than by version comparison:

    if git.IsReftableSupported() { /* ... */ }

To find out which backend an existing repository uses:

    format, err := repo.RefStorageFormat() // git.RefdbFiles or git.RefdbReftable

This mirrors libgit2's own rule: `extensions.refStorage` is only honoured when
`core.repositoryformatversion` is at least 1, so a version-0 repository that
declares `reftable` is still reported as `files`.

Related APIs: `Repository.Refdb`, `Repository.OpenRefdb`, `Refdb.Compress`,
`Repository.NewRefdbBackendFs`, `Repository.NewRefdbBackendReftable`, the
`Reflog` API, and `Repository.NewTransaction` for reference transactions.

Reference transactions are only available on the `files` backend. This is an
architectural mismatch rather than a missing upstream feature: libgit2's refdb
API locks one reference at a time, whereas reftable's atomicity unit is a single
"addition" that locks the entire reference database, so a per-reference mapping
breaks on the second reference of a transaction. `NewTransaction` still succeeds
on a reftable repository; `Transaction.LockRef` is where it reports the error.
See `docs/reftable-transaction-research.md` for the full analysis.

A single reference write is atomic on both backends, so grouping is only needed
when several references must change together. Branch on the backend up front:

    format, err := repo.RefStorageFormat()
    if err != nil {
        return err
    }
    if format == git.RefdbReftable {
        _, err = repo.References.Create(name, target, true, msg)
        return err
    }
    tx, err := repo.NewTransaction()
    // ... LockRef / SetTarget / Commit ...

Go programs can also supply their own reference database by implementing
`RefdbBackendInterface` and passing it to `NewRefdbBackendFromInterface`.

When linking against a libgit2 that predates reftable, build with the
`libgit2_no_reftable` tag to compile the files-only subset:

    go test --tags "static,libgit2_no_reftable" ./...

See `docs/sha256-reftable-integration-report.md` for the integration and
verification record.

### Contributing to the bindings

`docs/binding-design-guidelines.md` derives the conventions this binding layer
follows from git's and libgit2's own implementation choices: how C ownership
rules map onto Go lifetimes, why every cgo call is wrapped in
`runtime.LockOSThread` (libgit2 keeps its last error in thread-local storage),
how callback errors are round-tripped without losing the original Go `error`,
and how compile-time ABI assertions and runtime capability probes divide the
work of version compatibility. It ends with a checklist to run through when
adding a new binding.

`docs/capability-boundaries.md` records what this binding can and cannot do,
and why. It separates upstream limitations (reference transactions are
unavailable on reftable; no released libgit2 carries the promoted typed object
ids yet) from deliberate non-bindings (the `oid` comparison and formatting
helpers are implemented in Go) and from genuine gaps, and lists the outstanding
work split by whether it is breaking. Per-module coverage of the libgit2 public
API can be reproduced with:

    python3 script/audit-binding-coverage.py            # all modules
    python3 script/audit-binding-coverage.py oid odb    # unbound symbols

Parallelism and network operations
----------------------------------

libgit2 may use OpenSSL and LibSSH2 for performing encrypted network connections. For now, git2go asks libgit2 to set locking for OpenSSL. This makes HTTPS connections thread-safe, but it is fragile and will likely stop doing it soon. This may also make SSH connections thread-safe if your copy of libssh2 is linked against OpenSSL. Check libgit2's `THREADSAFE.md` for more information.

Running the tests
-----------------

For the stable version, `go test` will work as usual. For the `main` branch, similarly to installing, running the tests requires building a local libgit2 library, so the Makefile provides a wrapper that makes sure it's built

    make test-static

Alternatively, you can build the library manually first and then run the tests

    make install-static
    go test -v -tags static ./...

The supported test matrix also includes the race detector (the refdb backend
bridge and the managed HTTP transport both hand memory between libgit2 threads
and Go), the files-only degradation build, and the dynamic link:

    make test-static-race
    make test-static-no-reftable
    make test-dynamic

`make lint` runs `gofmt` and `go vet`, both of which must stay clean.

A few tests clone and negotiate against `github.com` and therefore need network
access. They are skipped under `go test -short` or when
`GIT2GO_SKIP_NETWORK_TESTS` is set, so an offline or firewalled host can run the
rest of the suite deterministically:

    make test-static-offline

License
-------

M to the I to the T. See the LICENSE file if you've never seen an MIT license before.

Authors
-------

- Carlos Martín (@carlosmn)
- Vicent Martí (@vmg)

