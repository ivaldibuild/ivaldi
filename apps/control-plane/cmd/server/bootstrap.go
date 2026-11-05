package server

import (
	"github.com/ivaldibuild/ivaldi-control-plane/internal/config"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
)

func getLogger(c *config.Logging) (logging.Logger, error) {
	opts := logging.NewLoggerOptions().SetLogLevel("info")

	if c != nil {
		opts.SetLogDirectory(c.Directory).
			SetLogFilePrefix(c.FilePrefix).
			SetMaxFileSize(c.MaxFileSizeMB).
			SetMaxAge(c.MaxAgeDays)

		if c.Level != nil {
			opts.SetLogLevel(*c.Level)
		}

		if c.Output != nil {
			opts.SetLogOutput(*c.Output)
		}

		if c.Format != nil {
			opts.SetLogFormat(*c.Format)
		}
	}

	return logging.NewLogger(opts)
}
