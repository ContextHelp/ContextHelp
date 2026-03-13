package autosuggest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/spf13/cobra"
)

// NewCLICommands returns a cobra.Command subtree for the autosuggest plugin.
// Mount it as: rootCmd.AddCommand(autosuggest.NewCLICommands(baseURL))
func NewCLICommands(baseURL string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "suggest",
		Short: "Manage auto-suggest tag/mention proposals",
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List objects with pending tag/mention suggestions",
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := http.Get(baseURL + "/api/v1/suggestions")
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			var items []map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
				return err
			}
			for _, item := range items {
				fmt.Fprintf(os.Stdout, "%s: tags=%v mentions=%v\n",
					item["object_id"], item["tags"], item["mentions"])
			}
			return nil
		},
	}

	approveCmd := &cobra.Command{
		Use:   "approve <object-id>",
		Short: "Apply pending tag/mention suggestions to an object",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := http.Post(baseURL+"/api/v1/suggestions/"+args[0]+"/approve",
				"application/json", nil)
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			fmt.Fprintf(os.Stdout, "approved: %s\n", args[0])
			return nil
		},
	}

	rejectCmd := &cobra.Command{
		Use:   "reject <object-id>",
		Short: "Discard pending tag/mention suggestions for an object",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := http.Post(baseURL+"/api/v1/suggestions/"+args[0]+"/reject",
				"application/json", nil)
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			fmt.Fprintf(os.Stdout, "rejected: %s\n", args[0])
			return nil
		},
	}

	cmd.AddCommand(listCmd, approveCmd, rejectCmd)
	return cmd
}
