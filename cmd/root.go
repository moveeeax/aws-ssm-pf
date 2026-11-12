// Package cmd wires the ssm-pf CLI.
package cmd

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/moveeeax/aws-ssm-pf/internal/awsclient"
	"github.com/moveeeax/aws-ssm-pf/internal/history"
	"github.com/moveeeax/aws-ssm-pf/internal/instance"
	"github.com/moveeeax/aws-ssm-pf/internal/picker"
	"github.com/moveeeax/aws-ssm-pf/internal/ports"
	"github.com/moveeeax/aws-ssm-pf/internal/session"
)

type globalFlags struct {
	profile string
	region  string
}

// listerFactory is overridable in tests.
var listerFactory = func(ctx context.Context, profile, region string) (awsclient.Lister, error) {
	return awsclient.New(ctx, profile, region)
}

// endpointListerFactory is overridable in tests.
var endpointListerFactory = func(ctx context.Context, profile, region string) (awsclient.EndpointLister, error) {
	return awsclient.NewRDS(ctx, profile, region)
}

func newRootCmd() *cobra.Command {
	var (
		g          globalFlags
		remotePort int
		localPort  int
		target     string
		query      string
		hostFlag   string
		rdsMode    bool
		rdsQuery   string
		document   string
		useRecent  bool
		noInput    bool
		awsBinary  string
	)

	root := &cobra.Command{
		Use:   "ssm-pf",
		Short: "Forward a local port to an SSM-managed instance with fuzzy target selection",
		Long: "ssm-pf wraps `aws ssm start-session` (AWS-StartPortForwardingSession) with a\n" +
			"fuzzy instance picker, remembered targets, and automatic local-port fallback.\n" +
			"With --host or --rds it forwards through the instance to another host\n" +
			"(AWS-StartPortForwardingSessionToRemoteHost).",
		Example: "  ssm-pf --remote-port 5432 --local-port 15432\n" +
			"  ssm-pf -r 6379 -q cache-prod\n" +
			"  ssm-pf --host db.internal.example:5432 -l 15432 -q bastion\n" +
			"  ssm-pf --rds --rds-query orders -q bastion\n" +
			"  ssm-pf --recent",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			// History is a convenience cache, not load-bearing: a missing,
			// unreadable, or corrupt history file must not stop an otherwise
			// valid port forward. Load always returns a usable empty store
			// alongside any error, so we warn and carry on rather than abort.
			store, err := history.Load(history.DefaultPath())
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not load history, starting fresh: %v\n", err)
			}

			// --host may carry the port ("db.internal:5432"); it is the remote
			// port then, and a different --remote-port is a contradiction.
			var host string
			if hostFlag != "" {
				h, hostPort, err := session.SplitHost(hostFlag)
				if err != nil {
					return err
				}
				host = h
				if hostPort != 0 {
					if remotePort != 0 && remotePort != hostPort {
						return fmt.Errorf("--host port %d conflicts with --remote-port %d", hostPort, remotePort)
					}
					remotePort = hostPort
				}
			}
			if rdsMode && host != "" {
				return fmt.Errorf("--rds and --host are mutually exclusive")
			}
			if rdsMode && useRecent {
				return fmt.Errorf("--rds and --recent are mutually exclusive: --recent already remembers the host")
			}
			if rdsQuery != "" && !rdsMode {
				return fmt.Errorf("--rds-query requires --rds")
			}

			if useRecent {
				last, ok := store.Recent()
				if !ok {
					return fmt.Errorf("no recent tunnel to reuse")
				}
				// Explicit flags always win over the remembered tunnel.
				if target == "" {
					target = last.Target
				}
				// The remembered host and remote port belong together: an
				// explicit --host replaces both unless --remote-port is given.
				if host == "" {
					host = last.Host
					if remotePort == 0 {
						remotePort = last.RemotePort
					}
				}
				if localPort == 0 {
					localPort = last.LocalPort
				}
				if g.profile == "" {
					g.profile = last.Profile
				}
				if g.region == "" {
					g.region = last.Region
				}
			}

			// With --rds the remote port can come from the chosen endpoint.
			if remotePort == 0 && !rdsMode {
				if host != "" {
					return fmt.Errorf("--remote-port is required (or give it as --host %s:PORT)", host)
				}
				return fmt.Errorf("--remote-port is required")
			}
			// Validate before touching AWS, binding a port, or recording history,
			// so a typo costs an error message rather than a half-done side effect.
			if remotePort != 0 {
				if err := session.ValidatePort("remote", remotePort); err != nil {
					return err
				}
			}
			if localPort != 0 {
				if err := session.ValidatePort("local", localPort); err != nil {
					return err
				}
			}
			if target != "" {
				if err := session.ValidateTarget(target); err != nil {
					return err
				}
			}
			if host != "" {
				// A host from history skipped SplitHost, so check it here too.
				if err := session.ValidateHost(host); err != nil {
					return err
				}
			}

			if rdsMode {
				epLister, err := endpointListerFactory(ctx, g.profile, g.region)
				if err != nil {
					return err
				}
				eps, err := epLister.ListEndpoints(ctx)
				if err != nil {
					return err
				}
				ep, err := picker.ChooseEndpoint(cmd.OutOrStdout(), cmd.InOrStdin(), eps, rdsQuery, noInput)
				if err != nil {
					return err
				}
				// The endpoint comes from the AWS API, but it still ends up in
				// the aws CLI argument vector, so it gets the same checks.
				if err := session.ValidateHost(ep.Host); err != nil {
					return err
				}
				host = ep.Host
				if remotePort == 0 {
					remotePort = ep.Port
				}
				if err := session.ValidatePort("remote", remotePort); err != nil {
					return fmt.Errorf("endpoint %s: %w", ep.Label(), err)
				}
			}

			var chosen instance.Instance
			if target != "" {
				chosen = instance.Instance{ID: target}
			} else {
				lister, err := listerFactory(ctx, g.profile, g.region)
				if err != nil {
					return err
				}
				insts, err := lister.List(ctx)
				if err != nil {
					return err
				}
				chosen, err = picker.Choose(cmd.OutOrStdout(), cmd.InOrStdin(), insts, query, noInput)
				if err != nil {
					return err
				}
			}

			local, substituted, err := ports.Pick(localPort)
			if err != nil {
				return fmt.Errorf("pick local port: %w", err)
			}
			if substituted {
				fmt.Fprintf(cmd.ErrOrStderr(), "local port %d busy, using %d instead\n", localPort, local)
			}

			opts := session.Options{
				Target:       chosen.ID,
				DocumentName: document,
				Host:         host,
				RemotePort:   remotePort,
				LocalPort:    local,
				Profile:      g.profile,
				Region:       g.region,
				AWSBinary:    awsBinary,
			}
			if err := opts.Validate(); err != nil {
				return err
			}

			store.Add(history.Entry{
				Target:     chosen.ID,
				Name:       chosen.Name,
				Host:       host,
				RemotePort: remotePort,
				LocalPort:  local,
				Profile:    g.profile,
				Region:     g.region,
				LastUsed:   now(),
			})
			if saveErr := store.Save(); saveErr != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not save history: %v\n", saveErr)
			}

			if host != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "Forwarding localhost:%d -> %s via %s (Ctrl-C to stop)\n",
					local, net.JoinHostPort(host, fmt.Sprint(remotePort)), targetLabel(chosen))
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "Forwarding localhost:%d -> %s:%d (Ctrl-C to stop)\n",
					local, targetLabel(chosen), remotePort)
			}
			return session.Run(ctx, opts)
		},
	}

	root.Flags().IntVarP(&remotePort, "remote-port", "r", 0, "remote port on the instance, or on --host (required unless --host has a port or --rds is set)")
	root.Flags().IntVarP(&localPort, "local-port", "l", 0, "local port to bind (auto-picked if 0 or busy)")
	root.Flags().StringVarP(&target, "target", "t", "", "instance id, skipping the picker")
	root.Flags().StringVarP(&query, "query", "q", "", "fuzzy filter for the instance picker")
	root.Flags().StringVar(&hostFlag, "host", "", "forward through the instance to this host (DNS name or IP, optionally host:port)")
	root.Flags().BoolVar(&rdsMode, "rds", false, "pick the remote host from RDS instance and cluster endpoints")
	root.Flags().StringVar(&rdsQuery, "rds-query", "", "fuzzy filter for the RDS endpoint picker")
	root.Flags().StringVar(&document, "document-name", "", "SSM document to run (default "+session.DefaultDocument+", or "+session.RemoteHostDocument+" with --host/--rds)")
	root.Flags().BoolVar(&useRecent, "recent", false, "reuse the most recent target and ports")
	root.Flags().BoolVar(&noInput, "no-input", false, "never prompt; require an unambiguous target")
	root.Flags().StringVar(&awsBinary, "aws-binary", "", "path to the aws CLI (default: aws in PATH)")
	root.PersistentFlags().StringVar(&g.profile, "profile", "", "AWS shared-config profile")
	root.PersistentFlags().StringVar(&g.region, "region", "", "AWS region")

	root.AddCommand(newListCmd(&g))
	return root
}

func newListCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List SSM-managed online instances",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			lister, err := listerFactory(ctx, g.profile, g.region)
			if err != nil {
				return err
			}
			insts, err := lister.List(ctx)
			if err != nil {
				return err
			}
			if len(insts) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no SSM-managed online instances found")
				return nil
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "INSTANCE ID\tNAME\tPING\tPLATFORM\tIP")
			for _, i := range insts {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", i.ID, dash(i.Name), i.PingStatus, dash(i.Platform), dash(i.IPAddress))
			}
			return tw.Flush()
		},
	}
}

func targetLabel(i instance.Instance) string {
	if i.Name != "" {
		return i.Label()
	}
	return i.ID
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// Execute runs the root command.
func Execute() error {
	return newRootCmd().Execute()
}
