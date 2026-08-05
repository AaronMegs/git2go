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

# Keep deprecated declarations by default while tracking libgit2 main. CI also
# builds with BUILD_DEPRECATED_HARD=ON to prove the v36-pre bindings no longer
# depend on APIs hidden behind upstream's hard-deprecation gate.
BUILD_DEPRECATED_HARD="${BUILD_DEPRECATED_HARD-OFF}"
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

# Force the vendored libgit2 headers to win over stale system-installed
# libgit2 headers for every CMake target. libgit2's bundled xdiff target uses
# SYSTEM include directories; without a plain -I here, macOS can select an old
# /usr/local/include/git2 header and compile xdiff against a different
# git_allocator layout, leading to SIGBUS in xdl_prepare_env.
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
