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

### Experimental SHA256 support

SHA256 object IDs are still an experimental, opt-in feature in libgit2 (gated by
its `EXPERIMENTAL_SHA256` build option). git2go's SHA256 bindings are aligned
against the libgit2 `main` branch (the vendored submodule is pinned to a `main`
commit) so that this project is ready for a future libgit2 2.x / SHA256
promotion, while the default build remains compatible with libgit2 1.9.x.

- **Default (SHA1-only) build**: unchanged. Works against both libgit2 1.9.x and
  `main`; no extra tags needed. The `Oid` type and its API are byte-for-byte the
  same as before.
- **Experimental SHA256 build**: build libgit2 with `EXPERIMENTAL_SHA256=ON` and
  add the `git_experimental_sha256` tag. Because libgit2 refactored the
  experimental object-id API between 1.9.x (overloaded legacy functions) and
  `main` (new `git_oid_from_*` / `*_ext` functions) — and the two cannot be told
  apart by version number — you must additionally pass the `libgit2_next` tag
  when linking a `main`-based libgit2:

      # against libgit2 main (recommended, forward-looking)
      make test-static-sha256-next
      # equivalently:
      go test -tags "static git_experimental_sha256 libgit2_next" ./...

      # against a libgit2 1.9.x experimental build
      make test-static-sha256
      # equivalently:
      go test -tags "static git_experimental_sha256" ./...

  The build script (`script/build-libgit2.sh`, run via
  `EXPERIMENTAL_SHA256=ON ... --static`) probes the installed headers and prints
  which of the two `make` targets to use. See `docs/sha256-compat-design.md` for
  the full design, the upstream API divergence, and the SHA256-promotion
  convergence plan.

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

