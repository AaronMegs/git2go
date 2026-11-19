package git

import (
	"io"
	"testing"
)

type testSmartSubtransport struct {
}

func (t *testSmartSubtransport) Action(url string, action SmartServiceAction) (SmartSubtransportStream, error) {
	return &testSmartSubtransportStream{}, nil
}

func (t *testSmartSubtransport) Close() error {
	return nil
}

func (t *testSmartSubtransport) Free() {
}

type testSmartSubtransportStream struct {
}

func (s *testSmartSubtransportStream) Read(buf []byte) (int, error) {
	payload := "" +
		"001e# service=git-upload-pack\n" +
		"0000005d0000000000000000000000000000000000000000 HEAD\x00symref=HEAD:refs/heads/master agent=libgit\n" +
		"003f0000000000000000000000000000000000000000 refs/heads/master\n" +
		"0000"

	return copy(buf, []byte(payload)), io.EOF
}

func (s *testSmartSubtransportStream) Write(buf []byte) (int, error) {
	return 0, io.EOF
}

func (s *testSmartSubtransportStream) Free() {
}

func TestRegisteredSmartTransportFreeFailurePreservesHandle(t *testing.T) {
	callback := func(*Remote, *Transport) (SmartSubtransport, error) {
		return &testSmartSubtransport{}, nil
	}
	transport, err := NewRegisteredSmartTransport("git2go-free-retry", true, callback)
	checkFatal(t, err)
	name, handle := transport.name, transport.handle
	defer func() {
		transport.name = name
		if err := transport.Free(); err != nil {
			t.Errorf("transport cleanup: %v", err)
		}
	}()

	transport.name = "git2go-free-unregistered"
	if err := transport.Free(); !IsErrorCode(err, ErrorCodeNotFound) {
		t.Fatalf("Free = %v, want not found", err)
	}
	if transport.handle != handle {
		t.Fatal("failed unregister lost the handle")
	}
	if value, ok := pointerHandles.GetOk(handle); !ok || value != transport {
		t.Fatal("failed unregister untracked the live transport")
	}

	transport.name = name
	checkFatal(t, transport.Free())
	if transport.handle != nil {
		t.Fatal("successful unregister did not clear the handle")
	}
	if _, ok := pointerHandles.GetOk(handle); ok {
		t.Fatal("successful unregister left a tracked handle")
	}
	checkFatal(t, transport.Free())
}

func TestTransport(t *testing.T) {
	t.Parallel()
	repo := createTestRepo(t)
	defer cleanupTestRepo(t, repo)

	callback := func(remote *Remote, transport *Transport) (SmartSubtransport, error) {
		return &testSmartSubtransport{}, nil
	}
	registeredSmartTransport, err := NewRegisteredSmartTransport("foo", true, callback)
	checkFatal(t, err)
	defer registeredSmartTransport.Free()

	remote, err := repo.Remotes.Create("test", "foo://bar")
	checkFatal(t, err)
	defer remote.Free()

	err = remote.ConnectFetch(nil, nil, nil)
	checkFatal(t, err)

	remoteHeads, err := remote.Ls()
	checkFatal(t, err)

	expectedRemoteHeads := []RemoteHead{
		{&Oid{}, "HEAD"},
		{&Oid{}, "refs/heads/master"},
	}
	// Compare field by field rather than with reflect.DeepEqual: in the
	// SHA256-capable build an Oid carries a type byte, so a zero-valued Oid{} is
	// not byte-identical to the all-zeroes SHA1 id libgit2 returns even though the
	// two are semantically the same id. Oid.Equal handles that.
	if len(expectedRemoteHeads) != len(remoteHeads) {
		t.Errorf("mismatched remote heads. expected %v, got %v", expectedRemoteHeads, remoteHeads)
	} else {
		for i, expected := range expectedRemoteHeads {
			actual := remoteHeads[i]
			if expected.Name != actual.Name || !expected.Id.Equal(actual.Id) {
				t.Errorf("mismatched remote head %d. expected %v (%s), got %v (%s)",
					i, expected.Name, expected.Id, actual.Name, actual.Id)
			}
		}
	}
}
