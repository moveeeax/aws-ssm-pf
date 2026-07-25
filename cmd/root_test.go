package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/moveeeax/aws-ssm-pf/internal/awsclient"
	"github.com/moveeeax/aws-ssm-pf/internal/history"
	"github.com/moveeeax/aws-ssm-pf/internal/instance"
)

type fakeLister struct {
	insts []instance.Instance
	err   error
}

func (f fakeLister) List(context.Context) ([]instance.Instance, error) { return f.insts, f.err }

func withFakeLister(t *testing.T, insts []instance.Instance) {
	t.Helper()
	orig := listerFactory
	listerFactory = func(context.Context, string, string) (awsclient.Lister, error) {
		return fakeLister{insts: insts}, nil
	}
	t.Cleanup(func() { listerFactory = orig })
}

func isolateConfig(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	t.Cleanup(func() { now = time.Now })
}

func run(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	root := newRootCmd()
	var out, errb bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errb)
	root.SetIn(strings.NewReader(""))
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), errb.String(), err
}

func TestConnectFlowRecordsHistory(t *testing.T) {
	isolateConfig(t)
	withFakeLister(t, []instance.Instance{{ID: "i-0333333333abcdef0", Name: "db-primary", PingStatus: "Online"}})

	out, _, err := run(t, "--remote-port", "5432", "--query", "db", "--no-input", "--aws-binary", "/usr/bin/true")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out, "Forwarding localhost:") || !strings.Contains(out, "db-primary") {
		t.Errorf("unexpected output:\n%s", out)
	}

	store, err := history.Load(history.DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	rec, ok := store.Recent()
	if !ok || rec.Target != "i-0333333333abcdef0" || rec.RemotePort != 5432 {
		t.Errorf("history not recorded: %+v (ok=%v)", rec, ok)
	}
}

func TestRecentReusesTarget(t *testing.T) {
	isolateConfig(t)
	// Seed history directly.
	store, _ := history.Load(history.DefaultPath())
	store.Add(history.Entry{Target: "i-0777777777abcdef0", Name: "cache", RemotePort: 6379, LocalPort: 16379})
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	// No lister needed: --recent supplies the target directly.
	listerFactory = func(context.Context, string, string) (awsclient.Lister, error) {
		t.Fatal("lister should not be called with --recent + known target")
		return nil, nil
	}
	t.Cleanup(func() {
		listerFactory = func(ctx context.Context, p, r string) (awsclient.Lister, error) {
			return awsclient.New(ctx, p, r)
		}
	})

	out, _, err := run(t, "--recent", "--aws-binary", "/usr/bin/true")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out, "6379") {
		t.Errorf("expected reused remote port 6379 in output:\n%s", out)
	}
	// --recent is documented as reusing "the most recent target and ports",
	// plural: the remembered local port must come back too.
	if !strings.Contains(out, "localhost:16379") {
		t.Errorf("expected reused local port 16379 in output:\n%s", out)
	}
}

func TestRecentDoesNotOverrideExplicitTarget(t *testing.T) {
	isolateConfig(t)
	store, _ := history.Load(history.DefaultPath())
	store.Add(history.Entry{Target: "i-0777777777abcdef0", Name: "cache", RemotePort: 6379, LocalPort: 16379})
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	withFakeLister(t, nil) // the picker must not be reached either

	out, _, err := run(t, "--recent", "--target", "i-0999999999abcdef0", "--aws-binary", "/usr/bin/true")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out, "i-0999999999abcdef0") {
		t.Errorf("explicit --target was clobbered by --recent:\n%s", out)
	}
}

// Values from --target reach the aws CLI as argument-vector elements. Anything
// that is not an instance id — in particular anything that could be mistaken
// for another CLI option — must be rejected before any AWS call is made.
func TestRejectsMalformedTarget(t *testing.T) {
	for _, bad := range []string{
		"--endpoint-url=http://attacker.example",
		"i-0123456789abcdef0 --profile evil",
		"i-0123456789abcdef0; rm -rf /",
		"$(whoami)",
		"not-an-instance",
	} {
		t.Run(bad, func(t *testing.T) {
			isolateConfig(t)
			listerFactory = func(context.Context, string, string) (awsclient.Lister, error) {
				t.Fatal("AWS must not be contacted for an invalid target")
				return nil, nil
			}
			t.Cleanup(func() {
				listerFactory = func(ctx context.Context, p, r string) (awsclient.Lister, error) {
					return awsclient.New(ctx, p, r)
				}
			})
			_, _, err := run(t, "--target", bad, "--remote-port", "5432", "--aws-binary", "/usr/bin/true")
			if err == nil {
				t.Fatalf("expected %q to be rejected", bad)
			}
		})
	}
}

func TestRejectsOutOfRangePorts(t *testing.T) {
	cases := [][]string{
		{"--target", "i-0111111111abcdef0", "--remote-port", "70000"},
		{"--target", "i-0111111111abcdef0", "--remote-port", "-1"},
		{"--target", "i-0111111111abcdef0", "--remote-port", "5432", "--local-port", "70000"},
		{"--target", "i-0111111111abcdef0", "--remote-port", "5432", "--local-port", "-1"},
	}
	for _, args := range cases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			isolateConfig(t)
			_, _, err := run(t, append(args, "--aws-binary", "/usr/bin/true")...)
			if err == nil {
				t.Fatalf("expected %v to be rejected", args)
			}
			// A rejected invocation must not leave a tunnel in the history.
			store, loadErr := history.Load(history.DefaultPath())
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			if _, ok := store.Recent(); ok {
				t.Error("invalid invocation was recorded in history")
			}
		})
	}
}

func TestRemotePortRequired(t *testing.T) {
	isolateConfig(t)
	withFakeLister(t, []instance.Instance{{ID: "i-0111111111abcdef0", PingStatus: "Online"}})
	if _, _, err := run(t, "--target", "i-0111111111abcdef0"); err == nil {
		t.Fatal("expected error when --remote-port is missing")
	}
}

func TestListCommand(t *testing.T) {
	isolateConfig(t)
	withFakeLister(t, []instance.Instance{
		{ID: "i-0111111111abcdef0", Name: "web-01", PingStatus: "Online", Platform: "Amazon Linux", IPAddress: "10.0.0.1"},
	})
	out, _, err := run(t, "list")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	for _, sub := range []string{"INSTANCE ID", "i-0111111111abcdef0", "web-01", "Online"} {
		if !strings.Contains(out, sub) {
			t.Errorf("list output missing %q:\n%s", sub, out)
		}
	}
}

func TestListEmpty(t *testing.T) {
	isolateConfig(t)
	withFakeLister(t, nil)
	out, _, err := run(t, "list")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out, "no SSM-managed online instances") {
		t.Errorf("unexpected empty output: %q", out)
	}
}

// Ensure the isolated config path really points inside the temp dir.
func TestDefaultPathHonorsXDG(t *testing.T) {
	isolateConfig(t)
	p := history.DefaultPath()
	if !strings.HasPrefix(p, os.Getenv("XDG_CONFIG_HOME")) {
		t.Errorf("history path %q not under XDG_CONFIG_HOME", p)
	}
	if filepath.Base(p) != "history.json" {
		t.Errorf("unexpected history filename %q", p)
	}
}
