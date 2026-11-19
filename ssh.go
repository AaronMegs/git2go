package git

/*
#include <git2.h>

#include <git2/sys/credential.h>
*/
import "C"
import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/url"
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/crypto/ssh"
)

// RegisterManagedSSHTransport registers a Go-native implementation of an SSH
// transport that doesn't rely on any system libraries (e.g. libssh2).
//
// If Shutdown or ReInit are called, make sure that the smart transports are
// freed before it.
func RegisterManagedSSHTransport(protocol string) (*RegisteredSmartTransport, error) {
	return NewRegisteredSmartTransport(protocol, false, sshSmartSubtransportFactory)
}

func registerManagedSSH() error {
	globalRegisteredSmartTransports.Lock()
	defer globalRegisteredSmartTransports.Unlock()

	for _, protocol := range []string{"ssh", "ssh+git", "git+ssh"} {
		if _, ok := globalRegisteredSmartTransports.transports[protocol]; ok {
			continue
		}
		managed, err := newRegisteredSmartTransport(protocol, false, sshSmartSubtransportFactory, true)
		if err != nil {
			return fmt.Errorf("failed to register transport for %q: %v", protocol, err)
		}
		globalRegisteredSmartTransports.transports[protocol] = managed
	}
	return nil
}

func sshSmartSubtransportFactory(remote *Remote, transport *Transport) (SmartSubtransport, error) {
	return &sshSmartSubtransport{
		transport: transport,
	}, nil
}

type sshSmartSubtransport struct {
	transport *Transport

	lastAction    SmartServiceAction
	client        *ssh.Client
	session       *ssh.Session
	stdin         io.WriteCloser
	stdout        io.Reader
	currentStream *sshSmartSubtransportStream
}

type sshRemoteLocation struct {
	host string
	port string
	path string
}

func validateSSHComponent(name, value string, rejectOption bool) error {
	if value == "" {
		return fmt.Errorf("empty SSH %s", name)
	}
	if rejectOption && strings.HasPrefix(value, "-") {
		return fmt.Errorf("SSH %s %q is ambiguous with a command-line option", name, value)
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("SSH %s contains a control character", name)
		}
	}
	return nil
}

func parseSSHRemoteLocation(raw string) (sshRemoteLocation, error) {
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil {
			return sshRemoteLocation{}, err
		}
		switch u.Scheme {
		case "ssh", "ssh+git", "git+ssh":
		default:
			return sshRemoteLocation{}, fmt.Errorf("unsupported SSH URL scheme %q", u.Scheme)
		}
		if u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
			return sshRemoteLocation{}, errors.New("SSH repository URL must not contain a query, fragment, or opaque path")
		}

		location := sshRemoteLocation{host: u.Hostname(), port: u.Port(), path: u.Path}
		if location.port == "" {
			location.port = "22"
		}
		if err := validateSSHComponent("host", location.host, true); err != nil {
			return sshRemoteLocation{}, err
		}
		if err := validateSSHComponent("repository path", location.path, true); err != nil {
			return sshRemoteLocation{}, err
		}
		return location, nil
	}

	// Git's SCP-like form is [user@]host:path. It is not a URL, so its path is
	// kept literally (in particular, percent sequences are not decoded).
	hostStart := strings.LastIndex(raw, "@") + 1
	if hostStart >= len(raw) {
		return sshRemoteLocation{}, errors.New("invalid SCP-like SSH remote")
	}
	colon := -1
	if raw[hostStart] == '[' {
		closeBracket := strings.IndexByte(raw[hostStart:], ']')
		if closeBracket >= 0 {
			candidate := hostStart + closeBracket + 1
			if candidate < len(raw) && raw[candidate] == ':' {
				colon = candidate
			}
		}
	} else if relative := strings.IndexByte(raw[hostStart:], ':'); relative >= 0 {
		colon = hostStart + relative
	}
	if colon < 0 {
		return sshRemoteLocation{}, errors.New("invalid SCP-like SSH remote; expected [user@]host:path")
	}

	host := strings.Trim(raw[hostStart:colon], "[]")
	location := sshRemoteLocation{host: host, port: "22", path: raw[colon+1:]}
	if err := validateSSHComponent("host", location.host, true); err != nil {
		return sshRemoteLocation{}, err
	}
	if err := validateSSHComponent("repository path", location.path, true); err != nil {
		return sshRemoteLocation{}, err
	}
	return location, nil
}

