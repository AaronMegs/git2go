#include "_cgo_export.h"

#include <git2.h>
#include <git2/sys/odb_backend.h>
#include <git2/sys/refdb_backend.h>
#include <git2/sys/credential.h>
#include <git2/sys/errors.h>

// There are two ways in which to declare a callback:
//
// * If there is a guarantee that the callback will always be called within the
//   same stack (e.g. by passing the callback directly into a function / into a
//   struct that goes into a function), the following pattern is preferred,
//   which preserves the error object as-is:
//
//   // myfile.go
//   type FooCallback func(...) (..., error)
//   type fooCallbackData struct {
//     callback    FooCallback
//     errorTarget *error
//   }
//
//   //export fooCallback
//   func fooCallback(..., handle unsafe.Pointer) C.int {
//     payload := pointerHandles.Get(handle)
//     data := payload.(*fooCallbackData)
//     ...
//     err := data.callback(...)
//     if err != nil {
//       *data.errorTarget = err
//       return C.int(ErrorCodeUser)
//     }
//     return C.int(ErrorCodeOK)
//   }
//
//   func MyFunction(... callback FooCallback) error {
//    var err error
//    data := fooCallbackData{
//      callback:    callback,
//      errorTarget: &err,
//    }
//    handle := pointerHandles.Track(&data)
//    defer pointerHandles.Untrack(handle)
//
//    runtime.LockOSThread()
//    defer runtime.UnlockOSThread()
//
//    ret := C._go_git_my_function(..., handle)
//    if ret == C.int(ErrorCodeUser) && err != nil {
//      return err
//    }
//    if ret < 0 {
//      return MakeGitError(ret)
//    }
//    return nil
//   }
//
//   // wrapper.c
//   int _go_git_my_function(..., void *payload)
//   {
//     return git_my_function(..., (git_foo_cb)&fooCallback, payload);
//   }
//
// * Additionally, if the same callback can be invoked from multiple functions or
//   from different stacks (e.g. when passing the callback to an object), the
//   following pattern should be used in tandem, which has the downside of
//   losing the original error object and converting it to a GitError if the
//   callback happens from a different stack:
//
//   // myfile.go
//   type FooCallback func(...) (..., error)
//   type fooCallbackData struct {
//     callback    FooCallback
//     errorTarget *error
//   }
//
//   //export fooCallback
//   func fooCallback(errorMessage **C.char, ..., handle unsafe.Pointer) C.int {
//     data := pointerHandles.Get(data).(*fooCallbackData)
//     ...
//     err := data.callback(...)
//     if err != nil {
//       if data.errorTarget != nil {
//         *data.errorTarget = err
//       }
//       return setCallbackError(errorMessage, err)
//     }
//     return C.int(ErrorCodeOK)
//   }
//
//   // wrapper.c
//   static int foo_callback(...)
//   {
//     char *error_message = NULL;
//     const int ret = fooCallback(&error_message, ...);
//     return set_callback_error(error_message, ret);
//   }

/**
 * Sets the thread-local error to the provided string. This needs to happen in
 * C because Go might change Goroutines _just_ before returning, which would
 * lose the contents of the error message.
 */
static int set_callback_error(char *error_message, int ret)
{
	if (error_message != NULL) {
		if (ret < 0)
			git_error_set_str(GIT_ERROR_CALLBACK, error_message);
		free(error_message);
	}
	return ret;
}

void _go_git_populate_apply_callbacks(git_apply_options *options)
{
	options->delta_cb = (git_apply_delta_cb)&deltaApplyCallback;
	options->hunk_cb = (git_apply_hunk_cb)&hunkApplyCallback;
}

static int commit_create_callback(
		git_oid *out,
		const git_signature *author,
		const git_signature *committer,
		const char *message_encoding,
		const char *message,
		const git_tree *tree,
		size_t parent_count,
		const git_commit *parents[],
		void *payload)
{
	char *error_message = NULL;
	const int ret = commitCreateCallback(
			&error_message,
			out,
			(git_signature *)author,
			(git_signature *)committer,
			(char *)message_encoding,
			(char *)message,
			(git_tree *)tree,
			parent_count,
			(git_commit **)parents,
			payload
	);
	return set_callback_error(error_message, ret);
}

