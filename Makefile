TEST_ARGS ?= --count=1

default: test


generate: static-build/install/lib/libgit2.a
	go generate --tags "static" ./...

# System library
# ==============
# This uses whatever version of libgit2 can be found in the system.
test:
	go run script/check-MakeGitError-thread-lock.go
	go run script/check-binding-invariants.go
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
# dyld ignores LD_LIBRARY_PATH, so an explicit rpath must be baked into the test
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
			go run script/check-binding-invariants.go
	PKG_CONFIG_PATH=dynamic-build/install/lib/pkgconfig \
			LD_LIBRARY_PATH=$(DYNAMIC_LIBDIR) \
			CGO_LDFLAGS="-Wl,-rpath,$(DYNAMIC_LIBDIR)" \
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
	go run script/check-binding-invariants.go
	go test --tags "static" $(TEST_ARGS) ./...

# The reference-storage bindings are compiled in by default, because the pinned
# libgit2 baseline always carries reftable. This target proves the documented
# opt-out still builds and passes for a libgit2 that predates reftable.
test-static-no-reftable: static-build/install/lib/libgit2.a
	go test --tags "static,libgit2_no_reftable" $(TEST_ARGS) ./...

# The refdb backend bridge and the managed HTTP transport both hand memory
# between libgit2's threads and Go, so the race detector is part of the
# supported test matrix rather than an optional extra.
test-static-race: static-build/install/lib/libgit2.a
	go test --tags "static" -race $(TEST_ARGS) ./...

install-static: static-build/install/lib/libgit2.a
	go install --tags "static" ./...

# Offline / firewalled environments. A handful of tests clone and negotiate
# against github.com; they are not hermetic and fail with connection or TLS
# timeouts that say nothing about this repository. This target runs everything
# else deterministically.
test-static-offline: static-build/install/lib/libgit2.a
	GIT2GO_SKIP_NETWORK_TESTS=1 go test --tags "static" $(TEST_ARGS) ./...

# Static analysis that must stay clean.
.PHONY: lint
lint:
	test -z "$$(gofmt -l $$(git ls-files '*.go' | grep -v '^vendor/'))" || \
		(gofmt -l $$(git ls-files '*.go' | grep -v '^vendor/'); exit 1)
	go vet --tags "static" ./...
	go run script/check-binding-invariants.go

# Go dependencies and the libgit2 submodule intentionally share vendor/. The Go
# command recreates vendor/ from scratch, so restore the pinned C submodule after
# every dependency vendor update.
.PHONY: vendor-go
vendor-go:
	go mod vendor
	git submodule update --init --checkout vendor/libgit2
