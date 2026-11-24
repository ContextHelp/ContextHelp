package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
)

var holdCmd = &cobra.Command{
	Use:   "hold",
	Short: "Manage legal and administrative holds on objects",
	Long:  "List, place, or release active holds that prevent any mutation or deletion of protected objects.",
}

var holdListCmd = &cobra.Command{
	Use:   "list",
	Short: "List active holds",
	RunE: func(cmd *cobra.Command, _ []string) error {
		objectID, _ := cmd.Flags().GetString("object")
		sourceKey, _ := cmd.Flags().GetString("source-key")
		holdID, _ := cmd.Flags().GetString("hold-id")
		client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
		if err != nil {
			return err
		}
		holds, err := client.ListHolds(cmd.Context(), strings.TrimSpace(objectID), strings.TrimSpace(sourceKey), strings.TrimSpace(holdID))
		if err != nil {
			return err
		}
		w := cmd.OutOrStdout()
		if len(holds) == 0 {
			fmt.Fprintln(w, "No active holds.")
			return nil
		}
		for _, h := range holds {
			fmt.Fprintf(w, "%s\tobject=%s\tsource=%s\treason=%s\n", h.HoldID, h.ObjectID, h.Source, h.Reason)
		}
		return nil
	},
}

var holdPlaceCmd = &cobra.Command{
	Use:   "place",
	Short: "Place a hold on an object or source_key",
	RunE: func(cmd *cobra.Command, _ []string) error {
		objectID, _ := cmd.Flags().GetString("object")
		sourceKey, _ := cmd.Flags().GetString("source-key")
		holdID, _ := cmd.Flags().GetString("hold-id")
		reason, _ := cmd.Flags().GetString("reason")
		client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
		if err != nil {
			return err
		}
		placed, err := client.PlaceHold(cmd.Context(), dpkmsclient.HoldRequest{
			ObjectID:  strings.TrimSpace(objectID),
			SourceKey: strings.TrimSpace(sourceKey),
			HoldID:    strings.TrimSpace(holdID),
			Source:    "operator",
			Reason:    strings.TrimSpace(reason),
		})
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Placed hold %s on %d target(s)\n", holdID, len(placed))
		return nil
	},
}

var holdReleaseCmd = &cobra.Command{
	Use:   "release",
	Short: "Release an active operator hold",
	RunE: func(cmd *cobra.Command, _ []string) error {
		objectID, _ := cmd.Flags().GetString("object")
		sourceKey, _ := cmd.Flags().GetString("source-key")
		holdID, _ := cmd.Flags().GetString("hold-id")
		reason, _ := cmd.Flags().GetString("reason")
		client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
		if err != nil {
			return err
		}
		n, err := client.ReleaseHold(cmd.Context(), strings.TrimSpace(holdID), strings.TrimSpace(objectID), strings.TrimSpace(sourceKey), strings.TrimSpace(reason))
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Released %d hold(s) for %s\n", n, holdID)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(holdCmd)
	holdCmd.AddCommand(holdListCmd, holdPlaceCmd, holdReleaseCmd)

	cliconv.WithSideEffect(holdListCmd, cliconv.SideEffectRead)
	cliconv.WithSideEffect(holdPlaceCmd, cliconv.SideEffectWrite)
	cliconv.WithSideEffect(holdReleaseCmd, cliconv.SideEffectWrite)

	holdListCmd.Flags().String("object", "", "filter by object ID")
	holdListCmd.Flags().String("source-key", "", "filter by source_key")
	holdListCmd.Flags().String("hold-id", "", "filter by hold ID")

	holdPlaceCmd.Flags().String("object", "", "target object ID")
	holdPlaceCmd.Flags().String("source-key", "", "target source_key")
	holdPlaceCmd.Flags().String("hold-id", "", "hold identifier")
	holdPlaceCmd.Flags().String("reason", "", "non-empty reason for placing the hold")

	holdReleaseCmd.Flags().String("object", "", "target object ID")
	holdReleaseCmd.Flags().String("source-key", "", "target source_key")
	holdReleaseCmd.Flags().String("hold-id", "", "hold identifier")
	holdReleaseCmd.Flags().String("reason", "", "reason for releasing the hold")
}
