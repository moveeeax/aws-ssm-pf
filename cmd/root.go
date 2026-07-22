// Package cmd wires the ssm-pf CLI.
package cmd

import (
	"context"
	"fmt"
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

func newRootCmd() *cobra.Command {
	var (
		g          globalFlags
		remotePort int
		localPort  int
		target     string
		query      string
		document   string
		useRecent  bool
		noInput    bool
		awsBinary  string
	)

	root := &cobra.Command{
		Use:   "ssm-pf",
		Short: "Forward a local port to an SSM-managed instance with fuzzy target selection",
		Long: "ssm-pf wraps `aws ssm start-session` (AWS-StartPortForwardingSession) with a\n" +
			"fuzzy instance picker, remembered targets, and automatic local-port fallback.",
		Example: "  ssm-pf --remote-port 5432 --local-port 15432\n" +
			"  ssm-pf -r 6379 -q cache-prod\n" +
			"  ssm-pf --recent",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			store, err := history.Load(history.DefaultPath())
			if err != nil {
				return fmt.Errorf("load history: %w", err)
			}

			if useRecent {
				last, ok := store.Recent()
				if !ok {
					return fmt.Errorf("no recent tunnel to reuse")
				}
				target = last.Target
				if remotePort == 0 {
					remotePort = last.RemotePort
				}
				if g.profile == "" {
					g.profile = last.Profile
				}
				if g.region == "" {
					g.region = last.Region
				}
			}

			if remotePort == 0 {
				return fmt.Errorf("--remote-port is required")
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
				RemotePort:   remotePort,
				LocalPort:    local,
				Profile:      g.profile,
				Region:       g.region,
				AWSBinary:    awsBinary,
			}

			store.Add(history.Entry{
				Target:     chosen.ID,
				Name:       chosen.Name,
				RemotePort: remotePort,
				LocalPort:  local,
				Profile:    g.profile,
				Region:     g.region,
				LastUsed:   now(),
			})
			if saveErr := store.Save(); saveErr != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not save history: %v\n", saveErr)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Forwarding localhost:%d -> %s:%d (Ctrl-C to stop)\n",
				local, targetLabel(chosen), remotePort)
			return session.Run(ctx, opts)
		},
	}

	root.Flags().IntVarP(&remotePort, "remote-port", "r", 0, "remote port on the instance (required)")
	root.Flags().IntVarP(&localPort, "local-port", "l", 0, "local port to bind (auto-picked if 0 or busy)")
	root.Flags().StringVarP(&target, "target", "t", "", "instance id, skipping the picker")
	root.Flags().StringVarP(&query, "query", "q", "", "fuzzy filter for the instance picker")
	root.Flags().StringVar(&document, "document-name", session.DefaultDocument, "SSM document to run")
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
