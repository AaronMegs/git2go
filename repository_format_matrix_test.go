package git

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

func TestRepositoryFormatMatrix(t *testing.T) {
	cases := []struct {
		name      string
		oidType   ObjectIdType
		refdbType RefdbType
		hexSize   int
	}{
		{"sha1-files", ObjectIdSHA1, RefdbFiles, 40},
		{"sha1-reftable", ObjectIdSHA1, RefdbReftable, 40},
		{"sha256-files", ObjectIdSHA256, RefdbFiles, 64},
		{"sha256-reftable", ObjectIdSHA256, RefdbReftable, 64},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.oidType == ObjectIdSHA256 && !IsSha256Supported() {
				t.Skip("linked libgit2 does not support SHA256 object ids")
			}
			if tc.refdbType == RefdbReftable && !IsReftableSupported() {
				t.Skip("linked libgit2 does not support reftable")
			}

			dir, err := ioutil.TempDir("", "git2go-format-matrix")
			checkFatal(t, err)
			defer os.RemoveAll(dir)
			repoPath := filepath.Join(dir, "repo")

			repo, err := InitRepositoryExt(repoPath, &RepositoryInitOptions{
				Flags:     RepositoryInitMkpath | RepositoryInitBare,
				OidType:   tc.oidType,
				RefdbType: tc.refdbType,
			})
			checkFatal(t, err)

			assertRepositoryFormats(t, repo, tc.oidType, tc.refdbType)
			commitID := seedCommit(t, repo)
			if got := len(commitID.String()); got != tc.hexSize {
				t.Fatalf("commit oid hex size = %d, want %d", got, tc.hexSize)
			}

			commit, err := repo.LookupCommit(commitID)
			checkFatal(t, err)
			branch, err := repo.CreateBranch("matrix", commit, false)
			checkFatal(t, err)
			branch.Free()
			commit.Free()
			repo.Free()

			reopened, err := OpenRepository(repoPath)
			checkFatal(t, err)
			defer reopened.Free()
			assertRepositoryFormats(t, reopened, tc.oidType, tc.refdbType)
			persisted, err := reopened.LookupBranch("matrix", BranchLocal)
			checkFatal(t, err)
			defer persisted.Free()
			if target := persisted.Target(); target == nil || !target.Equal(commitID) {
				t.Fatalf("reopened branch target = %v, want %v", target, commitID)
			}
		})
	}
}

func assertRepositoryFormats(t *testing.T, repo *Repository, oidType ObjectIdType, refdbType RefdbType) {
	t.Helper()
	if got := repo.OidType(); got != oidType {
		t.Fatalf("repository oid type = %v, want %v", got, oidType)
	}
	gotRefdbType, err := repo.RefStorageFormat()
	checkFatal(t, err)
	if gotRefdbType != refdbType {
		t.Fatalf("repository refdb type = %v, want %v", gotRefdbType, refdbType)
	}

	cfg, err := repo.Config()
	checkFatal(t, err)
	defer cfg.Free()
	if oidType == ObjectIdSHA256 {
		if got, err := cfg.LookupString("extensions.objectFormat"); err != nil || got != "sha256" {
			t.Fatalf("extensions.objectFormat = %q, err=%v; want sha256", got, err)
		}
	}
	if refdbType == RefdbReftable {
		if got, err := cfg.LookupString("extensions.refStorage"); err != nil || got != "reftable" {
			t.Fatalf("extensions.refStorage = %q, err=%v; want reftable", got, err)
		}
	}
}