void _go_git_populate_rebase_callbacks(git_rebase_options *opts)
{
	opts->commit_create_cb = commit_create_callback;
}

void _go_git_populate_clone_callbacks(git_clone_options *opts)
{
	opts->remote_cb = (git_remote_create_cb)&remoteCreateCallback;
}

void _go_git_populate_checkout_callbacks(git_checkout_options *opts)
{
	opts->notify_cb = (git_checkout_notify_cb)&checkoutNotifyCallback;
	opts->progress_cb = (git_checkout_progress_cb)&checkoutProgressCallback;
}

int _go_git_visit_submodule(git_repository *repo, void *fct)
{
	return git_submodule_foreach(repo, (git_submodule_cb)&submoduleCallback, fct);
}

int _go_git_treewalk(git_tree *tree, git_treewalk_mode mode, void *ptr)
{
	return git_tree_walk(tree, mode, (git_treewalk_cb)&treeWalkCallback, ptr);
}

int _go_git_packbuilder_foreach(git_packbuilder *pb, void *payload)
{
	return git_packbuilder_foreach(pb, (git_packbuilder_foreach_cb)&packbuilderForEachCallback, payload);
}

int _go_git_odb_foreach(git_odb *db, void *payload)
{
	return git_odb_foreach(db, (git_odb_foreach_cb)&odbForEachCallback, payload);
}

void _go_git_odb_backend_free(git_odb_backend *backend)
{
	if (!backend->free)
		return;
	backend->free(backend);
}

void _go_git_refdb_backend_free(git_refdb_backend *backend)
{
	if (!backend->free)
		return;
	backend->free(backend);
}

int _go_git_diff_foreach(git_diff *diff, int eachFile, int eachHunk, int eachLine, void *payload)
{
	git_diff_file_cb fcb = NULL;
	git_diff_hunk_cb hcb = NULL;
	git_diff_line_cb lcb = NULL;

	if (eachFile)
		fcb = (git_diff_file_cb)&diffForEachFileCallback;
	if (eachHunk)
		hcb = (git_diff_hunk_cb)&diffForEachHunkCallback;
	if (eachLine)
		lcb = (git_diff_line_cb)&diffForEachLineCallback;

	return git_diff_foreach(diff, fcb, NULL, hcb, lcb, payload);
}

int _go_git_diff_blobs(
		git_blob *old,
		const char *old_path,
		git_blob *new,
		const char *new_path,
		git_diff_options *opts,
		int eachFile,
		int eachHunk,
		int eachLine,
		void *payload)
{
	git_diff_file_cb fcb = NULL;
	git_diff_hunk_cb hcb = NULL;
	git_diff_line_cb lcb = NULL;

	if (eachFile)
		fcb = (git_diff_file_cb)&diffForEachFileCallback;
	if (eachHunk)
		hcb = (git_diff_hunk_cb)&diffForEachHunkCallback;
	if (eachLine)
		lcb = (git_diff_line_cb)&diffForEachLineCallback;

	return git_diff_blobs(old, old_path, new, new_path, opts, fcb, NULL, hcb, lcb, payload);
}

void _go_git_setup_diff_notify_callbacks(git_diff_options *opts)
{
	opts->notify_cb = (git_diff_notify_cb)&diffNotifyCallback;
}

static int sideband_progress_callback(const char *str, int len, void *payload)
{
	char *error_message = NULL;
	const int ret = sidebandProgressCallback(&error_message, (char *)str, len, payload);
	return set_callback_error(error_message, ret);
}

static int completion_callback(git_remote_completion_type completion_type, void *data)
{
	char *error_message = NULL;
	const int ret = completionCallback(&error_message, completion_type, data);
	return set_callback_error(error_message, ret);
}

