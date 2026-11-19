package git

/*
#include <git2.h>
#include <git2/common.h>
*/
import "C"

import "fmt"

type Feature int

const (
	// libgit2 was built with threading support
	FeatureThreads Feature = C.GIT_FEATURE_THREADS

	// libgit2 was built with HTTPS support built-in
	FeatureHTTPS Feature = C.GIT_FEATURE_HTTPS

	// libgit2 was build with SSH support built-in
	FeatureSSH Feature = C.GIT_FEATURE_SSH

	// libgit2 was built with nanosecond support for files
	FeatureNSec Feature = C.GIT_FEATURE_NSEC

	// HTTP parsing; always available
	FeatureHTTPParser Feature = C.GIT_FEATURE_HTTP_PARSER

	// Regular expression support; always available
	FeatureRegex Feature = C.GIT_FEATURE_REGEX

	// Internationalization support for filename translation
	FeatureI18N Feature = C.GIT_FEATURE_I18N

	// NTLM support over HTTPS
	FeatureAuthNTLM Feature = C.GIT_FEATURE_AUTH_NTLM

	// Kerberos (SPNEGO) authentication support over HTTPS
	FeatureAuthNegotiate Feature = C.GIT_FEATURE_AUTH_NEGOTIATE

	// zlib support; always available
	FeatureCompression Feature = C.GIT_FEATURE_COMPRESSION

	// SHA1 object support; always available
	FeatureSHA1 Feature = C.GIT_FEATURE_SHA1

	// SHA256 object support
	FeatureSHA256 Feature = C.GIT_FEATURE_SHA256
)

// Features returns a bit-flag of Feature values indicating which features the
// loaded libgit2 library has.
func Features() Feature {
	features := C.git_libgit2_features()

	return Feature(features)
}

// Version returns the major, minor and revision numbers of the libgit2 library
// that git2go is linked against, as reported at runtime by git_libgit2_version.
//
// This is the runtime counterpart to the compile-time capability guard in the
// Build_*.go files, which only enforces a lower bound. Prefer capability probes
// such as IsSha256Supported or IsReftableSupported over version comparisons.
func Version() (major, minor, patch int) {
	var cmajor, cminor, cpatch C.int
	C.git_libgit2_version(&cmajor, &cminor, &cpatch)
	return int(cmajor), int(cminor), int(cpatch)
}

// Prerelease returns the prerelease tag of the linked libgit2, or an empty
// string for a stable release.
//
// libgit2 built from an unreleased branch (for example the `main` build that
// carries promoted typed object ids and reftable support) reports a non-empty
// prerelease string, whereas tagged releases such as v1.9.4 return "".
// Combined with Version this lets callers distinguish a development build from
// a stable one.
func Prerelease() string {
	return C.GoString(C.git_libgit2_prerelease())
}

// VersionString returns a human-readable libgit2 version, for example "1.9.0"
// or "1.9.0-alpha" when a prerelease tag is present.
func VersionString() string {
	major, minor, patch := Version()
	base := fmt.Sprintf("%d.%d.%d", major, minor, patch)
	if pre := Prerelease(); pre != "" {
		return base + "-" + pre
	}
	return base
}
