// Package session builds and runs the underlying `aws ssm start-session`
// port-forward invocation.
package session

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"time"
)

// DefaultDocument is the managed SSM document for port forwarding.
const DefaultDocument = "AWS-StartPortForwardingSession"

// shutdownGrace is how long the aws CLI and session-manager-plugin get to tear
// the tunnel down politely after cancellation before they are killed outright.
const shutdownGrace = 5 * time.Second

// Every value below reaches the aws CLI as a separate element of the argument
// vector (never a shell string), so there is no shell metacharacter risk. The
// patterns exist to stop a value from *looking* like another CLI option — a
// target of "--endpoint-url=http://attacker" must never be accepted — and to
// fail fast on typos instead of surfacing an opaque error from the CLI.
var (
	// EC2 instance ids are "i-" plus 8 or 17 hex digits; hybrid activations use
	// the "mi-" prefix. The bounds are deliberately loose, the charset is not.
	targetPattern = regexp.MustCompile(`^(i|mi)-[0-9a-fA-F]{8,32}$`)
	// SSM document names, per the DocumentName API constraint. The leading
	// character class keeps a value like "--debug" from being accepted.
	documentPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.:/-]{2,127}$`)
	// AWS region codes, e.g. eu-central-1.
	regionPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
	// Shared-config profile names. Leading "-" is excluded by the first class.
	profilePattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._@=+:/-]{0,127}$`)
)

// Options describes a single port-forward session.
type Options struct {
	Target       string
	DocumentName string
	RemotePort   int
	LocalPort    int
	Profile      string
	Region       string
	// AWSBinary overrides the aws CLI path (useful for tests). Defaults to "aws".
	AWSBinary string
}

func (o Options) binary() string {
	if o.AWSBinary != "" {
		return o.AWSBinary
	}
	return "aws"
}

func (o Options) document() string {
	if o.DocumentName != "" {
		return o.DocumentName
	}
	return DefaultDocument
}

// Args returns the argument vector passed to the aws CLI (excluding the binary).
func (o Options) Args() []string {
	args := []string{
		"ssm", "start-session",
		"--target", o.Target,
		"--document-name", o.document(),
		"--parameters", fmt.Sprintf("portNumber=%d,localPortNumber=%d", o.RemotePort, o.LocalPort),
	}
	if o.Profile != "" {
		args = append(args, "--profile", o.Profile)
	}
	if o.Region != "" {
		args = append(args, "--region", o.Region)
	}
	return args
}

// ValidateTarget checks that s is a plausible SSM target id. It is exported so
// the CLI can reject a bad --target before doing any AWS work.
func ValidateTarget(s string) error {
	if s == "" {
		return fmt.Errorf("target is required")
	}
	if !targetPattern.MatchString(s) {
		return fmt.Errorf("invalid target %q: expected an instance id such as i-0123456789abcdef0 or mi-0123456789abcdef0", s)
	}
	return nil
}

// ValidatePort checks that port is a usable TCP port. name is used in the error
// message ("remote", "local").
func ValidatePort(name string, port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("invalid %s port %d: must be between 1 and 65535", name, port)
	}
	return nil
}

// Validate checks the options before spawning the CLI.
func (o Options) Validate() error {
	if err := ValidateTarget(o.Target); err != nil {
		return err
	}
	if err := ValidatePort("remote", o.RemotePort); err != nil {
		return err
	}
	if err := ValidatePort("local", o.LocalPort); err != nil {
		return err
	}
	if !documentPattern.MatchString(o.document()) {
		return fmt.Errorf("invalid document name %q", o.document())
	}
	if o.Profile != "" && !profilePattern.MatchString(o.Profile) {
		return fmt.Errorf("invalid profile name %q", o.Profile)
	}
	if o.Region != "" && !regionPattern.MatchString(o.Region) {
		return fmt.Errorf("invalid region %q", o.Region)
	}
	return nil
}

// Command builds an *exec.Cmd wired to the current process stdio.
//
// The child is placed in its own process group. `aws ssm start-session` is only
// a launcher: it forks session-manager-plugin, and that grandchild is what holds
// the local listener and the SSM data channel. Killing the aws process alone
// (the default exec.CommandContext behaviour) orphans the plugin and leaves a
// live tunnel into the VPC behind, so cancellation signals the whole group.
func (o Options) Command(ctx context.Context) *exec.Cmd {
	cmd := exec.CommandContext(ctx, o.binary(), o.Args()...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	setProcessGroup(cmd)
	cmd.Cancel = func() error {
		// Interrupt first: session-manager-plugin traps it and closes the data
		// channel server-side, which is what actually ends the SSM session.
		return signalProcessGroup(cmd.Process, os.Interrupt)
	}
	cmd.WaitDelay = shutdownGrace
	return cmd
}

// Run validates the options and executes the session, blocking until the tunnel
// closes or the context is cancelled. Cancellation is treated as a successful
// shutdown, and no descendant of the aws CLI is left running when it returns.
func Run(ctx context.Context, o Options) error {
	if err := o.Validate(); err != nil {
		return err
	}
	if _, err := exec.LookPath(o.binary()); err != nil {
		return fmt.Errorf("%s not found in PATH: install the AWS CLI v2 and the session-manager-plugin", o.binary())
	}

	cmd := o.Command(ctx)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", o.binary(), err)
	}
	proc := cmd.Process
	err := cmd.Wait()
	// Belt and braces: whatever happened above, nothing from this process group
	// may outlive us still holding the local port and the SSM session open.
	_ = signalProcessGroup(proc, os.Kill)

	if ctx.Err() != nil {
		// Ctrl-C or SIGTERM: the non-zero exit of the aws CLI is expected.
		return nil
	}
	return err
}
