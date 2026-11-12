package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/moveeeax/aws-ssm-pf/internal/awsclient"
	"github.com/moveeeax/aws-ssm-pf/internal/endpoint"
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

// A corrupt history file must not block an otherwise valid port forward:
// history is a convenience cache, and the command's real job is the tunnel.
func TestCorruptHistoryDoesNotBlockForward(t *testing.T) {
	isolateConfig(t)
	withFakeLister(t, []instance.Instance{{ID: "i-0333333333abcdef0", Name: "db-primary", PingStatus: "Online"}})

	path := history.DefaultPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not valid json"), 0o600); err != nil {
		t.Fatal(err)
	}

	out, errOut, err := run(t, "--remote-port", "5432", "--query", "db", "--no-input", "--aws-binary", "/usr/bin/true")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out, "Forwarding localhost:") {
		t.Errorf("forward should still proceed despite corrupt history:\n%s", out)
	}
	if !strings.Contains(errOut, "warning") || !strings.Contains(errOut, "history") {
		t.Errorf("expected a warning about the unreadable history file, got:\n%s", errOut)
	}

	// The corrupt file should have been replaced with a fresh, valid one.
	store, loadErr := history.Load(path)
	if loadErr != nil {
		t.Fatalf("history should be recoverable after the run: %v", loadErr)
	}
	if rec, ok := store.Recent(); !ok || rec.Target != "i-0333333333abcdef0" {
		t.Errorf("history not recorded after recovering from corruption: %+v (ok=%v)", rec, ok)
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

type fakeEndpointLister struct{ eps []endpoint.Endpoint }

func (f fakeEndpointLister) ListEndpoints(context.Context) ([]endpoint.Endpoint, error) {
	return f.eps, nil
}

func withFakeEndpoints(t *testing.T, eps []endpoint.Endpoint) {
	t.Helper()
	orig := endpointListerFactory
	endpointListerFactory = func(context.Context, string, string) (awsclient.EndpointLister, error) {
		return fakeEndpointLister{eps: eps}, nil
	}
	t.Cleanup(func() { endpointListerFactory = orig })
}

// noEndpoints fails the test if the RDS API would be contacted.
func noEndpoints(t *testing.T) {
	t.Helper()
	orig := endpointListerFactory
	endpointListerFactory = func(context.Context, string, string) (awsclient.EndpointLister, error) {
		t.Fatal("RDS must not be contacted")
		return nil, nil
	}
	t.Cleanup(func() { endpointListerFactory = orig })
}

// recordingAWS returns a stub aws CLI that writes its arguments, one per line,
// to the returned file, so tests can check the exact start-session call.
func recordingAWS(t *testing.T) (bin, argsFile string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("stub aws CLI is a shell script")
	}
	dir := t.TempDir()
	argsFile = filepath.Join(dir, "args")
	bin = filepath.Join(dir, "aws")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > '" + argsFile + "'\n"
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return bin, argsFile
}

func recordedArgs(t *testing.T, argsFile string) []string {
	t.Helper()
	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("stub aws CLI was not run: %v", err)
	}
	return strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
}

func argAfter(args []string, flag string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag {
			return args[i+1]
		}
	}
	return ""
}

func TestHostSwitchesToRemoteHostDocument(t *testing.T) {
	isolateConfig(t)
	noEndpoints(t)
	bin, argsFile := recordingAWS(t)

	out, _, err := run(t, "--target", "i-0123456789abcdef0", "--host", "db.internal.example:5432",
		"--local-port", "15432", "--aws-binary", bin)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out, "-> db.internal.example:5432 via i-0123456789abcdef0") {
		t.Errorf("unexpected output:\n%s", out)
	}
	args := recordedArgs(t, argsFile)
	if got := argAfter(args, "--document-name"); got != "AWS-StartPortForwardingSessionToRemoteHost" {
		t.Errorf("document = %q", got)
	}
	params := argAfter(args, "--parameters")
	for _, sub := range []string{`"host":["db.internal.example"]`, `"portNumber":["5432"]`, `"localPortNumber":[`} {
		if !strings.Contains(params, sub) {
			t.Errorf("parameters %q missing %s", params, sub)
		}
	}

	store, err := history.Load(history.DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	rec, ok := store.Recent()
	if !ok || rec.Host != "db.internal.example" || rec.RemotePort != 5432 || rec.Target != "i-0123456789abcdef0" {
		t.Errorf("history entry = %+v (ok=%v), want the host recorded", rec, ok)
	}
}

