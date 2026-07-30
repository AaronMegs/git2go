git2go
======
[![GoDoc](https://godoc.org/github.com/libgit2/git2go?status.svg)](http://godoc.org/github.com/libgit2/git2go/v35) [![Build Status](https://travis-ci.org/libgit2/git2go.svg?branch=main)](https://travis-ci.org/libgit2/git2go)

Go bindings for [libgit2](http://libgit2.github.com/).

### Which Go version to use

Due to the fact that Go 1.11 module versions have semantic meaning and don't necessarily align with libgit2's release schedule, please consult the following table for a mapping between libgit2 and git2go module versions:

| libgit2 | git2go        |
|---------|---------------|
| 1.9     | v35           |
| 1.5     | v34           |
| 1.3     | v33           |
| 1.2     | v32           |
| 1.1     | v31           |
| 1.0     | v30           |
| 0.99    | v29           |
| 0.28    | v28           |
| 0.27    | v27           |

You can import them in your project with the version's major number as a suffix. For example, if you have libgit2 v1.9 installed, you'd import git2go v35 with:

```sh
go get github.com/libgit2/git2go/v35
```
```go
import "github.com/libgit2/git2go/v35"
```

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
import "github.com/libgit2/git2go/v35"
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

    replace github.com/libgit2/git2go/v35 => ../../libgit2/git2go

Reference storage backends (reftable)
-------------------------------------

git2go can create and operate on repositories that use the **reftable**
reference storage backend in addition to the traditional `files` backend
(loose + packed refs).

> **Availability:** reftable is only present in libgit2's `main` branch
> (PR #7117 and later). It is **not** in any released libgit2 (v1.9.3 / v1.9.4
> do not have it). You therefore need the vendored/static `main` build
> (`make install-static` + `-tags static`); a system-installed released
> libgit2 will not support reftable.
>
> **Build tag:** the reftable bindings are gated behind the `libgit2_reftable`
> build tag. The bundled static/dynamic builds (`make install-static` /
> `test-static`) enable it by default. If you build against a **released**
> libgit2 (v1.9.x, which lacks the reftable C symbols), simply omit the tag:
> git2go still compiles, `IsReftableSupported()` returns `false`, and
> requesting `RefdbReftable` returns an error instead of failing to build.
> Override the default with `make ... REFTABLE_TAG=` to force the stable subset.

Detecting support and a repository's format at runtime (there is no
`GIT_FEATURE_REFTABLE` flag upstream):

```go
if git.IsReftableSupported() {
    // this libgit2 build can create reftable repositories
}

format, _ := repo.RefStorageFormat() // git.RefdbFiles or git.RefdbReftable
```

Initializing a repository with the reftable backend:

```go
repo, err := git.InitRepositoryExt("/path/to/repo", &git.RepositoryInitOptions{
    Flags:     git.RepositoryInitMkpath | git.RepositoryInitBare,
    RefdbType: git.RefdbReftable, // omit / RefdbDefault keeps the files backend
})
if err != nil {
    // On a libgit2 build without reftable support this fails; handle or fall
    // back to the default files backend.
}
```

Once created, all the usual reference APIs (`CreateBranch`, `LookupBranch`,
`References`, committing to `HEAD`, etc.) work unchanged — reftable is a
transparent backend. You can also ask the backend to compact/optimize its
storage:

```go
refdb, _ := repo.OpenRefdb()
defer refdb.Free()
_ = refdb.Compress() // files: pack refs; reftable: compact the reftable stack
```

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

License
-------

M to the I to the T. See the LICENSE file if you've never seen an MIT license before.

Authors
-------

- Carlos Martín (@carlosmn)
- Vicent Martí (@vmg)

