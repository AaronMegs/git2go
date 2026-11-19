package git

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	REMOTENAME = "testremote"
)

func TestClone(t *testing.T) {
	t.Parallel()
	repo := createTestRepo(t)
	defer cleanupTestRepo(t, repo)

	seedTestRepo(t, repo)

	path, err := ioutil.TempDir("", "git2go")
	checkFatal(t, err)

	branchName := defaultBranchName(t, repo)
	ref, err := repo.References.Lookup("refs/heads/" + branchName)
	checkFatal(t, err)

	repo2, err := Clone(repo.Path(), path, &CloneOptions{Bare: true})
	defer cleanupTestRepo(t, repo2)

	checkFatal(t, err)

	ref2, err := repo2.References.Lookup("refs/heads/" + branchName)
	checkFatal(t, err)

	if ref.Cmp(ref2) != 0 {
		t.Fatal("reference in clone does not match original ref")
	}
}

func TestCloneWithCallback(t *testing.T) {
	t.Parallel()
	testPayload := 0

	repo := createTestRepo(t)
	defer cleanupTestRepo(t, repo)

	seedTestRepo(t, repo)

	path, err := ioutil.TempDir("", "git2go")
	checkFatal(t, err)

	opts := CloneOptions{
		Bare: true,
		RemoteCreateCallback: func(r *Repository, name, url string) (*Remote, error) {
			testPayload += 1
			return r.Remotes.Create(REMOTENAME, url)
		},
	}

	repo2, err := Clone(repo.Path(), path, &opts)
	defer cleanupTestRepo(t, repo2)

	checkFatal(t, err)

	if testPayload != 1 {
		t.Fatal("Payload's value has not been changed")
	}

	remote, err := repo2.Remotes.Lookup(REMOTENAME)
	if err != nil || remote == nil {
		t.Fatal("Remote was not created properly")
	}
	defer remote.Free()
}

func TestCloneNilOptions(t *testing.T) {
	src := createTestRepo(t)
	defer cleanupTestRepo(t, src)
	id, _ := seedTestRepo(t, src)
	dst, err := Clone(src.Path(), filepath.Join(t.TempDir(), "clone"), nil)
	checkFatal(t, err)
	defer dst.Free()
	head, err := dst.Head()
	checkFatal(t, err)
	defer head.Free()
	if !head.Target().Equal(id) {
		t.Fatal("nil-options clone lost HEAD")
	}
}

func TestCloneRepositoryCallbackFormats(t *testing.T) {
	for _, oidType := range []ObjectIdType{ObjectIdSHA1, ObjectIdSHA256} {
		for _, refType := range []RefdbType{RefdbFiles, RefdbReftable} {
			t.Run(fmt.Sprintf("%s-%v", oidType, refType), func(t *testing.T) {
				if refType == RefdbReftable && !IsReftableSupported() {
					t.Skip("reftable disabled")
				}
				src, err := InitRepositoryWithOidType(t.TempDir(), true, oidType)
				checkFatal(t, err)
				defer src.Free()
				id := seedCommit(t, src)
				branch := defaultBranchName(t, src)
				dstPath := filepath.Join(t.TempDir(), "clone")
				var transferred *Repository
				var transferredRemote *Remote
				calls := 0
				opts := &CloneOptions{Bare: true, CheckoutBranch: branch,
					RepositoryCreateCallback: func(path string, bare bool) (*Repository, error) {
						calls++
						if filepath.Clean(path) != dstPath || !bare {
							return nil, errors.New("wrong repository callback arguments")
						}
						var err error
						transferred, err = InitRepositoryExt(path, &RepositoryInitOptions{Flags: RepositoryInitMkpath | RepositoryInitBare, OidType: oidType, RefdbType: refType})
						return transferred, err
					},
					RemoteCreateCallback: func(repo *Repository, name, url string) (*Remote, error) {
						var err error
						transferredRemote, err = repo.Remotes.Create(name, url)
						return transferredRemote, err
					},
				}
				dst, err := Clone(src.Path(), dstPath, opts)
				checkFatal(t, err)
				defer dst.Free()
				if calls != 1 || transferred == nil || transferred.ptr != nil || transferredRemote == nil || transferredRemote.ptr != nil {
					t.Fatal("callback ownership was not transferred exactly once")
				}
				transferred.Free()
				transferredRemote.Free()
				dst.Free()
				reopened, err := OpenRepository(dstPath)
				checkFatal(t, err)
				defer reopened.Free()
				assertRepositoryFormats(t, reopened, oidType, refType)
				ref, err := reopened.References.Lookup("refs/heads/" + branch)
				checkFatal(t, err)
				defer ref.Free()
				if !ref.Target().Equal(id) {
					t.Fatal("clone reference did not persist")
				}
				commit, err := reopened.LookupCommit(id)
				checkFatal(t, err)
				commit.Free()
			})
		}
	}
}

