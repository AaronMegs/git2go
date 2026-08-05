package git

import (
	"strings"
	"testing"

	"github.com/google/shlex"
)

func TestQuotePOSIXShellArg(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"/repo.git", "'/repo.git'"},
		{"/space dir/repo.git", "'/space dir/repo.git'"},
		{`/repo\name.git`, `'/repo\name.git'`},
		{"/repo's.git", "'/repo'\\''s.git'"},
		{"/$(id)", "'/$(id)'"},
		{"/`id`", "'/`id`'"},
		{"/a;b&c|d#e!f", "'/a;b&c|d#e!f'"},
		{"/中文/仓库.git", "'/中文/仓库.git'"},
	}

	for _, test := range tests {
		if got := quotePOSIXShellArg(test.input); got != test.want {
			t.Errorf("quotePOSIXShellArg(%q) = %q, want %q", test.input, got, test.want)
		}
	}
}

func TestBuildSSHRemoteCommandDoesNotAllowArgumentInjection(t *testing.T) {
	path := "/repo'; id; #"
	command, err := buildSSHRemoteCommand(SmartServiceActionUploadpackLs, path)
	if err != nil {
		t.Fatal(err)
	}
	if want := "git-upload-pack '/repo'\\''; id; #'"; command != want {
		t.Fatalf("command = %q, want %q", command, want)
	}

	args, err := shlex.Split(command)
	if err != nil {
		t.Fatal(err)
	}
	if len(args) != 2 || args[0] != "git-upload-pack" || args[1] != path {
		t.Fatalf("command split into %#v, want one fixed service and one literal path", args)
	}
}

func TestBuildSSHRemoteCommandActions(t *testing.T) {
	tests := []struct {
		action SmartServiceAction
		want   string
	}{
		{SmartServiceActionUploadpackLs, "git-upload-pack '/repo'"},
		{SmartServiceActionUploadpack, "git-upload-pack '/repo'"},
		{SmartServiceActionReceivepackLs, "git-receive-pack '/repo'"},
		{SmartServiceActionReceivepack, "git-receive-pack '/repo'"},
	}
	for _, test := range tests {
		got, err := buildSSHRemoteCommand(test.action, "/repo")
		if err != nil {
			t.Fatal(err)
		}
		if got != test.want {
			t.Errorf("action %v command = %q, want %q", test.action, got, test.want)
		}
	}
	if _, err := buildSSHRemoteCommand(SmartServiceAction(999), "/repo"); err == nil {
		t.Error("unexpected action was accepted")
	}
}

func TestParseSSHRemoteLocation(t *testing.T) {
	tests := []struct {
		raw  string
		host string
		port string
		path string
	}{
		{"ssh://example.com/repo.git", "example.com", "22", "/repo.git"},
		{"ssh+git://user@example.com:2222/space%20dir/repo.git", "example.com", "2222", "/space dir/repo.git"},
		{"git+ssh://example.com/repo%27s.git", "example.com", "22", "/repo's.git"},
		{"ssh://example.com/repo%2527s.git", "example.com", "22", "/repo%27s.git"},
		{"ssh://[::1]:2222/repo.git", "::1", "2222", "/repo.git"},
		{"git@example.com:path/to/repo.git", "example.com", "22", "path/to/repo.git"},
		{"example.com:repo%20literal", "example.com", "22", "repo%20literal"},
		{"git@[::1]:repo.git", "::1", "22", "repo.git"},
	}

	for _, test := range tests {
		got, err := parseSSHRemoteLocation(test.raw)
		if err != nil {
			t.Errorf("parseSSHRemoteLocation(%q): %v", test.raw, err)
			continue
		}
		if got.host != test.host || got.port != test.port || got.path != test.path {
			t.Errorf("parseSSHRemoteLocation(%q) = %#v, want host=%q port=%q path=%q", test.raw, got, test.host, test.port, test.path)
		}
	}
}

func TestParseSSHRemoteLocationRejectsUnsafeInput(t *testing.T) {
	invalid := []string{
		"ssh://example.com",
		"ssh://-oProxyCommand=bad/repo",
		"ssh://example.com/repo?query=1",
		"ssh://example.com/repo#fragment",
		"ssh://example.com/%00",
		"ssh://example.com/%09",
		"ssh://example.com/%0A",
		"ssh://example.com/%0D",
		"ssh://example.com/%1B",
		"ssh://example.com/%7F",
		"ssh://example.com/%GG",
		"example.com:-oProxyCommand=bad",
		"example.com:",
		"not-an-ssh-remote",
	}
	for _, raw := range invalid {
		if _, err := parseSSHRemoteLocation(raw); err == nil {
			t.Errorf("unsafe SSH remote %q was accepted", raw)
		}
	}

	for b := byte(0); b < 0x20; b++ {
		path := "repo" + string([]byte{b}) + "name"
		if err := validateSSHComponent("repository path", path, true); err == nil {
			t.Errorf("control byte 0x%02x was accepted", b)
		}
	}
	if err := validateSSHComponent("repository path", "repo"+string(rune(0x7f))+"name", true); err == nil {
		t.Error("DEL was accepted")
	}
}

func TestSSHCommandQuotingRoundTrip(t *testing.T) {
	paths := []string{
		"/normal/repo.git",
		"/space dir/repo.git",
		`/backslash\repo.git`,
		"/single'quote/repo.git",
		"/$(command);`substitution`&pipe|hash#bang!",
		"/中文/仓库.git",
	}
	for _, path := range paths {
		command, err := buildSSHRemoteCommand(SmartServiceActionReceivepackLs, path)
		if err != nil {
			t.Fatal(err)
		}
		args, err := shlex.Split(command)
		if err != nil {
			t.Fatal(err)
		}
		if len(args) != 2 || args[0] != "git-receive-pack" || args[1] != path {
			t.Errorf("path %q round-tripped as %#v", path, args)
		}
		if strings.Count(command, "git-receive-pack") != 1 {
			t.Errorf("service name unexpectedly repeated in %q", command)
		}
	}
}