static int credentials_callback(
		git_credential **cred,
		const char *url,
		const char *username_from_url,
		unsigned int allowed_types,
		void *data)
{
	char *error_message = NULL;
	const int ret = credentialsCallback(
			&error_message,
			cred,
			(char *)url,
			(char *)username_from_url,
			allowed_types,
			data
	);
	return set_callback_error(error_message, ret);
}

static int transfer_progress_callback(const git_transfer_progress *stats, void *data)
{
	char *error_message = NULL;
	const int ret = transferProgressCallback(
			&error_message,
			(git_transfer_progress *)stats,
			data
	);
	return set_callback_error(error_message, ret);
}

static int update_tips_callback(const char *refname, const git_oid *a, const git_oid *b, void *data)
{
	char *error_message = NULL;
	const int ret = updateTipsCallback(
			&error_message,
			(char *)refname,
			(git_oid *)a,
			(git_oid *)b,
			data
	);
	return set_callback_error(error_message, ret);
}

static int update_refs_callback(
		const char *refname,
		const git_oid *a,
		const git_oid *b,
		git_refspec *spec,
		void *data)
{
	char *error_message = NULL;
	const int ret = updateRefsCallback(
			&error_message,
			(char *)refname,
			(git_oid *)a,
			(git_oid *)b,
			spec,
			data
	);
	return set_callback_error(error_message, ret);
}

static int certificate_check_callback(git_cert *cert, int valid, const char *host, void *data)
{
	char *error_message = NULL;
	const int ret = certificateCheckCallback(
			&error_message,
			cert,
			valid,
			(char *)host,
			data
	);
	return set_callback_error(error_message, ret);
}

static int pack_progress_callback(int stage, unsigned int current, unsigned int total, void *data)
{
	char *error_message = NULL;
	const int ret = packProgressCallback(
			&error_message,
			stage,
			current,
			total,
			data
	);
	return set_callback_error(error_message, ret);
}

static int push_transfer_progress_callback(
		unsigned int current,
		unsigned int total,
		size_t bytes,
		void *data)
{
	char *error_message = NULL;
	const int ret = pushTransferProgressCallback(
			&error_message,
			current,
			total,
			bytes,
			data
	);
	return set_callback_error(error_message, ret);
}

static int push_update_reference_callback(const char *refname, const char *status, void *data)
{
	char *error_message = NULL;
	const int ret = pushUpdateReferenceCallback(
			&error_message,
			(char *)refname,
			(char *)status,
			data
	);
	return set_callback_error(error_message, ret);
}

void _go_git_populate_remote_callbacks(git_remote_callbacks *callbacks)
{
	callbacks->sideband_progress = sideband_progress_callback;
	callbacks->completion = completion_callback;
	callbacks->credentials = credentials_callback;
	callbacks->transfer_progress = transfer_progress_callback;
	callbacks->update_tips = update_tips_callback;
	callbacks->certificate_check = certificate_check_callback;
	callbacks->pack_progress = pack_progress_callback;
	callbacks->push_transfer_progress = push_transfer_progress_callback;
	callbacks->push_update_reference = push_update_reference_callback;
	callbacks->update_refs = update_refs_callback;
}

int _go_git_index_add_all(git_index *index, const git_strarray *pathspec, unsigned int flags, void *callback)
{
	git_index_matched_path_cb cb = callback ? (git_index_matched_path_cb)&indexMatchedPathCallback : NULL;
	return git_index_add_all(index, pathspec, flags, cb, callback);
}

int _go_git_index_update_all(git_index *index, const git_strarray *pathspec, void *callback)
{
	git_index_matched_path_cb cb = callback ? (git_index_matched_path_cb)&indexMatchedPathCallback : NULL;
	return git_index_update_all(index, pathspec, cb, callback);
}

int _go_git_index_remove_all(git_index *index, const git_strarray *pathspec, void *callback)
{
	git_index_matched_path_cb cb = callback ? (git_index_matched_path_cb)&indexMatchedPathCallback : NULL;
	return git_index_remove_all(index, pathspec, cb, callback);
}

