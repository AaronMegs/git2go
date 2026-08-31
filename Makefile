TEST_ARGS ?= --count=1

# vendor/ holds only the pinned libgit2 C submodule, never Go dependencies.
# Since Go 1.14 the toolchain auto-selects -mod=vendor whenever a vendor/
# directory exists, and then refuses to run because there is no Go vendor tree
# ("inconsistent vendoring"). Opt out explicitly for every recipe. readonly is
# preferred over mod so that go.mod/go.sum are never rewritten as a side effect.
export GOFLAGS ?= -mod=readonly

# The vendored libgit2 (used by the bundled static/dynamic builds) tracks
# libgit2 main, which includes reftable support. Enable the reftable bindings
# by default for these builds via the `libgit2_reftable` build tag. Override
# with `make ... REFTABLE_TAG=` to build the stable-compatible subset, or when
# building against a released libgit2 that lacks reftable.
REFTABLE_TAG ?= libgit2_reftable
STATIC_TAGS := static $(REFTABLE_TAG)
DYNAMIC_TAGS := $(REFTABLE_TAG)

default: test


generate: static-build/install/lib/libgit2.a
	go generate --tags "$(STATIC_TAGS)" ./...

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
#
# The bundled dylib on macOS records its install name as `@rpath/libgit2.*`, and
# dyld ignores LD_LIBRARY_PATH, so an explicit rpath is baked into the test
# binary. On Linux this is redundant but harmless, which keeps a single path.
DYNAMIC_LIBDIR := $(CURDIR)/dynamic-build/install/lib

.PHONY: build-libgit2-dynamic
build-libgit2-dynamic:
	./script/build-libgit2-dynamic.sh

dynamic-build/install/lib/libgit2.so:
	./script/build-libgit2-dynamic.sh

test-dynamic: dynamic-build/install/lib/libgit2.so
	PKG_CONFIG_PATH=dynamic-build/install/lib/pkgconfig \
			go run script/check-MakeGitError-thread-lock.go
	PKG_CONFIG_PATH=dynamic-build/install/lib/pkgconfig \
			LD_LIBRARY_PATH=$(DYNAMIC_LIBDIR) \
			CGO_LDFLAGS="-Wl,-rpath,$(DYNAMIC_LIBDIR)" \
			go test --tags "$(DYNAMIC_TAGS)" $(TEST_ARGS) ./...

install-dynamic: dynamic-build/install/lib/libgit2.so
	PKG_CONFIG_PATH=dynamic-build/install/lib/pkgconfig \
			go install --tags "$(DYNAMIC_TAGS)" ./...

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
	go test --tags "$(STATIC_TAGS)" $(TEST_ARGS) ./...

install-static: static-build/install/lib/libgit2.a
	go install --tags "$(STATIC_TAGS)" ./...
