package server

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/config"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
)

// APIVersion is the version the API's description states. It names the
// API's contract, /v1, not the binary, so the committed document only
// changes when the API does.
const APIVersion = "v1"

// OpenAPI returns the /v1 API's description as OpenAPI 3.0 JSON, built from
// the controllers the server serves. It is 3.0 rather than huma's 3.1 so
// client generators that read only 3.0 can use it.
func OpenAPI() ([]byte, error) {
	port := 0
	srv, err := api.New(&config.Config{Port: &port, ExternalURL: "https://ivaldi.example.com"}, APIVersion,
		logging.Nop(), api.WithRoutes(services{}.routes(true)...))
	if err != nil {
		return nil, err
	}
	raw, err := srv.OpenAPI().Downgrade()
	if err != nil {
		return nil, fmt.Errorf("describe the API: %w", err)
	}
	var out bytes.Buffer
	if err := json.Indent(&out, raw, "", "  "); err != nil {
		return nil, err
	}
	out.WriteByte('\n')
	return out.Bytes(), nil
}

// NewOpenAPICommand prints the API's description, which api/openapi.json
// holds and the CLI and web app generate their clients from.
func NewOpenAPICommand() *cobra.Command {
	return &cobra.Command{
		Use:   "openapi",
		Short: "Print the control plane API's OpenAPI description",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			doc, err := OpenAPI()
			if err != nil {
				return err
			}
			_, err = cmd.OutOrStdout().Write(doc)
			return err
		},
	}
}
