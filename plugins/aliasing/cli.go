package aliasing

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
)

// checkResponse returns nil for a 2xx response and otherwise an error
// carrying the status and the server's error: the code and message of a
// dpkms error envelope, else the raw body.
func checkResponse(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &env) == nil && env.Error.Message != "" {
		return fmt.Errorf("%d %s: %s", resp.StatusCode, env.Error.Code, env.Error.Message)
	}
	return fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(body)))
}

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
			if err := checkResponse(resp); err != nil {
				return fmt.Errorf("alias set: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "alias %q → %s\n", args[0], args[1])
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
			if err := checkResponse(resp); err != nil {
				return fmt.Errorf("alias list: %w", err)
			}
			var items []map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
				return fmt.Errorf("alias list: decode response: %w", err)
			}
			for _, item := range items {
				fmt.Fprintf(cmd.OutOrStdout(), "%s → %s (scope=%s)\n",
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
			req, err := http.NewRequestWithContext(cmd.Context(), http.MethodDelete,
				baseURL+"/api/v1/aliases/"+url.PathEscape(args[0])+"?"+q.Encode(), nil)
			if err != nil {
				return err
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			if err := checkResponse(resp); err != nil {
				return fmt.Errorf("alias remove: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "removed alias %q\n", args[0])
			return nil
		},
	}
	removeCmd.Flags().StringVar(&scope, "scope", "global", "Scope of the alias to remove")
	removeCmd.Flags().StringVar(&profile, "profile", "", "Profile (for profile-scope aliases)")

	cmd.AddCommand(setCmd, listCmd, removeCmd)
	return cmd
}
