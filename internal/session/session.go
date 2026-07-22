// Package session builds and runs the underlying `aws ssm start-session`
// port-forward invocation.
package session

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

// DefaultDocument is the managed SSM document for port forwarding.
const DefaultDocument = "AWS-StartPortForwardingSession"

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

// Validate checks the options before spawning the CLI.
func (o Options) Validate() error {
	if o.Target == "" {
		return fmt.Errorf("target is required")
	}
	if o.RemotePort <= 0 || o.RemotePort > 65535 {
		return fmt.Errorf("invalid remote port %d", o.RemotePort)
	}
	if o.LocalPort <= 0 || o.LocalPort > 65535 {
		return fmt.Errorf("invalid local port %d", o.LocalPort)
	}
	return nil
}

// Command builds an *exec.Cmd wired to the current process stdio. The provided
// context governs cancellation: on Ctrl-C the caller cancels it and the aws CLI
// (and session-manager-plugin) receive termination for a graceful teardown.
func (o Options) Command(ctx context.Context) *exec.Cmd {
	cmd := exec.CommandContext(ctx, o.binary(), o.Args()...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd
}

// Run validates the options and executes the session, blocking until the tunnel
// closes or the context is cancelled.
func Run(ctx context.Context, o Options) error {
	if err := o.Validate(); err != nil {
		return err
	}
	if _, err := exec.LookPath(o.binary()); err != nil {
		return fmt.Errorf("%s not found in PATH: install the AWS CLI v2 and the session-manager-plugin", o.binary())
	}
	return o.Command(ctx).Run()
}
