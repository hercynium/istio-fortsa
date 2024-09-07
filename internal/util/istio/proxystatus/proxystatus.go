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

package proxystatus

import (
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
	istioctlver "istio.io/istio/istioctl/pkg/version"
	"istio.io/istio/istioctl/pkg/workload"
	pilotxds "istio.io/istio/pilot/pkg/xds"
	istiocmd "istio.io/istio/pkg/cmd"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

type XDSResponses map[string]*discovery.DiscoveryResponse

func GetProxyStatus() XDSResponses {
	if err := cmd.ConfigAndEnvProcessing(); err != nil {
		fmt.Fprintf(os.Stderr, "Could not initialize: %v\n", err)
		exitCode := cmd.GetExitCode(err)
		os.Exit(exitCode)
	}

	// since this code is so tied up in cobra stuff, pass a channel in so we can get
	// the data we want out...
	xdsResponsesChannel := make(chan XDSResponses)

	rootCmd := GetRootCmd([]string{"proxy-status"}, xdsResponsesChannel)

	// Do not uncomment the line below - it causes a panic!
	//log.EnableKlogWithCobra()

	go func() {
		if err := rootCmd.Execute(); err != nil {
			exitCode := cmd.GetExitCode(err)
			os.Exit(exitCode)
		}
	}()

	return <-xdsResponsesChannel
}

// GetRootCmd returns the root of the cobra command-tree.
func GetRootCmd(args []string, xdsResponsesChannel chan XDSResponses) *cobra.Command {
	rootCmd := &cobra.Command{
		SilenceUsage:      true,
		DisableAutoGenTag: true,
		PersistentPreRunE: cmd.ConfigureLogging,
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

	istiocmd.AddFlags(rootCmd)

	experimentalCmd := &cobra.Command{
		Use:     "experimental",
		Aliases: []string{"x", "exp"},
		Short:   "Experimental commands that may be modified or deprecated",
	}

	xdsBasedTroubleshooting := []*cobra.Command{
		// TODO(hanxiaop): I think experimental version still has issues, so we keep the old version for now.
		istioctlver.XdsVersionCommand(ctx),
		// TODO(hanxiaop): this is kept for some releases in case someone is using it.
		//proxystatus.XdsStatusCommand(ctx),
		XdsStatusCommand(ctx, xdsResponsesChannel),
	}
	troubleshootingCommands := []*cobra.Command{
		istioctlver.NewVersionCommand(ctx),
		//proxystatus.StableXdsStatusCommand(ctx),
		StableXdsStatusCommand(ctx, xdsResponsesChannel),
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

func StableXdsStatusCommand(ctx cli.Context, xdsResponsesChannel chan XDSResponses) *cobra.Command {
	cmd := XdsStatusCommand(ctx, xdsResponsesChannel)
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

func XdsStatusCommand(ctx cli.Context, xdsResponsesChannel chan XDSResponses) *cobra.Command {
	var opts clioptions.ControlPlaneOptions
	var centralOpts clioptions.CentralControlPlaneOptions
	var multiXdsOpts multixds.Options

	statusCmd := &cobra.Command{
		Use:     "proxy-status [<type>/]<name>[.<namespace>]",
		Aliases: []string{"ps"},
		RunE: func(c *cobra.Command, args []string) error {
			kubeClient, err := ctx.CLIClient()
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

			xdsResponsesChannel <- xdsResponses

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
