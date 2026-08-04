#!/bin/sh

# Since CMake cannot build the static and dynamic libraries in the same
# directory, this script helps build both static and dynamic versions of it and
# have the common flags in one place instead of split between two places.

set -e

usage() {
	echo "Usage: $0 <--dynamic|--static> [--system]">&2
	exit 1
}

if [ "$#" -eq "0" ]; then
	usage
fi

ROOT=${ROOT-"$(cd "$(dirname "$0")/.." && echo "${PWD}")"}
VENDORED_PATH=${VENDORED_PATH-"${ROOT}/vendor/libgit2"}
BUILD_SYSTEM=OFF

while [ $# -gt 0 ]; do
	case "$1" in
		--static)
			BUILD_PATH="${ROOT}/static-build"
			BUILD_SHARED_LIBS=OFF
			;;

		--dynamic)
			BUILD_PATH="${ROOT}/dynamic-build"
			BUILD_SHARED_LIBS=ON
			;;

		--system)
			BUILD_SYSTEM=ON
			;;

		*)
			usage
			;;
	esac
	shift
done

if [ -z "${BUILD_SHARED_LIBS}" ]; then
	usage
fi

if [ -n "${BUILD_LIBGIT_REF}" ]; then
	git -C "${VENDORED_PATH}" checkout "${BUILD_LIBGIT_REF}"
	trap "git submodule update --init" EXIT
fi

# DEPRECATE_HARD is kept OFF so that deprecated-but-still-valid symbols remain
# linkable. This matters because the vendored libgit2 baseline tracks the `main`
# branch, where some symbols git2go still binds (e.g. git_odb_hash) are marked
# deprecated; with DEPRECATE_HARD=ON those symbols would be omitted and the
# default (SHA1) static build would fail to link.
BUILD_DEPRECATED_HARD="OFF"
if [ "${BUILD_SYSTEM}" = "ON" ]; then
	BUILD_INSTALL_PREFIX=${SYSTEM_INSTALL_PREFIX-"/usr"}
	# Most system-wide installations won't intentionally omit deprecated symbols.
	BUILD_DEPRECATED_HARD="OFF"
else
	BUILD_INSTALL_PREFIX="${BUILD_PATH}/install"
	mkdir -p "${BUILD_PATH}/install/lib"
fi

USE_BUNDLED_ZLIB="ON"
if [ "${USE_CHROMIUM_ZLIB}" = "ON" ]; then
	USE_BUNDLED_ZLIB="Chromium"
fi

# Opt-in experimental SHA256 support. Set the EXPERIMENTAL_SHA256 environment
# variable to "ON" to build libgit2 with the experimental SHA256 object id
# support. This must be paired with building git2go using the
# `git_experimental_sha256` go build tag. Note that this changes the libgit2
# ABI (the git_oid struct grows a type byte and a 32-byte id), so the resulting
# library is NOT compatible with the default (SHA1-only) git2go build.
BUILD_EXPERIMENTAL_SHA256="OFF"
if [ "${EXPERIMENTAL_SHA256}" = "ON" ]; then
	BUILD_EXPERIMENTAL_SHA256="ON"
fi

# Force libgit2's own headers to win over any libgit2 headers already installed
# on the build machine.
#
# libgit2's bundled xdiff target declares its include directories with cmake's
# SYSTEM keyword, so they are passed as `-isystem` rather than `-I`. That puts
# them in the same search chain as the compiler's builtin system directories
# (notably /usr/local/include on macOS), so a stale libgit2 installed there can
# be picked up instead of the in-tree headers. When that happens the xdiff
# objects are compiled against a *different* `git_allocator` layout than the rest
# of the library (e.g. libgit2 1.5.x had9 function pointers, current libgit2 has
# 3), and the first xdiff call dereferences garbage as `gfree` and crashes with
# SIGBUS inside xdl_prepare_env.
#
# Passing the in-tree include directory as a plain `-I` via CMAKE_C_FLAGS makes it
# take precedence over every `-isystem`/builtin directory for all targets.
LIBGIT2_INTREE_INCLUDE="-I${VENDORED_PATH}/include"