int _go_git_tag_foreach(git_repository *repo, void *payload)
{
	return git_tag_foreach(repo, (git_tag_foreach_cb)&tagForeachCallback, payload);
}

int _go_git_merge_file(
		git_merge_file_result* out,
		char* ancestorContents,
		size_t ancestorLen,
		char* ancestorPath,
		unsigned int ancestorMode,
		char* oursContents,
		size_t oursLen,
		char* oursPath,
		unsigned int oursMode,
		char* theirsContents,
		size_t theirsLen,
		char* theirsPath,
		unsigned int theirsMode,
		git_merge_file_options* copts)
{
	git_merge_file_input ancestor = GIT_MERGE_FILE_INPUT_INIT;
	git_merge_file_input ours = GIT_MERGE_FILE_INPUT_INIT;
	git_merge_file_input theirs = GIT_MERGE_FILE_INPUT_INIT;

	ancestor.ptr = ancestorContents;
	ancestor.size = ancestorLen;
	ancestor.path = ancestorPath;
	ancestor.mode = ancestorMode;

	ours.ptr = oursContents;
	ours.size = oursLen;
	ours.path = oursPath;
	ours.mode = oursMode;

	theirs.ptr = theirsContents;
	theirs.size = theirsLen;
	theirs.path = theirsPath;
	theirs.mode = theirsMode;

	return git_merge_file(out, &ancestor, &ours, &theirs, copts);
}

void _go_git_populate_stash_apply_callbacks(git_stash_apply_options *opts)
{
	opts->progress_cb = (git_stash_apply_progress_cb)&stashApplyProgressCallback;
}

int _go_git_stash_foreach(git_repository *repo, void *payload)
{
	return git_stash_foreach(repo, (git_stash_cb)&stashForeachCallback, payload);
}

int _go_git_writestream_write(git_writestream *stream, const char *buffer, size_t len)
{
	return stream->write(stream, buffer, len);
}

int _go_git_writestream_close(git_writestream *stream)
{
	return stream->close(stream);
}

void _go_git_writestream_free(git_writestream *stream)
{
	stream->free(stream);
}

git_credential_t _go_git_credential_credtype(git_credential *cred)
{
	return cred->credtype;
}

static int credential_ssh_sign_callback(
		LIBSSH2_SESSION *session,
		unsigned char **sig, size_t *sig_len,
		const unsigned char *data, size_t data_len,
		void **abstract)
{
	char *error_message = NULL;
	const int ret = credentialSSHSignCallback(
			&error_message,
			sig,
			sig_len,
			(unsigned char *)data,
			data_len,
			(void *)*(uintptr_t *)abstract);
	return set_callback_error(error_message, ret);
}

void _go_git_populate_credential_ssh_custom(git_credential_ssh_custom *cred)
{
	cred->parent.free = (void (*)(git_credential *))credentialSSHCustomFree;
	cred->sign_callback = credential_ssh_sign_callback;
}

int _go_git_odb_write_pack(git_odb_writepack **out, git_odb *db, void *progress_payload)
{
	return git_odb_write_pack(out, db, transfer_progress_callback, progress_payload);
}

int _go_git_odb_writepack_append(
		git_odb_writepack *writepack,
		const void *data,
		size_t size,
		git_transfer_progress *stats)
{
	return writepack->append(writepack, data, size, stats);
}

int _go_git_odb_writepack_commit(git_odb_writepack *writepack, git_transfer_progress *stats)
{
	return writepack->commit(writepack, stats);
}

void _go_git_odb_writepack_free(git_odb_writepack *writepack)
{
	writepack->free(writepack);
}

