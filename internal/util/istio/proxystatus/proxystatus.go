// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// see here for how to navigate the proxy-status data:
//  https://github.com/istio/istio/blob/master/istioctl/pkg/writer/pilot/status.go

package main

import (
	"flag"
	"fmt"
	"os"

	// import all known client auth plugins
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/spf13/cobra"
	"istio.io/istio/istioctl/cmd"
	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/istioctl/pkg/clioptions"
	"istio.io/istio/istioctl/pkg/completion"
	"istio.io/istio/istioctl/pkg/multixds"
	"istio.io/istio/istioctl/pkg/root"
	"istio.io/istio/istioctl/pkg/util"
	"istio.io/istio/istioctl/pkg/version"
	"istio.io/istio/istioctl/pkg/workload"
	pilotxds "istio.io/istio/pilot/pkg/xds"
	"istio.io/istio/pkg/collateral"
	"istio.io/istio/pkg/log"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

const (
	Revision = "v1-21-3-b16"
)

func main() {
	if err := cmd.ConfigAndEnvProcessing(); err != nil {
		fmt.Fprintf(os.Stderr, "Could not initialize: %v\n", err)
		exitCode := cmd.GetExitCode(err)
		os.Exit(exitCode)
	}

	rootCmd := GetRootCmd([]string{"proxy-status"})
	//rootCmd := GetRootCmd(os.Args[1:])

	log.EnableKlogWithCobra()

	if err := rootCmd.Execute(); err != nil {
		exitCode := cmd.GetExitCode(err)
		os.Exit(exitCode)
	}
}

// AddFlags adds all command line flags to the given command.
func AddFlags(rootCmd *cobra.Command) {
	rootCmd.PersistentFlags().AddGoFlagSet(flag.CommandLine)
}

// GetRootCmd returns the root of the cobra command-tree.
func GetRootCmd(args []string) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:               "istioctl",
		Short:             "Istio control interface.",
		SilenceUsage:      true,
		DisableAutoGenTag: true,
		PersistentPreRunE: cmd.ConfigureLogging,
		Long: `Istio configuration command line utility for service operators to
debug and diagnose their Istio mesh.
`,
	}

	rootCmd.SetArgs(args)

	flags := rootCmd.PersistentFlags()
	rootOptions := cli.AddRootFlags(flags)

	ctx := cli.NewCLIContext(rootOptions)

	_ = rootCmd.RegisterFlagCompletionFunc(cli.FlagIstioNamespace, func(
		cmd *cobra.Command, args []string, toComplete string,
	) ([]string, cobra.ShellCompDirective) {
		return completion.ValidNamespaceArgs(cmd, ctx, args, toComplete)
	})
	_ = rootCmd.RegisterFlagCompletionFunc(cli.FlagNamespace, func(
		cmd *cobra.Command, args []string, toComplete string,
	) ([]string, cobra.ShellCompDirective) {
		return completion.ValidNamespaceArgs(cmd, ctx, args, toComplete)
	})

	// Attach the Istio logging options to the command.
	root.LoggingOptions.AttachCobraFlags(rootCmd)
	hiddenFlags := []string{
		"log_as_json", "log_rotate", "log_rotate_max_age", "log_rotate_max_backups",
		"log_rotate_max_size", "log_stacktrace_level", "log_target", "log_caller", "log_output_level",
	}
	for _, opt := range hiddenFlags {
		_ = rootCmd.PersistentFlags().MarkHidden(opt)
	}

	AddFlags(rootCmd)

	experimentalCmd := &cobra.Command{
		Use:     "experimental",
		Aliases: []string{"x", "exp"},
		Short:   "Experimental commands that may be modified or deprecated",
	}

	xdsBasedTroubleshooting := []*cobra.Command{
		// TODO(hanxiaop): I think experimental version still has issues, so we keep the old version for now.
		version.XdsVersionCommand(ctx),
		// TODO(hanxiaop): this is kept for some releases in case someone is using it.
		//proxystatus.XdsStatusCommand(ctx),
		XdsStatusCommand(ctx),
	}
	troubleshootingCommands := []*cobra.Command{
		version.NewVersionCommand(ctx),
		//proxystatus.StableXdsStatusCommand(ctx),
		StableXdsStatusCommand(ctx),
	}
	var debugCmdAttachmentPoint *cobra.Command
	debugCmdAttachmentPoint = rootCmd

	for _, c := range xdsBasedTroubleshooting {
		experimentalCmd.AddCommand(c)
	}
	for _, c := range troubleshootingCommands {
		debugCmdAttachmentPoint.AddCommand(c)
	}

	rootCmd.AddCommand(experimentalCmd)
	experimentalCmd.AddCommand(workload.Cmd(ctx))

	rootCmd.AddCommand(collateral.CobraCommand(rootCmd, collateral.Metadata{
		Title:   "Istio Control",
		Section: "istioctl CLI",
		Manual:  "Istio Control",
	}))

	//rootCmd.AddCommand(optionsCommand(rootCmd))

	// BFS applies the flag error function to all subcommands
	seenCommands := make(map[*cobra.Command]bool)
	var commandStack []*cobra.Command

	commandStack = append(commandStack, rootCmd)

	for len(commandStack) > 0 {
		n := len(commandStack) - 1
		curCmd := commandStack[n]
		commandStack = commandStack[:n]
		seenCommands[curCmd] = true
		for _, command := range curCmd.Commands() {
			if !seenCommands[command] {
				commandStack = append(commandStack, command)
			}
		}
		curCmd.SetFlagErrorFunc(func(_ *cobra.Command, e error) error {
			return util.CommandParseError{Err: e}
		})
	}

	return rootCmd
}

