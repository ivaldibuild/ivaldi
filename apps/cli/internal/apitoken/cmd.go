// Package apitoken issues, lists, and revokes service accounts' API tokens.
package apitoken

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ivaldibuild/ivaldi-cli/internal/apiclient"
	"github.com/ivaldibuild/ivaldi-cli/internal/app"
	"github.com/ivaldibuild/ivaldi-cli/internal/client"
)

func New(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "api-token",
		Short: "Issue, list, and revoke service accounts' API tokens",
		Long: "Issue, list, and revoke the API tokens service accounts authenticate with. Pass a token to\n" +
			"ivaldictl with --api-token or IVALDI_API_TOKEN.",
	}
	cmd.AddCommand(createCommand(a), listCommand(a), revokeCommand(a))
	return cmd
}

type createFlags struct {
	serviceAccount, name, expiresAt, expiresIn string
}

func createCommand(a *app.App) *cobra.Command {
	var f createFlags
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Issue an API token for a service account; its secret is shown only once",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body := apiclient.CreateApiTokenJSONRequestBody{Name: f.name}
			expires, err := f.expiry(a.Now())
			if err != nil {
				return err
			}
			body.ExpiresAt = expires

			c, err := a.Client(cmd)
			if err != nil {
				return err
			}
			resp, err := c.CreateApiTokenWithResponse(cmd.Context(), f.serviceAccount, body)
			if err != nil {
				return err
			}
			if err := client.Check(resp, resp.JSON201 != nil); err != nil {
				return err
			}

			issued := resp.JSON201.Data
			return a.Print(cmd, issued, func(w io.Writer) {
				fmt.Fprintf(w, "API token %s (%s) for %s, %s.\n", issued.Token.Name, issued.Token.Id, issued.Token.PrincipalId, expiry(issued.Token))
				fmt.Fprintln(w, issued.Secret)
				fmt.Fprintln(cmd.ErrOrStderr(), "Store the secret now: it will not be shown again.")
			})
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&f.serviceAccount, "service-account", "", "The service account's ID (required)")
	fl.StringVar(&f.name, "name", "", "What the token is for, such as github-actions (required)")
	fl.StringVar(&f.expiresAt, "expires-at", "", "When it stops working, as RFC 3339, such as 2027-06-01T00:00:00Z")
	fl.StringVar(&f.expiresIn, "expires-in", "", "How long it works, such as 90d or 12h")
	_ = cmd.MarkFlagRequired("service-account")
	_ = cmd.MarkFlagRequired("name")
	cmd.MarkFlagsMutuallyExclusive("expires-at", "expires-in")
	return cmd
}

// expiry returns when the token stops working, or nil for never.
func (f createFlags) expiry(now time.Time) (*time.Time, error) {
	switch {
	case f.expiresAt != "":
		t, err := time.Parse(time.RFC3339, f.expiresAt)
		if err != nil {
			return nil, fmt.Errorf("--expires-at %q must be RFC 3339, such as 2027-06-01T00:00:00Z", f.expiresAt)
		}
		return &t, nil
	case f.expiresIn != "":
		d, err := parseDuration(f.expiresIn)
		if err != nil {
			return nil, err
		}
		t := now.Add(d).UTC().Truncate(time.Second)
		return &t, nil
	}
	return nil, nil
}

// parseDuration reads a whole number of days, such as 90d, or a Go
// duration, such as 12h.
func parseDuration(s string) (time.Duration, error) {
	invalid := fmt.Errorf("--expires-in %q must be days, such as 90d, or a duration, such as 12h", s)
	var d time.Duration
	if days, ok := strings.CutSuffix(s, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil {
			return 0, invalid
		}
		d = time.Duration(n) * 24 * time.Hour
	} else {
		var err error
		if d, err = time.ParseDuration(s); err != nil {
			return 0, invalid
		}
	}
	if d <= 0 {
		return 0, errors.New("--expires-in must be in the future")
	}
	return d, nil
}

func listCommand(a *app.App) *cobra.Command {
	var serviceAccount string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List a service account's API tokens, without their secrets",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := a.Client(cmd)
			if err != nil {
				return err
			}
			resp, err := c.ListApiTokensWithResponse(cmd.Context(), serviceAccount)
			if err != nil {
				return err
			}
			if err := client.Check(resp, resp.JSON200 != nil); err != nil {
				return err
			}
			tokens := resp.JSON200.Data
			return a.Print(cmd, tokens, func(w io.Writer) {
				for _, t := range tokens {
					fmt.Fprintf(w, "%-32s %-16s %-24s %s\n", t.Id, t.TokenPrefix, t.Name, expiry(t))
				}
			})
		},
	}
	cmd.Flags().StringVar(&serviceAccount, "service-account", "", "The service account's ID (required)")
	_ = cmd.MarkFlagRequired("service-account")
	return cmd
}

func revokeCommand(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "revoke API_TOKEN_ID",
		Short: "Revoke an API token for good",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := a.Client(cmd)
			if err != nil {
				return err
			}
			resp, err := c.RevokeApiTokenWithResponse(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if err := client.Check(resp, resp.JSON200 != nil); err != nil {
				return err
			}
			t := resp.JSON200.Data
			return a.Print(cmd, t, func(w io.Writer) {
				fmt.Fprintf(w, "API token %s (%s) is %s.\n", t.Name, t.Id, expiry(t))
			})
		},
	}
}

// expiry says whether a token works, and until when.
func expiry(t apiclient.APITokenSnapshot) string {
	switch {
	case t.RevokedAt != nil:
		return "revoked " + t.RevokedAt.UTC().Format(time.RFC3339)
	case t.ExpiresAt != nil:
		return "expires " + t.ExpiresAt.UTC().Format(time.RFC3339)
	}
	return "never expires"
}