int _go_git_indexer_new(
		git_indexer **out,
		const char *path,
		unsigned int mode,
		git_odb *odb,
		int oid_type,
		void *progress_cb_payload)
{
	git_indexer_options indexer_options = GIT_INDEXER_OPTIONS_INIT;
	indexer_options.progress_cb = transfer_progress_callback;
	indexer_options.progress_cb_payload = progress_cb_payload;
#ifdef GIT_EXPERIMENTAL_SHA256
	// In the experimental ABI mode and odb moved into the options struct.
	indexer_options.mode = mode;
	indexer_options.odb = odb;
	if (oid_type != 0)
		indexer_options.oid_type = (git_oid_t)oid_type;
	return git_indexer_new(out, path, &indexer_options);
#else
	(void)oid_type;
	return git_indexer_new(out, path, mode, odb, &indexer_options);
#endif
}

// ----------------------------------------------------------------------------
// SHA1/SHA256 compatibility shims.
//
// cgo cannot conditionally call functions whose signature changes with a macro,
// so we expose these stable-signature wrappers and select the right underlying
// call here. The `oid_type` argument follows git_oid_t (1=SHA1, 2=SHA256); a
// value of 0 means "use the libgit2 default" (SHA1).
//
// TWO upstream shapes are supported behind GIT_EXPERIMENTAL_SHA256:
//
//   (A) The libgit2 1.9.x experimental "overload" shape (the project's pinned
//       submodule, f7164261 == 1.9.4): the existing functions gain an extra
//       git_oid_t parameter / options field in place. This is the DEFAULT path
//       here and is the one covered by the end-to-end tests.
//
//   (B) The libgit2 `main` "split" shape: the legacy names are frozen to SHA1
//       and NEW, separately-named entry points carry the type
//       (git_oid_from_string/from_prefix/from_raw, git_odb_new_ext,
//       git_index_new_ext/open_ext, git_diff_from_buffer_ext, ...). Select this
//       path by defining GIT2GO_LIBGIT2_OID_EXT_API (the `libgit2_next` go build
//       tag injects it). NOTE: libgit2 main's version.h still reports 1.9.0
//       (LOWER than the 1.9.4 release), so the two shapes cannot be told apart
//       by LIBGIT2_VERSION_NUMBER; an explicit opt-in is required.
//
// Functions whose shape is IDENTICAL on both (git_repository_init_ext via
// opts.oid_type, git_indexer_new overload) need no (B) branch.
//
// TODO(sha256-merge): when upstream promotes SHA256 out of GIT_EXPERIMENTAL_SHA256,
// collapse to the (then-stable) signature. See docs/sha256-compat-design.md s4.6.
// ----------------------------------------------------------------------------

int _go_git_oid_fromstrn(git_oid *out, const char *str, size_t length, int oid_type)
{
#ifdef GIT_EXPERIMENTAL_SHA256
# if defined(GIT2GO_LIBGIT2_OID_EXT_API)
	return git_oid_from_prefix(out, str, length, oid_type ? (git_oid_t)oid_type : GIT_OID_DEFAULT);
# else
	return git_oid_fromstrn(out, str, length, oid_type ? (git_oid_t)oid_type : GIT_OID_DEFAULT);
# endif
#else
	(void)oid_type;
	return git_oid_fromstrn(out, str, length);
#endif
}

int _go_git_oid_fromraw(git_oid *out, const unsigned char *raw, int oid_type)
{
#ifdef GIT_EXPERIMENTAL_SHA256
# if defined(GIT2GO_LIBGIT2_OID_EXT_API)
	return git_oid_from_raw(out, raw, oid_type ? (git_oid_t)oid_type : GIT_OID_DEFAULT);
# else
	return git_oid_fromraw(out, raw, oid_type ? (git_oid_t)oid_type : GIT_OID_DEFAULT);
# endif
#else
	(void)oid_type;
	return git_oid_fromraw(out, raw);
#endif
}

int _go_git_repository_init(git_repository **out, const char *path, unsigned is_bare, int oid_type)
{
	git_repository_init_options opts = GIT_REPOSITORY_INIT_OPTIONS_INIT;
	if (is_bare)
		opts.flags |= GIT_REPOSITORY_INIT_BARE;
#ifdef GIT_EXPERIMENTAL_SHA256
	if (oid_type != 0)
		opts.oid_type = (git_oid_t)oid_type;
#else
	(void)oid_type;
#endif
	return git_repository_init_ext(out, path, &opts);
}