func TestCloneCallbackFailures(t *testing.T) {
	src := createTestRepo(t)
	defer cleanupTestRepo(t, src)
	seedTestRepo(t, src)
	sentinel := errors.New("clone callback failure")
	for _, kind := range []string{"repository", "remote"} {
		for _, failure := range []string{"error", "nil", "panic", "panic-nil"} {
			t.Run(kind+"-"+failure, func(t *testing.T) {
				fail := func() error {
					if failure == "panic-nil" {
						panic(nil)
					}
					if failure == "panic" {
						panic("clone panic probe")
					}
					if failure == "error" {
						return sentinel
					}
					return nil
				}
				opts := &CloneOptions{Bare: true}
				if kind == "repository" {
					opts.RepositoryCreateCallback = func(string, bool) (*Repository, error) { return nil, fail() }
				} else {
					opts.RemoteCreateCallback = func(*Repository, string, string) (*Remote, error) { return nil, fail() }
				}
				repo, err := Clone(src.Path(), filepath.Join(t.TempDir(), "clone"), opts)
				if repo != nil {
					repo.Free()
					t.Fatal("failed callback returned a repository")
				}
				if err == nil {
					t.Fatal("callback failure was swallowed")
				}
				if failure == "error" && err != sentinel {
					t.Fatalf("original error lost: %v", err)
				}
				if strings.HasPrefix(failure, "panic") && (!IsErrorCode(err, ErrorCodeUser) || !IsErrorClass(err, ErrorClassCallback) || !strings.Contains(err.Error(), "panic in clone callback")) {
					t.Fatalf("panic not contained: %v", err)
				}
				if failure == "panic" && !strings.Contains(err.Error(), "clone panic probe") {
					t.Fatalf("panic not preserved as error: %v", err)
				}
			})
		}
	}
}

func TestCloneBundleFormats(t *testing.T) {
	for _, oidType := range []ObjectIdType{ObjectIdSHA1, ObjectIdSHA256} {
		for _, refType := range []RefdbType{RefdbFiles, RefdbReftable} {
			t.Run(fmt.Sprintf("%s-%v", oidType, refType), func(t *testing.T) {
				if refType == RefdbReftable && !IsReftableSupported() {
					t.Skip("reftable disabled")
				}
				name := "testrepo.bundle"
				if oidType == ObjectIdSHA256 {
					name = "testrepo_256.bundle"
				}
				bundle, err := filepath.Abs(filepath.Join("vendor", "libgit2", "tests", "resources", "bundle", name))
				checkFatal(t, err)
				var opts *CloneOptions
				if refType == RefdbReftable {
					opts = &CloneOptions{RepositoryCreateCallback: func(path string, bare bool) (*Repository, error) {
						flags := RepositoryInitMkpath
						if bare {
							flags |= RepositoryInitBare
						}
						return InitRepositoryExt(path, &RepositoryInitOptions{Flags: flags, OidType: oidType, RefdbType: refType})
					}}
				}
				dstPath := filepath.Join(t.TempDir(), "clone")
				repo, err := Clone(bundle, dstPath, opts)
				checkFatal(t, err)
				repo.Free()
				reopened, err := OpenRepository(dstPath)
				checkFatal(t, err)
				defer reopened.Free()
				assertRepositoryFormats(t, reopened, oidType, refType)
				head, err := reopened.Head()
				checkFatal(t, err)
				defer head.Free()
				commit, err := reopened.LookupCommit(head.Target())
				checkFatal(t, err)
				commit.Free()
				if _, err := os.Stat(filepath.Join(dstPath, "README")); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

// TestCloneWithExternalHTTPUrl
func TestCloneWithExternalHTTPUrl(t *testing.T) {
	requiresNetwork(t)

	path, err := ioutil.TempDir("", "git2go")
	defer os.RemoveAll(path)

	// clone the repo
	url := "https://github.com/libgit2/TestGitRepository"
	_, err = Clone(url, path, &CloneOptions{})
	if err != nil {
		t.Fatal("cannot clone remote repo via https, error: ", err)
	}
}
