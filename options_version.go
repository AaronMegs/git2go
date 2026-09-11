package git

/*
#include <git2.h>
*/
import "C"

import "fmt"

// Options structs in libgit2 are versioned: `git_*_options_init` refuses a
// version it does not recognize. The version git2go passes is a constant baked
// in at compile time from the libgit2 headers, so a dynamically linked build
// can end up passing a version that the loaded library predates.
//
// That skew is a global, static property of the build — it cannot vary between
// individual calls — so it is checked once here rather than at each of the
// ~30 call sites. Checking it at startup also reports the problem before any
// repository is touched, instead of surfacing as a puzzling failure from an
// unrelated operation later on.
//
// Failing loudly is deliberate: an options struct that libgit2 declined to
// initialize is left holding whatever was on the stack, and passing that to
// libgit2 is undefined behaviour. This mirrors the existing checks for missing
// thread support (git.go) and for a mismatched git_oid layout (oid_typed.go).
func checkOptionsVersions() error {
	// One representative per options struct that git2go initializes. Each entry
	// calls the real initializer against a zeroed struct, which is exactly what
	// the populate helpers do.
	checks := []struct {
		name string
		init func() C.int
	}{
		{"git_checkout_options", func() C.int {
			var o C.git_checkout_options
			return C.git_checkout_options_init(&o, C.GIT_CHECKOUT_OPTIONS_VERSION)
		}},
		{"git_clone_options", func() C.int {
			var o C.git_clone_options
			return C.git_clone_options_init(&o, C.GIT_CLONE_OPTIONS_VERSION)
		}},
		{"git_diff_options", func() C.int {
			var o C.git_diff_options
			return C.git_diff_options_init(&o, C.GIT_DIFF_OPTIONS_VERSION)
		}},
		{"git_diff_find_options", func() C.int {
			var o C.git_diff_find_options
			return C.git_diff_find_options_init(&o, C.GIT_DIFF_FIND_OPTIONS_VERSION)
		}},
		{"git_apply_options", func() C.int {
			var o C.git_apply_options
			return C.git_apply_options_init(&o, C.GIT_APPLY_OPTIONS_VERSION)
		}},
		{"git_merge_options", func() C.int {
			var o C.git_merge_options
			return C.git_merge_options_init(&o, C.GIT_MERGE_OPTIONS_VERSION)
		}},
		{"git_merge_file_options", func() C.int {
			var o C.git_merge_file_options
			return C.git_merge_file_options_init(&o, C.GIT_MERGE_FILE_OPTIONS_VERSION)
		}},
		{"git_fetch_options", func() C.int {
			var o C.git_fetch_options
			return C.git_fetch_options_init(&o, C.GIT_FETCH_OPTIONS_VERSION)
		}},
		{"git_push_options", func() C.int {
			var o C.git_push_options
			return C.git_push_options_init(&o, C.GIT_PUSH_OPTIONS_VERSION)
		}},
		{"git_proxy_options", func() C.int {
			var o C.git_proxy_options
			return C.git_proxy_options_init(&o, C.GIT_PROXY_OPTIONS_VERSION)
		}},
		{"git_remote_create_options", func() C.int {
			var o C.git_remote_create_options
			return C.git_remote_create_options_init(&o, C.GIT_REMOTE_CREATE_OPTIONS_VERSION)
		}},
		{"git_repository_init_options", func() C.int {
			var o C.git_repository_init_options
			return C.git_repository_init_options_init(&o, C.GIT_REPOSITORY_INIT_OPTIONS_VERSION)
		}},
		{"git_rebase_options", func() C.int {
			var o C.git_rebase_options
			return C.git_rebase_options_init(&o, C.GIT_REBASE_OPTIONS_VERSION)
		}},
		{"git_stash_save_options", func() C.int {
			var o C.git_stash_save_options
			return C.git_stash_save_options_init(&o, C.GIT_STASH_SAVE_OPTIONS_VERSION)
		}},
		{"git_stash_apply_options", func() C.int {
			var o C.git_stash_apply_options
			return C.git_stash_apply_options_init(&o, C.GIT_STASH_APPLY_OPTIONS_VERSION)
		}},
		{"git_status_options", func() C.int {
			var o C.git_status_options
			return C.git_status_options_init(&o, C.GIT_STATUS_OPTIONS_VERSION)
		}},
		{"git_describe_options", func() C.int {
			var o C.git_describe_options
			return C.git_describe_options_init(&o, C.GIT_DESCRIBE_OPTIONS_VERSION)
		}},
		{"git_describe_format_options", func() C.int {
			var o C.git_describe_format_options
			return C.git_describe_format_options_init(&o, C.GIT_DESCRIBE_FORMAT_OPTIONS_VERSION)
		}},
		{"git_blame_options", func() C.int {
			var o C.git_blame_options
			return C.git_blame_options_init(&o, C.GIT_BLAME_OPTIONS_VERSION)
		}},
		{"git_revert_options", func() C.int {
			var o C.git_revert_options
			return C.git_revert_options_init(&o, C.GIT_REVERT_OPTIONS_VERSION)
		}},
		{"git_cherrypick_options", func() C.int {
			var o C.git_cherrypick_options
			return C.git_cherrypick_options_init(&o, C.GIT_CHERRYPICK_OPTIONS_VERSION)
		}},
		{"git_submodule_update_options", func() C.int {
			var o C.git_submodule_update_options
			return C.git_submodule_update_options_init(&o, C.GIT_SUBMODULE_UPDATE_OPTIONS_VERSION)
		}},
	}

	for _, check := range checks {
		if ret := check.init(); ret < 0 {
			return fmt.Errorf("the loaded libgit2 rejected the compiled-in version of %s; "+
				"the headers git2go was built against are newer than the library it is "+
				"linked to", check.name)
		}
	}
	return nil
}