// _go_git_repository_oid_type returns the object id type of a repository as an
// int (following git_oid_t: 1=SHA1, 2=SHA256). This getter is unconditional on
// libgit2 main; on older libgit2 that predates it, the GIT2GO_HAVE_REPO_OID_TYPE
// gate (injected by the libgit2_next build tag) keeps it out of the build. When
// unavailable it reports 1 (SHA1), which is the only type those libgit2 support.
int _go_git_repository_oid_type(git_repository *repo)
{
#if defined(GIT2GO_HAVE_REPO_OID_TYPE)
	return (int)git_repository_oid_type(repo);
#else
	(void)repo;
	return 1; /* GIT_OID_SHA1 */
#endif
}

int _go_git_odb_hash(git_oid *out, const void *data, size_t len, git_object_t obj_type, int oid_type)
{
#ifdef GIT_EXPERIMENTAL_SHA256
# if defined(GIT2GO_LIBGIT2_OID_EXT_API)
	// On libgit2 main, git_odb_hash is deprecated and SHA1-only; typed hashing
	// moved to git_object_id_from_buffer(oid, buf, len, git_object_id_options*),
	// where the options carry both object_type and oid_type.
	git_object_id_options opts = GIT_OBJECT_ID_OPTIONS_INIT;
	opts.object_type = obj_type;
	if (oid_type != 0)
		opts.oid_type = (git_oid_t)oid_type;
	return git_object_id_from_buffer(out, data, len, &opts);
# else
	return git_odb_hash(out, data, len, obj_type, oid_type ? (git_oid_t)oid_type : GIT_OID_DEFAULT);
# endif
#else
	(void)oid_type;
	return git_odb_hash(out, data, len, obj_type);
#endif
}

int _go_git_odb_new(git_odb **out)
{
#ifdef GIT_EXPERIMENTAL_SHA256
# if defined(GIT2GO_LIBGIT2_OID_EXT_API)
	return git_odb_new_ext(out, NULL);
# else
	return git_odb_new(out, NULL);
# endif
#else
	return git_odb_new(out);
#endif
}

int _go_git_index_new(git_index **out)
{
#ifdef GIT_EXPERIMENTAL_SHA256
# if defined(GIT2GO_LIBGIT2_OID_EXT_API)
	return git_index_new_ext(out, NULL);
# else
	return git_index_new(out, NULL);
# endif
#else
	return git_index_new(out);
#endif
}

int _go_git_index_open(git_index **out, const char *index_path)
{
#ifdef GIT_EXPERIMENTAL_SHA256
# if defined(GIT2GO_LIBGIT2_OID_EXT_API)
	return git_index_open_ext(out, index_path, NULL);
# else
	return git_index_open(out, index_path, NULL);
# endif
#else
	return git_index_open(out, index_path);
#endif
}

int _go_git_diff_from_buffer(git_diff **out, const char *content, size_t content_len)
{
#ifdef GIT_EXPERIMENTAL_SHA256
# if defined(GIT2GO_LIBGIT2_OID_EXT_API)
	return git_diff_from_buffer_ext(out, content, content_len, NULL);
# else
	return git_diff_from_buffer(out, content, content_len, NULL);
# endif
#else
	return git_diff_from_buffer(out, content, content_len);
#endif
}

// git_odb_backend_one_pack gains a git_odb_backend_pack_options* on both the
// 1.9.x experimental ABI and libgit2 main (same shape here), so a single
// experimental branch covers both. git_odb_backend_loose likewise takes a
// git_odb_backend_loose_options* under experimental on both.
int _go_git_odb_backend_one_pack(git_odb_backend **out, const char *index_file)
{
#ifdef GIT_EXPERIMENTAL_SHA256
	return git_odb_backend_one_pack(out, index_file, NULL);
#else
	return git_odb_backend_one_pack(out, index_file);
#endif
}

