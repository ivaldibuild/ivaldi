package agent

import (
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/cli"
	"github.com/spf13/cobra"
)

const VERSION = "dev"
const HEARTBEAT_INTERVAL = 30 * time.Second
const CREDENTIALS_STORE_PATH = "/home/ivaldi-k8s-agent/credentials"

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "ivaldi-k8s-agent",
		Aliases:      []string{"ivld-k8s-agent", "iv-k8s-agent"},
		Short:        "Ivaldi kubernetes runtime agent",
		Long:         "Ivaldi kubernetes runtime agent manages the lifecycle of applications within a Kubernetes cluster.",
		Version:      VERSION,
		SilenceUsage: true,
	}

	cli.AddPersistentFlags(cmd, initPersistentFlags())

	return cmd
}

func initPersistentFlags() map[string]cli.Flag {
	return map[string]cli.Flag{
		"server-url": {
			Type:        "string",
			Description: "URL of the Ivaldi control plane; defaults to IVALDI_SERVER_URL, then the empty string",
			Default:     "",
		},
		"authentication-method": {
			Type:        "string",
			Description: "Authentication method to use with the Ivaldi control plane; defaults to IVALDI_AUTH_METHOD; bearer, mtls, kubernetes",
			Default:     "",
		},
		"credentials-store-path": {
			Type:        "string",
			Description: "Path to the credentials file for the Ivaldi control plane; defaults to IVALDI_CREDENTIALS_PATH, then the empty string",
			Default:     cli.GetEnvString("IVALDI_CREDENTIALS_DIR", CREDENTIALS_STORE_PATH),
		},
		"client-cert-file": {
			Type:        "string",
			Description: "Path to the client certificate file for the Ivaldi control plane; defaults to IVALDI_CLIENT_CERT_FILE, then the empty string",
			Default:     "",
		},
		"client-key-file": {
			Type:        "string",
			Description: "Path to the client key file for the Ivaldi control plane; defaults to IVALDI_CLIENT_KEY_FILE, then the empty string",
			Default:     "",
		},
		"client-ca-file": {
			Type:        "string",
			Description: "Path to the client CA file for the Ivaldi control plane; defaults to IVALDI_CLIENT_CA_FILE, then the empty string",
			Default:     "",
		},
		"kubernetes-token-file": {
			Type:        "string",
			Description: "Path to the Kubernetes token file; defaults to KUBERNETES_TOKEN_FILE, then the empty string",
			Default:     "",
		},
		"heartbeat-interval": {
			Type:        "duration",
			Description: "Interval for sending heartbeat signals to the Ivaldi control plane; defaults to IVALDI_HEARTBEAT_INTERVAL",
			Default:     cli.GetEnvDuration("IVALDI_HEARTBEAT_INTERVAL", HEARTBEAT_INTERVAL),
		},
	}
}
