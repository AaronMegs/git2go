TEST_ARGS ?= --count=1

default: test


generate: static-build/install/lib/libgit2.a
	go generate --tags "static" ./...

# System library
# ==============
# This uses whatever version of libgit2 can be found in the system.
test:
	go run script/check-MakeGitError-thread-lock.go
	go test $(TEST_ARGS) ./...

install:
	go install ./...

# Bundled dynamic library
# =======================
# In order to avoid having to manipulate `git_dynamic.go`, which would prevent
# the system-wide libgit2.so from being used in a sort of ergonomic way, this
# instead moves the complexity of overriding the paths so that the built
# libraries can be found by the build and tests.
.PHONY: build-libgit2-dynamic
build-libgit2-dynamic:
	./script/build-libgit2-dynamic.sh

dynamic-build/install/lib/libgit2.so:
	./script/build-libgit2-dynamic.sh

test-dynamic: dynamic-build/install/lib/libgit2.so
	PKG_CONFIG_PATH=dynamic-build/install/lib/pkgconfig \
			go run script/check-MakeGitError-thread-lock.go
	PKG_CONFIG_PATH=dynamic-build/install/lib/pkgconfig \
			LD_LIBRARY_PATH=dynamic-build/install/lib \
			go test $(TEST_ARGS) ./...

install-dynamic: dynamic-build/install/lib/libgit2.so
	PKG_CONFIG_PATH=dynamic-build/install/lib/pkgconfig \
			go install ./...

# Bundled static library
# ======================
# This is mostly used in tests, but can also be used to provide a
# statically-linked library with the bundled version of libgit2.
.PHONY: build-libgit2-static
build-libgit2-static:
	./script/build-libgit2-static.sh

static-build/install/lib/libgit2.a:
	./script/build-libgit2-static.sh

test-static: static-build/install/lib/libgit2.a
	go run script/check-MakeGitError-thread-lock.go
	go test --tags "static" $(TEST_ARGS) ./...

install-static: static-build/install/lib/libgit2.a
	go install --tags "static" ./...

# Experimental SHA256 static library
# ==================================
# Builds a libgit2 with -DEXPERIMENTAL_SHA256=ON (produces libgit2-experimental.a
# and an -experimental include layout; the build script adds git2.h/git2 compat
# symlinks). Use test-static-sha256 for a libgit2 with the 1.9.x experimental
# API, or test-static-sha256-next when linking a libgit2 main (which uses the
# git_oid_from_*/_ext API shape and requires the libgit2_next tag).
.PHONY: build-libgit2-static-sha256
build-libgit2-static-sha256:
	EXPERIMENTAL_SHA256=ON ./script/build-libgit2.sh --static

static-build/install/lib/libgit2-experimental.a:
	EXPERIMENTAL_SHA256=ON ./script/build-libgit2.sh --static

test-static-sha256: static-build/install/lib/libgit2-experimental.a
	go test --tags "static git_experimental_sha256" $(TEST_ARGS) ./...

test-static-sha256-next: static-build/install/lib/libgit2-experimental.a
	go test --tags "static git_experimental_sha256 libgit2_next" $(TEST_ARGS) ./...
