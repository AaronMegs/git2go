package git

import (
	"testing"
)

// libgit2 hard-deprecated git_remote_callbacks.update_tips and only invokes it
// when update_refs is unset. git2go always installs update_refs, so
// UpdateTipsCallback has to be dispatched from update_refs to fire at all.
func TestUpdateTipsCallbackIsInvokedViaUpdateRefs(t *testing.T) {
	t.Parallel()

	upstream := createBareTestRepo(t)
	defer cleanupTestRepo(t, upstream)

	local := createTestRepo(t)
	defer cleanupTestRepo(t, local)

	remote, err := local.Remotes.Create("origin", upstream.Path())
	checkFatal(t, err)
	defer remote.Free()

	seedTestRepo(t, local)
	branchName := defaultBranchName(t, local)

	err = remote.Push([]string{"refs/heads/" + branchName}, nil)
	checkFatal(t, err)

	fetcher := createTestRepo(t)
	defer cleanupTestRepo(t, fetcher)

	fetchRemote, err := fetcher.Remotes.Create("origin", upstream.Path())
	checkFatal(t, err)
	defer fetchRemote.Free()

	type update struct {
		refname string
		a       *Oid
		b       *Oid
	}

	var tipsUpdates []update
	opts := FetchOptions{
		RemoteCallbacks: RemoteCallbacks{
			UpdateTipsCallback: func(refname string, a *Oid, b *Oid) error {
				tipsUpdates = append(tipsUpdates, update{refname: refname, a: a, b: b})
				return nil
			},
		},
	}
	err = fetchRemote.Fetch([]string{"refs/heads/*:refs/remotes/origin/*"}, &opts, "")
	checkFatal(t, err)

	if len(tipsUpdates) == 0 {
		t.Fatal("UpdateTipsCallback was never invoked")
	}
	for _, u := range tipsUpdates {
		if u.refname == "" {
			t.Error("UpdateTipsCallback received an empty refname")
		}
		if u.b == nil || u.b.IsZero() {
			t.Errorf("UpdateTipsCallback received no new id for %q", u.refname)
		}
	}
}

// When both callbacks are set, only UpdateRefsCallback runs, matching libgit2's
// documented precedence, and it receives the refspec the legacy callback lacks.
func TestUpdateRefsCallbackTakesPrecedenceOverUpdateTips(t *testing.T) {
	t.Parallel()

	upstream := createBareTestRepo(t)
	defer cleanupTestRepo(t, upstream)

	local := createTestRepo(t)
	defer cleanupTestRepo(t, local)

	remote, err := local.Remotes.Create("origin", upstream.Path())
	checkFatal(t, err)
	defer remote.Free()

	seedTestRepo(t, local)
	branchName := defaultBranchName(t, local)

	err = remote.Push([]string{"refs/heads/" + branchName}, nil)
	checkFatal(t, err)

	fetcher := createTestRepo(t)
	defer cleanupTestRepo(t, fetcher)

	fetchRemote, err := fetcher.Remotes.Create("origin", upstream.Path())
	checkFatal(t, err)
	defer fetchRemote.Free()

	refsCalls := 0
	tipsCalls := 0
	sawRefspec := false
	opts := FetchOptions{
		RemoteCallbacks: RemoteCallbacks{
			UpdateRefsCallback: func(refname string, a *Oid, b *Oid, spec *Refspec) error {
				refsCalls++
				if spec != nil {
					sawRefspec = true
				}
				return nil
			},
			UpdateTipsCallback: func(refname string, a *Oid, b *Oid) error {
				tipsCalls++
				return nil
			},
		},
	}
	err = fetchRemote.Fetch([]string{"refs/heads/*:refs/remotes/origin/*"}, &opts, "")
	checkFatal(t, err)

	if refsCalls == 0 {
		t.Fatal("UpdateRefsCallback was never invoked")
	}
	if tipsCalls != 0 {
		t.Errorf("UpdateTipsCallback ran %d times while UpdateRefsCallback was set", tipsCalls)
	}
	if !sawRefspec {
		t.Error("UpdateRefsCallback never received a refspec")
	}
}
