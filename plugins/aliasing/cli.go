package aliasing

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"

	"github.com/spf13/cobra"
)

// NewCLICommands returns a cobra.Command subtree for the aliasing plugin.
// Mount it as: rootCmd.AddCommand(aliasing.NewCLICommands(baseURL))
func NewCLICommands(baseURL string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "alias",
		Short: "Manage human-readable aliases for knowledge objects",
	}

	var scope, profile string

	setCmd := &cobra.Command{
		Use:   "set <alias> <object-id>",
		Short: "Create an alias for an object",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, _ := json.Marshal(map[string]string{
				"alias":     args[0],
				"object_id": args[1],
				"scope":     scope,
				"profile":   profile,
			})
			resp, err := http.Post(baseURL+"/api/v1/aliases", "application/json",
				bytes.NewReader(body))
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			fmt.Fprintf(os.Stdout, "alias %q → %s\n", args[0], args[1])
			return nil
		},
	}
	setCmd.Flags().StringVar(&scope, "scope", "global", "Scope: global or profile")
	setCmd.Flags().StringVar(&profile, "profile", "", "Profile name (for profile scope)")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List aliases",
		RunE: func(cmd *cobra.Command, args []string) error {
			objectID, _ := cmd.Flags().GetString("object")
			q := url.Values{}
			if objectID != "" {
				q.Set("object_id", objectID)
			}
			resp, err := http.Get(baseURL + "/api/v1/aliases?" + q.Encode())
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			var items []map[string]interface{}
			json.NewDecoder(resp.Body).Decode(&items)
			for _, item := range items {
				fmt.Fprintf(os.Stdout, "%s → %s (scope=%s)\n",
					item["alias"], item["object_id"], item["scope"])
			}
			return nil
		},
	}
	listCmd.Flags().String("object", "", "Filter by object ID")

	removeCmd := &cobra.Command{
		Use:   "remove <alias>",
		Short: "Delete an alias",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			q := url.Values{}
			q.Set("scope", scope)
			if profile != "" {
				q.Set("profile", profile)
			}
			req, _ := http.NewRequest(http.MethodDelete,
				baseURL+"/api/v1/aliases/"+args[0]+"?"+q.Encode(), nil)
			client := &http.Client{}
			resp, err := client.Do(req)
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			fmt.Fprintf(os.Stdout, "removed alias %q\n", args[0])
			return nil
		},
	}
	removeCmd.Flags().StringVar(&scope, "scope", "global", "Scope of the alias to remove")
	removeCmd.Flags().StringVar(&profile, "profile", "", "Profile (for profile-scope aliases)")

	cmd.AddCommand(setCmd, listCmd, removeCmd)
	return cmd
}