func TestHostWithSeparateRemotePort(t *testing.T) {
	isolateConfig(t)
	bin, argsFile := recordingAWS(t)
	if _, _, err := run(t, "--target", "i-0123456789abcdef0", "--host", "10.0.0.5", "-r", "6379", "--aws-binary", bin); err != nil {
		t.Fatalf("execute: %v", err)
	}
	params := argAfter(recordedArgs(t, argsFile), "--parameters")
	if !strings.Contains(params, `"host":["10.0.0.5"]`) || !strings.Contains(params, `"portNumber":["6379"]`) {
		t.Errorf("unexpected parameters %q", params)
	}
}

func TestHostWithoutPortRequiresRemotePort(t *testing.T) {
	isolateConfig(t)
	_, _, err := run(t, "--target", "i-0123456789abcdef0", "--host", "db.internal", "--aws-binary", "/usr/bin/true")
	if err == nil || !strings.Contains(err.Error(), "--remote-port is required") {
		t.Fatalf("expected a missing remote port error, got %v", err)
	}
}

func TestRecentRestoresHost(t *testing.T) {
	isolateConfig(t)
	store, _ := history.Load(history.DefaultPath())
	store.Add(history.Entry{Target: "i-0777777777abcdef0", Name: "bastion", Host: "orders.internal", RemotePort: 5432, LocalPort: 15432})
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	withFakeLister(t, nil)
	bin, argsFile := recordingAWS(t)

	out, _, err := run(t, "--recent", "--aws-binary", bin)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out, "-> orders.internal:5432 via i-0777777777abcdef0") {
		t.Errorf("unexpected output:\n%s", out)
	}
	if got := argAfter(recordedArgs(t, argsFile), "--document-name"); got != "AWS-StartPortForwardingSessionToRemoteHost" {
		t.Errorf("document = %q", got)
	}
}

// An explicit --host replaces the remembered host and its port: forwarding the
// remembered Postgres port to a different host would be a silent guess.
func TestRecentWithExplicitHostDropsRememberedPort(t *testing.T) {
	isolateConfig(t)
	store, _ := history.Load(history.DefaultPath())
	store.Add(history.Entry{Target: "i-0777777777abcdef0", Host: "orders.internal", RemotePort: 5432, LocalPort: 15432})
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	_, _, err := run(t, "--recent", "--host", "cache.internal", "--aws-binary", "/usr/bin/true")
	if err == nil || !strings.Contains(err.Error(), "--remote-port is required") {
		t.Fatalf("expected a missing remote port error, got %v", err)
	}
}

