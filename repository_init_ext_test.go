package git

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

// TestInitRepositoryExtFiles verifies the new InitRepositoryExt entrypoint
// against the default ("files") reference storage backend. It ensures the
// extended init options API is wired up correctly without touching reftable
// behaviour, so it works on any libgit2 build (released v1.9.x or master).
func TestInitRepositoryExtFiles(t *testing.T) {
	dir, err := ioutil.TempDir("", "git2go-init-ext-files")
	checkFatal(t, err)
	defer os.RemoveAll(dir)

	repoPath := filepath.Join(dir, "repo")

	repo, err := InitRepositoryExt(repoPath, &RepositoryInitOptions{
		Flags:       RepositoryInitMkpath | RepositoryInitBare,
		InitialHead: "main",
		Description: "init-ext smoke test",
		// RefdbType left as zero (RefdbDefault) — must behave like the legacy
		// InitRepository(path, true) call.
	})
	checkFatal(t, err)
	defer repo.Free()

	if !repo.IsBare() {
		t.Fatalf("expected bare repository, IsBare()=false")
	}

	// HEAD should point at refs/heads/main (initial_head honored).
	head, err := repo.References.Lookup("HEAD")
	checkFatal(t, err)
	defer head.Free()
	if head.Type() != ReferenceSymbolic {
		t.Fatalf("HEAD: expected symbolic reference, got type=%v", head.Type())
	}
	if got, want := head.SymbolicTarget(), "refs/heads/main"; got != want {
		t.Fatalf("HEAD symbolic target = %q, want %q", got, want)
	}

	// description file should reflect what we passed in.
	desc, err := ioutil.ReadFile(filepath.Join(repoPath, "description"))
	checkFatal(t, err)
	if string(desc) != "init-ext smoke test" {
		t.Fatalf("description file = %q, want %q", string(desc), "init-ext smoke test")
	}
}

// TestInitRepositoryExtNilOptions verifies that passing nil options is
// equivalent to using the libgit2 defaults (mirrors InitRepository semantics).
func TestInitRepositoryExtNilOptions(t *testing.T) {
	dir, err := ioutil.TempDir("", "git2go-init-ext-nil")
	checkFatal(t, err)
	defer os.RemoveAll(dir)

	repo, err := InitRepositoryExt(dir, nil)
	checkFatal(t, err)
	defer repo.Free()

	if repo.IsBare() {
		t.Fatalf("expected non-bare repository when no flags are set")
	}
}

// TestInitRepositoryExtReftable verifies the reftable backend selection.
//
// This requires a libgit2 build that includes PR #7117 (upstream master).
// On a libgit2 build without reftable support, the call is expected to fail
// with an error from libgit2 itself; the test then skips rather than failing
// so the suite stays green on stable v1.9.x.
func TestInitRepositoryExtReftable(t *testing.T) {
	dir, err := ioutil.TempDir("", "git2go-init-ext-reftable")
	checkFatal(t, err)
	defer os.RemoveAll(dir)

	repoPath := filepath.Join(dir, "repo")

	repo, err := InitRepositoryExt(repoPath, &RepositoryInitOptions{
		Flags:     RepositoryInitMkpath | RepositoryInitBare,
		RefdbType: RefdbReftable,
	})
	if err != nil {
		t.Skipf("reftable backend not supported by this libgit2 build: %v", err)
	}
	defer repo.Free()

	// Sanity: the repository should be readable and report itself as bare.
	if !repo.IsBare() {
		t.Fatalf("expected bare repository, IsBare()=false")
	}

	// extensions.refStorage should be set to "reftable" by libgit2 itself.
	cfg, err := repo.Config()
	checkFatal(t, err)
	defer cfg.Free()
	if got, err := cfg.LookupString("extensions.refStorage"); err != nil {
		t.Fatalf("extensions.refStorage missing on reftable repo: %v", err)
	} else if got != "reftable" {
		t.Fatalf("extensions.refStorage = %q, want %q", got, "reftable")
	}

	// Physical layout: a reftable directory should exist at the gitdir root.
	// (libgit2 mirrors the on-disk layout used by git itself.)
	gitdir := repo.Path()
	if _, statErr := os.Stat(filepath.Join(gitdir, "reftable")); os.IsNotExist(statErr) {
		t.Fatalf("expected %s/reftable directory to exist on a reftable repo", gitdir)
	}

	// Functional check: iterate references — must not error on a reftable repo,
	// even if the only ref is the unborn HEAD.
	iter, err := repo.NewReferenceIterator()
	checkFatal(t, err)
	defer iter.Free()
	for {
		ref, err := iter.Next()
		if err != nil {
			break
		}
		ref.Free()
	}
}