func StableXdsStatusCommand(ctx cli.Context) *cobra.Command {
	cmd := XdsStatusCommand(ctx)
	unstableFlags := []string{"xds-via-agents", "xds-via-agents-limit"}
	cmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		for _, flag := range unstableFlags {
			if cmd.PersistentFlags().Changed(flag) {
				return fmt.Errorf("--%s is experimental. Use `istioctl experimental ps --%s`", flag, flag)
			}
		}
		return nil
	}
	for _, flag := range unstableFlags {
		_ = cmd.PersistentFlags().MarkHidden(flag)
	}
	return cmd
}

func XdsStatusCommand(ctx cli.Context) *cobra.Command {
	var opts clioptions.ControlPlaneOptions
	var centralOpts clioptions.CentralControlPlaneOptions
	var multiXdsOpts multixds.Options

	statusCmd := &cobra.Command{
		Use:   "proxy-status [<type>/]<name>[.<namespace>]",
		Short: "Retrieves the synchronization status of each Envoy in the mesh",
		Long: `
Retrieves last sent and last acknowledged xDS sync from Istiod to each Envoy in the mesh
`,
		Example: `  # Retrieve sync status for all Envoys in a mesh
  istioctl proxy-status

  # Retrieve sync status for Envoys in a specific namespace
  istioctl proxy-status --namespace foo

  # Retrieve sync diff for a single Envoy and Istiod
  istioctl proxy-status istio-egressgateway-59585c5b9c-ndc59.istio-system

`,
		Aliases: []string{"ps"},
		RunE: func(c *cobra.Command, args []string) error {
			kubeClient, err := ctx.CLIClientWithRevision(opts.Revision)
			if err != nil {
				return err
			}
			multiXdsOpts.MessageWriter = c.OutOrStdout()
			xdsRequest := discovery.DiscoveryRequest{
				TypeUrl: pilotxds.TypeDebugSyncronization,
			}
			xdsResponses, err := multixds.AllRequestAndProcessXds(&xdsRequest, centralOpts, ctx.IstioNamespace(), "", "", kubeClient, multiXdsOpts)
			if err != nil {
				return err
			}

			for _, r := range xdsResponses {
				fmt.Printf("%s", r.String())
			}

			return nil
			// sw := pilot.XdsStatusWriter{
			// 	Writer:    c.OutOrStdout(),
			// 	Namespace: ctx.Namespace(),
			// }
			// return sw.PrintAll(xdsResponses)
		},
		ValidArgsFunction: completion.ValidPodsNameArgs(ctx),
	}

	opts.AttachControlPlaneFlags(statusCmd)
	centralOpts.AttachControlPlaneFlags(statusCmd)
	statusCmd.PersistentFlags().BoolVar(&multiXdsOpts.XdsViaAgents, "xds-via-agents", false,
		"Access Istiod via the tap service of each agent")
	statusCmd.PersistentFlags().IntVar(&multiXdsOpts.XdsViaAgentsLimit, "xds-via-agents-limit", 100,
		"Maximum number of pods being visited by istioctl when `xds-via-agent` flag is true."+
			"To iterate all the agent pods without limit, set to 0")

	return statusCmd
}
