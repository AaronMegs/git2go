//go:build !libgit2_reftable
// +build !libgit2_reftable

package git

import "testing"

type stableInitializingRefdbBackend struct {
	*countingRefdbBackend
}

func (b *stableInitializingRefdbBackend) Init(initialHead *string, mode RepositoryInitMode, flags RefdbBackendInitFlag) error {
	return nil
}

func TestRefdbBackendInitCapabilityRequiresMainABI(t *testing.T) {
	impl := &stableInitializingRefdbBackend{countingRefdbBackend: &countingRefdbBackend{}}
	backend, err := NewRefdbBackendFromInterface(impl)
	if backend != nil {
		backend.Free()
		t.Fatal("expected initializer backend construction to fail without libgit2_reftable tag")
	}
	if !IsErrorCode(err, ErrorCodeInvalid) {
		t.Fatalf("initializer backend error = %v, want ErrorCodeInvalid", err)
	}
}
