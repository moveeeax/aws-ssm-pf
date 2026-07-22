package session

import (
	"context"
	"slices"
	"strings"
	"testing"
)

func TestArgsBasic(t *testing.T) {
	o := Options{Target: "i-123", RemotePort: 5432, LocalPort: 15432}
	got := o.Args()
	want := []string{
		"ssm", "start-session",
		"--target", "i-123",
		"--document-name", "AWS-StartPortForwardingSession",
		"--parameters", "portNumber=5432,localPortNumber=15432",
	}
	if !slices.Equal(got, want) {
		t.Errorf("Args = %v\nwant %v", got, want)
	}
}

func TestArgsWithProfileRegionAndDocument(t *testing.T) {
	o := Options{
		Target: "i-abc", RemotePort: 6379, LocalPort: 6380,
		Profile: "prod", Region: "eu-central-1", DocumentName: "Custom-Doc",
	}
	got := strings.Join(o.Args(), " ")
	for _, sub := range []string{
		"--target i-abc",
		"--document-name Custom-Doc",
		"portNumber=6379,localPortNumber=6380",
		"--profile prod",
		"--region eu-central-1",
	} {
		if !strings.Contains(got, sub) {
			t.Errorf("args %q missing %q", got, sub)
		}
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name string
		o    Options
		ok   bool
	}{
		{"ok", Options{Target: "i-1", RemotePort: 80, LocalPort: 8080}, true},
		{"no target", Options{RemotePort: 80, LocalPort: 8080}, false},
		{"bad remote", Options{Target: "i-1", RemotePort: 0, LocalPort: 8080}, false},
		{"bad local", Options{Target: "i-1", RemotePort: 80, LocalPort: 70000}, false},
	}
	for _, c := range cases {
		err := c.o.Validate()
		if c.ok && err != nil {
			t.Errorf("%s: unexpected error %v", c.name, err)
		}
		if !c.ok && err == nil {
			t.Errorf("%s: expected error", c.name)
		}
	}
}

func TestRunFailsOnMissingBinary(t *testing.T) {
	o := Options{Target: "i-1", RemotePort: 80, LocalPort: 8080, AWSBinary: "definitely-not-a-real-binary-xyz"}
	if err := Run(context.Background(), o); err == nil {
		t.Fatal("expected error for missing binary")
	}
}

func TestCommandUsesConfiguredBinary(t *testing.T) {
	o := Options{Target: "i-1", RemotePort: 80, LocalPort: 8080, AWSBinary: "/usr/bin/true"}
	cmd := o.Command(context.Background())
	if cmd.Path != "/usr/bin/true" {
		t.Errorf("command path = %q, want /usr/bin/true", cmd.Path)
	}
	if !slices.Contains(cmd.Args, "start-session") {
		t.Errorf("args missing start-session: %v", cmd.Args)
	}
}
