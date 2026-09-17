package session

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

const testTarget = "i-0123456789abcdef0"

func TestArgsBasic(t *testing.T) {
	o := Options{Target: testTarget, RemotePort: 5432, LocalPort: 15432}
	got := o.Args()
	want := []string{
		"ssm", "start-session",
		"--target", testTarget,
		"--document-name", "AWS-StartPortForwardingSession",
		"--parameters", "portNumber=5432,localPortNumber=15432",
	}
	if !slices.Equal(got, want) {
		t.Errorf("Args = %v\nwant %v", got, want)
	}
}

func TestArgsWithProfileRegionAndDocument(t *testing.T) {
	o := Options{
		Target: testTarget, RemotePort: 6379, LocalPort: 6380,
		Profile: "prod", Region: "eu-central-1", DocumentName: "Custom-Doc",
	}
	got := strings.Join(o.Args(), " ")
	for _, sub := range []string{
		"--target " + testTarget,
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
	base := func(mut func(*Options)) Options {
		o := Options{Target: testTarget, RemotePort: 80, LocalPort: 8080}
		mut(&o)
		return o
	}
	cases := []struct {
		name string
		o    Options
		ok   bool
	}{
		{"ok", base(func(*Options) {}), true},
		{"ok managed instance", base(func(o *Options) { o.Target = "mi-0123456789abcdef0" }), true},
		{"ok short instance id", base(func(o *Options) { o.Target = "i-0abc1234" }), true},
		{"no target", base(func(o *Options) { o.Target = "" }), false},
		{"bad remote", base(func(o *Options) { o.RemotePort = 0 }), false},
		{"negative remote", base(func(o *Options) { o.RemotePort = -1 }), false},
		{"bad local", base(func(o *Options) { o.LocalPort = 70000 }), false},

		// Everything below would otherwise be handed straight to the aws CLI as
		// an argument vector element. None of it may be accepted.
		{"target that looks like a flag", base(func(o *Options) { o.Target = "--endpoint-url=http://attacker.example" }), false},
		{"target with spaces", base(func(o *Options) { o.Target = "i-0123456789abcdef0 --profile evil" }), false},
		{"target with shell metacharacters", base(func(o *Options) { o.Target = "i-0123456789abcdef0; rm -rf /" }), false},
		{"target with newline", base(func(o *Options) { o.Target = "i-0123456789abcdef0\n--debug" }), false},
		{"non-instance target", base(func(o *Options) { o.Target = "i-demo" }), false},
		{"document that looks like a flag", base(func(o *Options) { o.DocumentName = "--debug" }), false},
		{"document with spaces", base(func(o *Options) { o.DocumentName = "Doc --profile evil" }), false},
		{"profile that looks like a flag", base(func(o *Options) { o.Profile = "--endpoint-url=http://attacker.example" }), false},
		{"profile with spaces", base(func(o *Options) { o.Profile = "prod --debug" }), false},
		{"region that looks like a flag", base(func(o *Options) { o.Region = "--endpoint-url=http://attacker.example" }), false},
		{"region of only dashes and letters", base(func(o *Options) { o.Region = "--debug" }), false},
		{"region with spaces", base(func(o *Options) { o.Region = "eu-central-1 --debug" }), false},
		{"profile of only dashes and letters", base(func(o *Options) { o.Profile = "--debug" }), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.o.Validate()
			if c.ok && err != nil {
				t.Errorf("unexpected error %v", err)
			}
			if !c.ok && err == nil {
				t.Errorf("expected error for %+v", c.o)
			}
		})
	}
}

func TestValidatePortRange(t *testing.T) {
	for _, p := range []int{-1, 0, 65536, 99999} {
		if err := ValidatePort("remote", p); err == nil {
			t.Errorf("expected error for port %d", p)
		}
	}
	for _, p := range []int{1, 22, 5432, 65535} {
		if err := ValidatePort("remote", p); err != nil {
			t.Errorf("unexpected error for port %d: %v", p, err)
		}
	}
}

func TestRunFailsOnMissingBinary(t *testing.T) {
	o := Options{Target: testTarget, RemotePort: 80, LocalPort: 8080, AWSBinary: "definitely-not-a-real-binary-xyz"}
	if err := Run(context.Background(), o); err == nil {
		t.Fatal("expected error for missing binary")
	}
}

func TestCommandUsesConfiguredBinary(t *testing.T) {
	o := Options{Target: testTarget, RemotePort: 80, LocalPort: 8080, AWSBinary: "/usr/bin/true"}
	cmd := o.Command(context.Background())
	if cmd.Path != "/usr/bin/true" {
		t.Errorf("command path = %q, want /usr/bin/true", cmd.Path)
	}
	if !slices.Contains(cmd.Args, "start-session") {
		t.Errorf("args missing start-session: %v", cmd.Args)
	}
}

// writeFakeAWS creates a stub that behaves like `aws ssm start-session`: it
// forks a long-lived grandchild (standing in for session-manager-plugin) and
// then blocks. The grandchild's pid is written to pidFile.
func writeFakeAWS(t *testing.T, pidFile string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-aws")
	script := "#!/bin/sh\n" +
		"sleep 120 &\n" +
		"echo $! > " + strconv.Quote(pidFile) + "\n" +
		"wait\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

// probe reports whether pid is still a live process.
func probe(pid int) bool {
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "pid=").Output()
	return err == nil && strings.TrimSpace(string(out)) != ""
}

// TestRunKillsOrphanedGrandchild is the regression test for a leaked tunnel:
// cancelling the context must take down session-manager-plugin, not just the
// aws CLI that spawned it. Without process-group teardown the grandchild
// survives, still holding the local port and the SSM session open.
func TestRunKillsOrphanedGrandchild(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process-group teardown is POSIX-only")
	}
	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")
	fake := writeFakeAWS(t, pidFile)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Options{
			Target: testTarget, RemotePort: 5432, LocalPort: 15432, AWSBinary: fake,
		})
	}()

	var pid int
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(pidFile)
		if err == nil && strings.TrimSpace(string(raw)) != "" {
			pid, err = strconv.Atoi(strings.TrimSpace(string(raw)))
			if err == nil && pid > 0 {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("stub aws CLI never reported a grandchild pid")
	}
	if !probe(pid) {
		t.Fatalf("grandchild %d was not running to begin with", pid)
	}

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("cancellation should be a clean shutdown, got %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after cancellation")
	}

	// Give the kernel a moment to reap; the process must be gone, not merely
	// re-parented to init.
	for i := 0; i < 100 && probe(pid); i++ {
		time.Sleep(20 * time.Millisecond)
	}
	if probe(pid) {
		t.Fatalf("grandchild %d survived cancellation: the tunnel would still be live", pid)
	}
}

// TestRunReportsRealFailures makes sure the cancellation carve-out above does
// not swallow a genuine non-zero exit.
func TestRunReportsRealFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("relies on /usr/bin/false")
	}
	err := Run(context.Background(), Options{
		Target: testTarget, RemotePort: 5432, LocalPort: 15432, AWSBinary: "/usr/bin/false",
	})
	if err == nil {
		t.Fatal("expected the aws CLI's non-zero exit to be reported")
	}
}