mkdir -p "${BUILD_PATH}/build" &&
cd "${BUILD_PATH}/build" &&
cmake -DTHREADSAFE=ON \
      -DBUILD_TESTS=OFF \
      -DBUILD_SHARED_LIBS"=${BUILD_SHARED_LIBS}" \
      -DREGEX_BACKEND=builtin \
      -DUSE_BUNDLED_ZLIB="${USE_BUNDLED_ZLIB}" \
      -DUSE_HTTPS=OFF \
      -DUSE_SSH=OFF \
      -DUSE_NTLMCLIENT=OFF \
      -DUSE_GSSAPI=OFF \
      -DEXPERIMENTAL_SHA256="${BUILD_EXPERIMENTAL_SHA256}" \
      -DCMAKE_C_FLAGS="-fPIC ${LIBGIT2_INTREE_INCLUDE}" \
      -DCMAKE_BUILD_TYPE="RelWithDebInfo" \
      -DCMAKE_INSTALL_PREFIX="${BUILD_INSTALL_PREFIX}" \
      -DCMAKE_INSTALL_LIBDIR="lib" \
      -DDEPRECATE_HARD="${BUILD_DEPRECATED_HARD}" \
      "${VENDORED_PATH}"

build_and_install() {
	if which make nproc >/dev/null && [ -f Makefile ]; then
		# Make the build parallel if make is available and cmake used Makefiles.
		make "-j$(nproc --all)" install
	else
		cmake --build . --target install
	fi
}

build_and_install

# When building the experimental SHA256 library, libgit2 installs everything
# under an "-experimental" suffix (libgit2-experimental.a, git2-experimental.h,
# include/git2-experimental/, libgit2-experimental.pc) and does NOT install the
# usual git2.h / git2/ headers. The shared git2go cgo files include <git2.h> and
# <git2/sys/...>, so create compatibility symlinks pointing at the experimental
# headers. The experimental build wiring (Build_bundled_static_sha256.go) links
# the -experimental archive but includes <git2.h> via these symlinks.
if [ "${BUILD_EXPERIMENTAL_SHA256}" = "ON" ] && [ "${BUILD_SYSTEM}" != "ON" ]; then
	INCDIR="${BUILD_INSTALL_PREFIX}/include"
	if [ -e "${INCDIR}/git2-experimental.h" ] && [ ! -e "${INCDIR}/git2.h" ]; then
		ln -sf git2-experimental.h "${INCDIR}/git2.h"
	fi
	if [ -d "${INCDIR}/git2-experimental" ] && [ ! -e "${INCDIR}/git2" ]; then
		ln -sf git2-experimental "${INCDIR}/git2"
	fi

	# Detect which experimental object-id API shape the headers expose. libgit2
	# main renamed the typed entry points to git_oid_from_string/from_prefix/
	# from_raw (+ *_ext), while the 1.9.x release overloaded the legacy names.
	# Since main's version.h still reports 1.9.0 the two cannot be told apart by
	# version, so git2go selects the main shape via the `libgit2_next` build tag.
	OIDHDR=""
	if [ -f "${INCDIR}/git2/oid.h" ]; then
		OIDHDR="${INCDIR}/git2/oid.h"
	elif [ -f "${INCDIR}/git2-experimental/oid.h" ]; then
		OIDHDR="${INCDIR}/git2-experimental/oid.h"
	fi
	if [ -n "${OIDHDR}" ] && grep -q "git_oid_from_string" "${OIDHDR}"; then
		echo "NOTE: this libgit2 exposes the 'main' experimental oid API (git_oid_from_string/*_ext)." >&2
		echo "      Test git2go with: make test-static-sha256-next" >&2
		echo "      (equivalently: -tags \"static git_experimental_sha256 libgit2_next\")" >&2
	else
		echo "NOTE: this libgit2 exposes the 1.9.x experimental oid API (overloaded legacy names)." >&2
		echo "      Test git2go with: make test-static-sha256" >&2
		echo "      (equivalently: -tags \"static git_experimental_sha256\")" >&2
	fi
fi
