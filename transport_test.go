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

	expectedNames := []string{"HEAD", "refs/heads/master"}
	if len(remoteHeads) != len(expectedNames) {
		t.Fatalf("remote head count = %d, want %d: %v", len(remoteHeads), len(expectedNames), remoteHeads)
	}
	for i, head := range remoteHeads {
		if head.Name != expectedNames[i] {
			t.Errorf("remote head %d name = %q, want %q", i, head.Name, expectedNames[i])
		}
		if head.Id == nil || !head.Id.IsZero() {
			t.Errorf("remote head %d oid = %v, want zero oid", i, head.Id)
		}
	}
}