func TestRejectsMalformedHost(t *testing.T) {
	for _, bad := range []string{
		"--endpoint-url=http://attacker.example",
		"-db.internal:5432",
		"db.internal --profile evil",
		"db.internal;rm -rf /",
		"db.internal:notaport",
		"db.internal:70000",
	} {
		t.Run(bad, func(t *testing.T) {
			isolateConfig(t)
			listerFactory = func(context.Context, string, string) (awsclient.Lister, error) {
				t.Fatal("AWS must not be contacted for an invalid host")
				return nil, nil
			}
			t.Cleanup(func() {
				listerFactory = func(ctx context.Context, p, r string) (awsclient.Lister, error) {
					return awsclient.New(ctx, p, r)
				}
			})
			_, _, err := run(t, "--host", bad, "--remote-port", "5432", "--aws-binary", "/usr/bin/true")
			if err == nil {
				t.Fatalf("expected %q to be rejected", bad)
			}
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

func TestHostFlagConflicts(t *testing.T) {
	cases := [][]string{
		{"--host", "db.internal:5432", "--remote-port", "3306"},
		{"--host", "db.internal:5432", "--rds"},
		{"--rds", "--recent"},
		{"--rds-query", "orders", "-r", "5432"},
	}
	for _, args := range cases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			isolateConfig(t)
			noEndpoints(t)
			if _, _, err := run(t, append(args, "--target", "i-0123456789abcdef0", "--aws-binary", "/usr/bin/true")...); err == nil {
				t.Fatalf("expected %v to be rejected", args)
			}
		})
	}
}

func rdsSample() []endpoint.Endpoint {
	return []endpoint.Endpoint{
		{ID: "orders", Kind: endpoint.KindCluster, Engine: "aurora-postgresql", Status: "available", Host: "orders.cluster-abc.eu-central-1.rds.amazonaws.com", Port: 5432},
		{ID: "billing", Kind: endpoint.KindInstance, Engine: "mysql", Status: "available", Host: "billing.abc.eu-central-1.rds.amazonaws.com", Port: 3306},
	}
}

func TestRDSModeUsesEndpointHostAndPort(t *testing.T) {
	isolateConfig(t)
	withFakeEndpoints(t, rdsSample())
	withFakeLister(t, []instance.Instance{{ID: "i-0333333333abcdef0", Name: "bastion", PingStatus: "Online"}})
	bin, argsFile := recordingAWS(t)

	out, _, err := run(t, "--rds", "--rds-query", "billing", "--no-input", "--aws-binary", bin)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out, "-> billing.abc.eu-central-1.rds.amazonaws.com:3306 via bastion (i-0333333333abcdef0)") {
		t.Errorf("unexpected output:\n%s", out)
	}
	params := argAfter(recordedArgs(t, argsFile), "--parameters")
	if !strings.Contains(params, `"host":["billing.abc.eu-central-1.rds.amazonaws.com"]`) || !strings.Contains(params, `"portNumber":["3306"]`) {
		t.Errorf("unexpected parameters %q", params)
	}
	store, _ := history.Load(history.DefaultPath())
	if rec, _ := store.Recent(); rec.Host != "billing.abc.eu-central-1.rds.amazonaws.com" || rec.RemotePort != 3306 {
		t.Errorf("history entry = %+v", rec)
	}
}

func TestRDSModeRemotePortOverridesEndpointPort(t *testing.T) {
	isolateConfig(t)
	withFakeEndpoints(t, rdsSample())
	bin, argsFile := recordingAWS(t)

	if _, _, err := run(t, "--rds", "--rds-query", "orders", "--no-input", "-r", "6432",
		"--target", "i-0123456789abcdef0", "--aws-binary", bin); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if params := argAfter(recordedArgs(t, argsFile), "--parameters"); !strings.Contains(params, `"portNumber":["6432"]`) {
		t.Errorf("explicit --remote-port should win over the endpoint port: %q", params)
	}
}

func TestRDSModeAmbiguousWithNoInput(t *testing.T) {
	isolateConfig(t)
	withFakeEndpoints(t, rdsSample())
	_, _, err := run(t, "--rds", "--no-input", "--target", "i-0123456789abcdef0", "--aws-binary", "/usr/bin/true")
	if err == nil || !strings.Contains(err.Error(), "endpoints match") {
		t.Fatalf("expected an ambiguity error, got %v", err)
	}
}

// An endpoint address that would not pass --host validation must not reach
// the aws CLI just because it came from the RDS API.
func TestRDSModeRejectsBadEndpointHost(t *testing.T) {
	isolateConfig(t)
	withFakeEndpoints(t, []endpoint.Endpoint{{ID: "odd", Kind: endpoint.KindInstance, Host: "--debug", Port: 5432}})
	_, _, err := run(t, "--rds", "--no-input", "--target", "i-0123456789abcdef0", "--aws-binary", "/usr/bin/true")
	if err == nil {
		t.Fatal("expected the endpoint host to be rejected")
	}
}
