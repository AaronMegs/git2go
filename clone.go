package git

/*
#include <git2.h>

extern void _go_git_populate_clone_callbacks(git_clone_options *opts, int repository_cb, int remote_cb);
*/
import "C"
import (
	"fmt"
	"runtime"
	"unsafe"
)

// RemoteCreateCallback creates a remote owned by the supplied borrowed
// repository. On success, ownership transfers to libgit2 and the returned
// remote wrapper becomes invalid. Neither wrapper may be used after the callback.
type RemoteCreateCallback func(repo *Repository, name, url string) (*Remote, error)

// RepositoryCreateCallback creates an empty repository for Clone. It can use
// InitRepositoryExt to select an object id type and reference storage format.
// On success, ownership transfers to libgit2 and the returned wrapper becomes
// invalid. Do not retain dependent objects or free the repository in the
// callback. On error, the callback remains responsible for any resources it created.
// The object id type must match the source repository.
type RepositoryCreateCallback func(path string, bare bool) (*Repository, error)

type CloneOptions struct {
	CheckoutOptions          CheckoutOptions
	FetchOptions             FetchOptions
	Bare                     bool
	CheckoutBranch           string
	RemoteCreateCallback     RemoteCreateCallback
	RepositoryCreateCallback RepositoryCreateCallback
}

func Clone(url string, path string, options *CloneOptions) (*Repository, error) {
	curl := C.CString(url)
	defer C.free(unsafe.Pointer(curl))

	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))

	var err error
	cOptions := populateCloneOptions(&C.git_clone_options{}, options, &err)
	defer freeCloneOptions(cOptions)

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	var ptr *C.git_repository
	ret := C.git_clone(&ptr, curl, cpath, cOptions)

	if ret == C.int(ErrorCodeUser) && err != nil {
		return nil, err
	}
	if ret < 0 {
		return nil, MakeGitError(ret)
	}

	return newRepositoryFromC(ptr), nil
}

//export remoteCreateCallback
func remoteCreateCallback(
	out **C.git_remote,
	crepo *C.git_repository,
	cname, curl *C.char,
	handle unsafe.Pointer,
) (ret C.int) {
	completed := false
	defer recoverCloneCallback(&ret, handle, &completed)
	name := C.GoString(cname)
	url := C.GoString(curl)
	repo := newRepositoryFromC(crepo)
	repo.weak = true
	defer repo.Free()

	data, ok := pointerHandles.Get(handle).(*cloneCallbackData)
	if !ok {
		panic("invalid remote create callback")
	}

	remote, err := data.options.RemoteCreateCallback(repo, name, url)

	if err != nil {
		*data.errorTarget = err
		completed = true
		return C.int(ErrorCodeUser)
	}
	if remote == nil || remote.ptr == nil || remote.weak || remote.repo == nil || remote.repo.ptr != crepo {
		*data.errorTarget = fmt.Errorf("remote create callback must return a live owned remote from the supplied repository")
		completed = true
		return C.int(ErrorCodeUser)
	}

	*out = remote.ptr
	remote.repo.Remotes.untrackRemote(remote)
	runtime.SetFinalizer(remote, nil)
	remote.ptr = nil
	remote.repo = nil

	completed = true
	return C.int(ErrorCodeOK)
}

//export repositoryCreateCallback
func repositoryCreateCallback(out **C.git_repository, cpath *C.char, bare C.int, handle unsafe.Pointer) (ret C.int) {
	completed := false
	defer recoverCloneCallback(&ret, handle, &completed)
	data := pointerHandles.Get(handle).(*cloneCallbackData)
	repo, err := data.options.RepositoryCreateCallback(C.GoString(cpath), bare != 0)
	if err != nil {
		*data.errorTarget = err
		completed = true
		return C.int(ErrorCodeUser)
	}
	if repo == nil || repo.ptr == nil || repo.weak {
		*data.errorTarget = fmt.Errorf("repository create callback must return a live owned repository")
		completed = true
		return C.int(ErrorCodeUser)
	}
	*out = repo.ptr
	runtime.SetFinalizer(repo, nil)
	repo.ptr = nil
	completed = true
	return C.int(ErrorCodeOK)
}

func recoverCloneCallback(ret *C.int, handle unsafe.Pointer, completed *bool) {
	if value := recover(); value != nil || !*completed {
		*ret = C.int(ErrorCodeUser)
		if payload, ok := pointerHandles.GetOk(handle); ok {
			if data, ok := payload.(*cloneCallbackData); ok && data.errorTarget != nil {
				*data.errorTarget = &GitError{
					Message: fmt.Sprintf("panic in clone callback: %v", value),
					Class:   ErrorClassCallback,
					Code:    ErrorCodeUser,
				}
			}
		}
	}
}

type cloneCallbackData struct {
	options     *CloneOptions
	errorTarget *error
}

func populateCloneOptions(copts *C.git_clone_options, opts *CloneOptions, errorTarget *error) *C.git_clone_options {
	C.git_clone_options_init(copts, C.GIT_CLONE_OPTIONS_VERSION)
	if opts == nil {
		return nil
	}
	populateCheckoutOptions(&copts.checkout_opts, &opts.CheckoutOptions, errorTarget)
	populateFetchOptions(&copts.fetch_opts, &opts.FetchOptions, errorTarget)
	copts.bare = cbool(opts.Bare)
	if opts.CheckoutBranch != "" {
		copts.checkout_branch = C.CString(opts.CheckoutBranch)
	}

	if opts.RemoteCreateCallback != nil || opts.RepositoryCreateCallback != nil {
		data := &cloneCallbackData{
			options:     opts,
			errorTarget: errorTarget,
		}
		C._go_git_populate_clone_callbacks(copts, cbool(opts.RepositoryCreateCallback != nil), cbool(opts.RemoteCreateCallback != nil))
		if opts.RemoteCreateCallback != nil {
			copts.remote_cb_payload = pointerHandles.Track(data)
		}
		if opts.RepositoryCreateCallback != nil {
			copts.repository_cb_payload = pointerHandles.Track(data)
		}
	}

	return copts
}

func freeCloneOptions(copts *C.git_clone_options) {
	if copts == nil {
		return
	}

	freeCheckoutOptions(&copts.checkout_opts)
	freeFetchOptions(&copts.fetch_opts)

	if copts.remote_cb_payload != nil {
		pointerHandles.Untrack(copts.remote_cb_payload)
	}
	if copts.repository_cb_payload != nil {
		pointerHandles.Untrack(copts.repository_cb_payload)
	}

	C.free(unsafe.Pointer(copts.checkout_branch))
}
