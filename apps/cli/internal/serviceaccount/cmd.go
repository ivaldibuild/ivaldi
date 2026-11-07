// Package serviceaccount creates and manages the Organization's service
// accounts, which CI and other integrations act as.
package serviceaccount

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/ivaldibuild/ivaldi-cli/internal/apiclient"
	"github.com/ivaldibuild/ivaldi-cli/internal/app"
	"github.com/ivaldibuild/ivaldi-cli/internal/client"
)

func New(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "service-account",
		Aliases: []string{"sa"},
		Short:   "Create and manage the service accounts CI and other integrations act as",
		Long: "Create and manage the Organization's service accounts. A service account acts with the roles\n" +
			"granted to it, and authenticates with API tokens: see ivaldictl api-token.",
	}

	cmd.AddCommand(
		createCommand(a),
		listCommand(a),
		getCommand(a),
		changeCommand(a, "suspend", "Suspend a service account, which stops all of its tokens",
			func(c *apiclient.ClientWithResponses, ctx context.Context, id string) (client.Response, *apiclient.ResponseServiceAccount, error) {
				resp, err := c.SuspendServiceAccountWithResponse(ctx, id)
				if err != nil {
					return nil, nil, err
				}
				return resp, resp.JSON200, nil
			}),
		changeCommand(a, "reactivate", "Reactivate a suspended service account, so its tokens work again",
			func(c *apiclient.ClientWithResponses, ctx context.Context, id string) (client.Response, *apiclient.ResponseServiceAccount, error) {
				resp, err := c.ReactivateServiceAccountWithResponse(ctx, id)
				if err != nil {
					return nil, nil, err
				}
				return resp, resp.JSON200, nil
			}),
	)

	return cmd
}

func createCommand(a *app.App) *cobra.Command {
	var name, description string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a service account",
		Long: "Create a service account. It holds no roles until one is granted to it, and it\n" +
			"authenticates with tokens from ivaldictl api-token create.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := a.Client(cmd)
			if err != nil {
				return err
			}
			body := apiclient.CreateServiceAccountJSONRequestBody{DisplayName: name}
			if description != "" {
				body.Description = &description
			}
			resp, err := c.CreateServiceAccountWithResponse(cmd.Context(), body)
			if err != nil {
				return err
			}
			if err := client.Check(resp, resp.JSON201 != nil); err != nil {
				return err
			}
			return printAccount(a, cmd, resp.JSON201.Data)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Its name, such as \"GitHub Actions\" (required)")
	cmd.Flags().StringVar(&description, "description", "", "What it is for")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func listCommand(a *app.App) *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the Organization's service accounts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := a.Client(cmd)
			if err != nil {
				return err
			}
			params := &apiclient.ListServiceAccountsParams{}
			if all {
				params.IncludeInactive = &all
			}
			resp, err := c.ListServiceAccountsWithResponse(cmd.Context(), params)
			if err != nil {
				return err
			}
			if err := client.Check(resp, resp.JSON200 != nil); err != nil {
				return err
			}
			accounts := resp.JSON200.Data
			return a.Print(cmd, accounts, func(w io.Writer) {
				for _, sa := range accounts {
					fmt.Fprintf(w, "%-32s %-10s %s\n", sa.Id, sa.Status, sa.DisplayName)
				}
			})
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "Include suspended service accounts")
	return cmd
}

func getCommand(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "get SERVICE_ACCOUNT_ID",
		Short: "Show a service account",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := a.Client(cmd)
			if err != nil {
				return err
			}
			resp, err := c.GetServiceAccountWithResponse(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if err := client.Check(resp, resp.JSON200 != nil); err != nil {
				return err
			}
			return printAccount(a, cmd, resp.JSON200.Data)
		},
	}
}

// changeCommand builds a command that changes one service account's status.
func changeCommand(a *app.App, use, short string,
	call func(*apiclient.ClientWithResponses, context.Context, string) (client.Response, *apiclient.ResponseServiceAccount, error),
) *cobra.Command {
	return &cobra.Command{
		Use:   use + " SERVICE_ACCOUNT_ID",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := a.Client(cmd)
			if err != nil {
				return err
			}
			resp, data, err := call(c, cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if err := client.Check(resp, data != nil); err != nil {
				return err
			}
			return printAccount(a, cmd, data.Data)
		},
	}
}

// printAccount writes a service account as JSON, or for -o text, a summary.
func printAccount(a *app.App, cmd *cobra.Command, sa apiclient.ServiceAccount) error {
	return a.Print(cmd, sa, func(w io.Writer) {
		fmt.Fprintf(w, "Service account %s (%s) is %s.\n", sa.DisplayName, sa.Id, sa.Status)
		if sa.Description != "" {
			fmt.Fprintln(w, sa.Description)
		}
	})
}
