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

# Keep deprecated declarations by default while tracking libgit2 main. Set
# DEPRECATE_HARD=ON in CI/audits to prove git2go no longer depends on APIs that
# upstream has hidden behind its hard-deprecation gate.
BUILD_DEPRECATED_HARD="${DEPRECATE_HARD-OFF}"
case "${BUILD_DEPRECATED_HARD}" in
	ON|OFF) ;;
	*)
		echo "DEPRECATE_HARD must be ON or OFF" >&2
		exit 1
		;;
esac

if [ "${BUILD_SYSTEM}" = "ON" ]; then
	BUILD_INSTALL_PREFIX=${SYSTEM_INSTALL_PREFIX-"/usr"}
else
	BUILD_INSTALL_PREFIX="${BUILD_PATH}/install"
	mkdir -p "${BUILD_PATH}/install/lib"
fi

USE_BUNDLED_ZLIB="ON"
if [ "${USE_CHROMIUM_ZLIB}" = "ON" ]; then
	USE_BUNDLED_ZLIB="Chromium"
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
cmake -DUSE_THREADS=ON \
      -DBUILD_TESTS=OFF \
      -DBUILD_CLI=OFF \
      -DBUILD_SHARED_LIBS"=${BUILD_SHARED_LIBS}" \
      -DUSE_REGEX=builtin \
      -DUSE_BUNDLED_ZLIB="${USE_BUNDLED_ZLIB}" \
      -DUSE_HTTPS=OFF \
      -DUSE_SSH=OFF \
      -DUSE_AUTH_NTLM=OFF \
      -DUSE_AUTH_NEGOTIATE=OFF \
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
