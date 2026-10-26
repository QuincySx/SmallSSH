package accesspolicy

import (
	"errors"
	"strings"
	"testing"

	"sshclient/internal/approval"
)

func TestDefaultPolicyLimits(t *testing.T) {
	var p Policy
	for _, op := range []approval.Operation{
		{Kind: "put", Path: "/etc/hosts"}, {Kind: "remove", Path: "/usr/bin/tool"},
		{Kind: "rename", Path: "/srv/safe", Destination: "/etc/new"},
		{Kind: "mkdir", Path: "/"}, {Kind: "replace", Path: "/etc"},
	} {
		if err := p.Check(op); !errors.Is(err, ErrProtected) {
			t.Errorf("%+v: %v", op, err)
		}
	}
	for _, kind := range []string{"exec", "shell", "docker-terminal", "compose-update", "chmod", "file-attributes", "put-resume", "unknown"} {
		if err := p.Check(approval.Operation{Kind: kind, Path: "/srv/safe"}); !errors.Is(err, ErrUncontrolled) {
			t.Errorf("%s: %v", kind, err)
		}
	}
	for _, op := range []approval.Operation{
		{Kind: "mkdir", Path: "/etc2"}, {Kind: "put", Path: "/srv/safe"},
		{Kind: "replace", Path: "/srv/safe"}, {Kind: "remove", Path: "/srv/safe"},
		{Kind: "rename", Path: "/srv/a", Destination: "/srv/b"},
		{Kind: "list", Path: "/etc"}, {Kind: "get", Path: "/etc/hosts"},
		{Kind: "monitor"}, {Kind: "docker-logs"}, {Kind: "compose-plan"}, {Kind: "grant"},
	} {
		if err := p.Check(op); err != nil {
			t.Errorf("%+v: %v", op, err)
		}
	}
}

func TestDisableAlwaysWinsAndOptOut(t *testing.T) {
	for _, kind := range []string{"exec", "grant", "get", "put", "monitor", "shell", "unknown"} {
		if err := (Policy{Disabled: true, Unrestricted: true}).Check(approval.Operation{Kind: kind}); !errors.Is(err, ErrDisabled) {
			t.Errorf("%s: %v", kind, err)
		}
	}
	for _, kind := range []string{"exec", "shell", "chmod", "file-attributes", "put-resume", "compose-update"} {
		if err := (Policy{Unrestricted: true}).Check(approval.Operation{Kind: kind, Path: "/srv/protected"}); err != nil {
			t.Errorf("%s: %v", kind, err)
		}
	}
}

func TestInvalidPathsFailClosed(t *testing.T) {
	for _, v := range []string{"", "relative", "/srv/safe/../protected", "/srv/./safe", "/srv//safe", "/srv/safe/", "/srv/ctrl\x01", "/srv/line\n", "/" + strings.Repeat("a", 4096), string([]byte{'/', 255})} {
		if err := (Policy{}).Check(approval.Operation{Kind: "put", Path: v}); !errors.Is(err, ErrProtected) {
			t.Errorf("accepted %q: %v", v, err)
		}
		if v != "" {
			if err := (Policy{ProtectedPaths: []string{v}}).Validate(); !errors.Is(err, ErrInvalidPolicy) {
				t.Errorf("invalid policy %q: %v", v, err)
			}
		}
	}
	if err := (Policy{ProtectedPaths: make([]string, 33)}).Validate(); err == nil {
		t.Fatal("accepted too many paths")
	}
}

func TestCustomPathsAndCopies(t *testing.T) {
	p := Policy{ProtectedPaths: []string{"/srv/protected"}}
	for _, v := range []string{"/srv", "/srv/protected", "/srv/protected/a"} {
		if err := p.Check(approval.Operation{Kind: "remove", Path: v}); !errors.Is(err, ErrProtected) {
			t.Errorf("accepted %s", v)
		}
	}
	if err := p.Check(approval.Operation{Kind: "rename", Path: "/srv/safe", Destination: "/srv/protected/new"}); !errors.Is(err, ErrProtected) {
		t.Fatal("rename destination not protected", err)
	}
	if err := p.Check(approval.Operation{Kind: "put", Path: "/srv/protected2"}); err != nil {
		t.Fatal(err)
	}
	clone, paths := p.Clone(), p.Paths()
	clone.ProtectedPaths[0], paths[0] = "/changed", "/changed"
	if p.ProtectedPaths[0] != "/srv/protected" {
		t.Fatal("policy slice exposed")
	}
	defaults := (Policy{}).Paths()
	defaults[0] = "/changed"
	if (Policy{}).Paths()[0] != "/etc" {
		t.Fatal("default slice exposed")
	}
	if err := (Policy{ProtectedPaths: []string{"/"}}).Check(approval.Operation{Kind: "put", Path: "/srv/safe"}); !errors.Is(err, ErrProtected) {
		t.Fatal(err)
	}
	for err, want := range map[error]string{ErrDisabled: "automation_disabled", ErrProtected: "protected_path", ErrUncontrolled: "uncontrolled_operation", ErrInvalidPolicy: "invalid_policy"} {
		if Reason(errors.Join(errors.New("context"), err)) != want {
			t.Fatal(err)
		}
	}
}