func quotePOSIXShellArg(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func buildSSHRemoteCommand(action SmartServiceAction, path string) (string, error) {
	if err := validateSSHComponent("repository path", path, true); err != nil {
		return "", err
	}

	var service string
	switch action {
	case SmartServiceActionUploadpackLs, SmartServiceActionUploadpack:
		service = "git-upload-pack"
	case SmartServiceActionReceivepackLs, SmartServiceActionReceivepack:
		service = "git-receive-pack"
	default:
		return "", fmt.Errorf("unexpected action: %v", action)
	}
	return service + " " + quotePOSIXShellArg(path), nil
}

func (t *sshSmartSubtransport) Action(urlString string, action SmartServiceAction) (SmartSubtransportStream, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	location, err := parseSSHRemoteLocation(urlString)
	if err != nil {
		return nil, err
	}
	cmd, err := buildSSHRemoteCommand(action, location.path)
	if err != nil {
		return nil, err
	}

	switch action {
	case SmartServiceActionUploadpackLs, SmartServiceActionUploadpack:
		if t.currentStream != nil {
			if t.lastAction == SmartServiceActionUploadpackLs {
				return t.currentStream, nil
			}
			t.Close()
		}
	case SmartServiceActionReceivepackLs, SmartServiceActionReceivepack:
		if t.currentStream != nil {
			if t.lastAction == SmartServiceActionReceivepackLs {
				return t.currentStream, nil
			}
			t.Close()
		}
	}

	cred, err := t.transport.SmartCredentials("", CredentialTypeSSHKey|CredentialTypeSSHMemory)
	if err != nil {
		return nil, err
	}
	defer cred.Free()

	sshConfig, err := getSSHConfigFromCredential(cred)
	if err != nil {
		return nil, err
	}
	sshConfig.HostKeyCallback = func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		marshaledKey := key.Marshal()
		cert := &Certificate{
			Kind: CertificateHostkey,
			Hostkey: HostkeyCertificate{
				Kind:         HostkeySHA1 | HostkeyMD5 | HostkeySHA256 | HostkeyRaw,
				HashMD5:      md5.Sum(marshaledKey),
				HashSHA1:     sha1.Sum(marshaledKey),
				HashSHA256:   sha256.Sum256(marshaledKey),
				Hostkey:      marshaledKey,
				SSHPublicKey: key,
			},
		}

		return t.transport.SmartCertificateCheck(cert, true, hostname)
	}

	addr := net.JoinHostPort(location.host, location.port)
	t.client, err = ssh.Dial("tcp", addr, sshConfig)
	if err != nil {
		return nil, err
	}
	cleanup := true
	defer func() {
		if cleanup {
			t.closeConnection()
		}
	}()

	t.session, err = t.client.NewSession()
	if err != nil {
		return nil, err
	}

	t.stdin, err = t.session.StdinPipe()
	if err != nil {
		return nil, err
	}

	t.stdout, err = t.session.StdoutPipe()
	if err != nil {
		return nil, err
	}

	if err := t.session.Start(cmd); err != nil {
		return nil, err
	}
	cleanup = false

	t.lastAction = action
	t.currentStream = &sshSmartSubtransportStream{
		owner: t,
	}

	return t.currentStream, nil
}

func (t *sshSmartSubtransport) closeConnection() {
	if t.stdin != nil {
		_ = t.stdin.Close()
		t.stdin = nil
	}
	if t.session != nil {
		_ = t.session.Close()
		t.session = nil
	}
	if t.client != nil {
		_ = t.client.Close()
		t.client = nil
	}
	t.stdout = nil
}

func (t *sshSmartSubtransport) Close() error {
	t.currentStream = nil
	t.closeConnection()
	return nil
}

func (t *sshSmartSubtransport) Free() {
}

type sshSmartSubtransportStream struct {
	owner *sshSmartSubtransport
}

func (stream *sshSmartSubtransportStream) Read(buf []byte) (int, error) {
	return stream.owner.stdout.Read(buf)
}

func (stream *sshSmartSubtransportStream) Write(buf []byte) (int, error) {
	return stream.owner.stdin.Write(buf)
}

func (stream *sshSmartSubtransportStream) Free() {
}

func getSSHConfigFromCredential(cred *Credential) (*ssh.ClientConfig, error) {
	switch cred.Type() {
	case CredentialTypeSSHCustom:
		credSSHCustom := (*C.git_credential_ssh_custom)(unsafe.Pointer(cred.ptr))
		data, ok := pointerHandles.Get(credSSHCustom.payload).(*credentialSSHCustomData)
		if !ok {
			return nil, errors.New("unsupported custom SSH credentials")
		}
		return &ssh.ClientConfig{
			User: C.GoString(credSSHCustom.username),
			Auth: []ssh.AuthMethod{ssh.PublicKeys(data.signer)},
		}, nil
	}

	username, _, privatekey, passphrase, err := cred.GetSSHKey()
	if err != nil {
		return nil, err
	}

	var pemBytes []byte
	if cred.Type() == CredentialTypeSSHMemory {
		pemBytes = []byte(privatekey)
	} else {
		pemBytes, err = ioutil.ReadFile(privatekey)
		if err != nil {
			return nil, err
		}
	}

	var key ssh.Signer
	if passphrase != "" {
		key, err = ssh.ParsePrivateKeyWithPassphrase(pemBytes, []byte(passphrase))
		if err != nil {
			return nil, err
		}
	} else {
		key, err = ssh.ParsePrivateKey(pemBytes)
		if err != nil {
			return nil, err
		}
	}

	return &ssh.ClientConfig{
		User: username,
		Auth: []ssh.AuthMethod{ssh.PublicKeys(key)},
	}, nil
}