int _go_git_odb_backend_loose(
		git_odb_backend **out,
		const char *objects_dir,
		int compression_level,
		int do_fsync,
		unsigned int dir_mode,
		unsigned int file_mode)
{
#ifdef GIT_EXPERIMENTAL_SHA256
	git_odb_backend_loose_options opts = GIT_ODB_BACKEND_LOOSE_OPTIONS_INIT;
	opts.compression_level = compression_level;
	if (do_fsync)
		opts.flags |= GIT_ODB_BACKEND_LOOSE_FSYNC;
	opts.dir_mode = dir_mode;
	opts.file_mode = file_mode;
	return git_odb_backend_loose(out, objects_dir, &opts);
#else
	return git_odb_backend_loose(out, objects_dir, compression_level, do_fsync, dir_mode, file_mode);
#endif
}

static int smart_transport_callback(
		git_transport **out,
		git_remote *owner,
		void *param)
{
	char *error_message = NULL;
	const int ret = smartTransportCallback(
			&error_message,
			out,
			owner,
			param);
	return set_callback_error(error_message, ret);
}

int _go_git_transport_register(const char *prefix, void *param)
{
	return git_transport_register(prefix, smart_transport_callback, param);
}

static int smart_subtransport_action_callback(
		git_smart_subtransport_stream **out,
		git_smart_subtransport *transport,
		const char *url,
		git_smart_service_t action)
{
	char *error_message = NULL;
	const int ret = smartSubtransportActionCallback(
			&error_message,
			out,
			transport,
			(char *)url,
			action);
	return set_callback_error(error_message, ret);
}

static int smart_subtransport_close_callback(git_smart_subtransport *transport)
{
	char *error_message = NULL;
	const int ret = smartSubtransportCloseCallback(
			&error_message,
			transport);
	return set_callback_error(error_message, ret);
}

static int smart_subtransport_callback(
		git_smart_subtransport **out,
		git_transport *owner,
		void *param)
{
	_go_managed_smart_subtransport *subtransport = (_go_managed_smart_subtransport *)param;
	subtransport->parent.action = smart_subtransport_action_callback;
	subtransport->parent.close = smart_subtransport_close_callback;
	subtransport->parent.free = smartSubtransportFreeCallback;

	*out = &subtransport->parent;
	char *error_message = NULL;
	const int ret = smartTransportSubtransportCallback(&error_message, subtransport, owner);
	return set_callback_error(error_message, ret);
}

int _go_git_transport_smart(
		git_transport **out,
		git_remote *owner,
		int stateless,
		_go_managed_smart_subtransport *subtransport_payload)
{
	git_smart_subtransport_definition definition = {
		smart_subtransport_callback,
		stateless,
		subtransport_payload,
	};

	return git_transport_smart(out, owner, &definition);
}

static int smart_subtransport_stream_read_callback(
		git_smart_subtransport_stream *stream,
		char *buffer,
		size_t buf_size,
		size_t *bytes_read)
{
	char *error_message = NULL;
	const int ret = smartSubtransportStreamReadCallback(
			&error_message,
			stream,
			buffer,
			buf_size,
			bytes_read);
	return set_callback_error(error_message, ret);
}

static int smart_subtransport_stream_write_callback(
		git_smart_subtransport_stream *stream,
		const char *buffer,
		size_t len)
{
	char *error_message = NULL;
	const int ret = smartSubtransportStreamWriteCallback(
			&error_message,
			stream,
			(char *)buffer,
			len);
	return set_callback_error(error_message, ret);
}

void _go_git_setup_smart_subtransport_stream(_go_managed_smart_subtransport_stream *stream)
{
	_go_managed_smart_subtransport_stream *managed_stream = (_go_managed_smart_subtransport_stream *)stream;
	managed_stream->parent.read = smart_subtransport_stream_read_callback;
	managed_stream->parent.write = smart_subtransport_stream_write_callback;
	managed_stream->parent.free = smartSubtransportStreamFreeCallback;
}
