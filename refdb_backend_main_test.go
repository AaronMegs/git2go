//go:build libgit2_reftable
// +build libgit2_reftable

package git

import "testing"

type initializingRefdbBackend struct {
	*countingRefdbBackend
	initCalls   int
	initialHead *string
	mode        RepositoryInitMode
	flags       RefdbBackendInitFlag
}

func (b *initializingRefdbBackend) Init(initialHead *string, mode RepositoryInitMode, flags RefdbBackendInitFlag) error {
	b.initCalls++
	b.initialHead = initialHead
	b.mode = mode
	b.flags = flags
	return nil
}

func TestRefdbBackendOptionalCapabilityPointers(t *testing.T) {
	base := &countingRefdbBackend{}
	backend, err := NewRefdbBackendFromInterface(base)
	checkFatal(t, err)
	if got := refdbBackendCapabilities(backend); got != 0 {
		t.Fatalf("base backend capabilities = %#x, want 0", got)
	}
	backend.Free()

	initializer := &initializingRefdbBackend{countingRefdbBackend: &countingRefdbBackend{}}
	backend, err = NewRefdbBackendFromInterface(initializer)
	checkFatal(t, err)
	if got := refdbBackendCapabilities(backend); got != refdbBackendCapabilityInit {
		t.Fatalf("initializer capabilities = %#x, want %#x", got, refdbBackendCapabilityInit)
	}
	initialHead := "refs/heads/main"
	flags := RefdbBackendInitIsWorktree | RefdbBackendInitForceHead
	if err := invokeRefdbBackendInit(backend, &initialHead, RepositoryInitMode(0750), flags); err != nil {
		t.Fatalf("invokeRefdbBackendInit failed: %v", err)
	}
	if initializer.initCalls != 1 || initializer.initialHead == nil || *initializer.initialHead != initialHead {
		t.Fatalf("init callback calls=%d head=%v, want 1/%q", initializer.initCalls, initializer.initialHead, initialHead)
	}
	if initializer.mode != RepositoryInitMode(0750) || initializer.flags != flags {
		t.Fatalf("init callback mode=%#o flags=%#x, want %#o/%#x", initializer.mode, initializer.flags, RepositoryInitMode(0750), flags)
	}
	backend.Free()

	compressor := &compressingRefdbBackend{countingRefdbBackend: &countingRefdbBackend{}}
	backend, err = NewRefdbBackendFromInterface(compressor)
	checkFatal(t, err)
	if got := refdbBackendCapabilities(backend); got != refdbBackendCapabilityCompress {
		t.Fatalf("compressor capabilities = %#x, want %#x", got, refdbBackendCapabilityCompress)
	}
	backend.Free()

	locker := &transactionRefdbBackend{countingRefdbBackend: &countingRefdbBackend{}}
	backend, err = NewRefdbBackendFromInterface(locker)
	checkFatal(t, err)
	if got := refdbBackendCapabilities(backend); got != refdbBackendCapabilityLock {
		t.Fatalf("locker capabilities = %#x, want %#x", got, refdbBackendCapabilityLock)
	}
	backend.Free()
}
