package logging

import "fmt"

type LoggerOptions struct {
	// Log level: debug, info, warn, error, fatal
	LogLevel string
	// Where to output logs; console, file
	LogOutput string
	// Log output format: text, json
	LogFormat string
	// Log file prefix (only used when LogOutput is file)
	LogFilePrefix string
	// Log directory (only used when LogOutput is file)
	LogDirectory string
	// Max file size in MB (only used when LogOutput is file)
	MaxFileSize int
	// Max age in days for rotated log files (only used when LogOutput is file)
	MaxAge int
}

func NewLoggerOptions() *LoggerOptions {
	return &LoggerOptions{
		LogOutput:     "console",
		LogFormat:     "text",
		LogDirectory:  "logs",
		LogFilePrefix: "ivaldi",
		MaxFileSize:   50,
		MaxAge:        7,
	}
}

func (o *LoggerOptions) SetLogLevel(level string) *LoggerOptions {
	o.LogLevel = level
	return o
}

func (o *LoggerOptions) SetLogOutput(output string) *LoggerOptions {
	o.LogOutput = output
	return o
}

func (o *LoggerOptions) SetLogFormat(format string) *LoggerOptions {
	o.LogFormat = format
	return o
}

func (o *LoggerOptions) SetLogFilePrefix(prefix string) *LoggerOptions {
	o.LogFilePrefix = prefix
	return o
}

func (o *LoggerOptions) SetLogDirectory(directory string) *LoggerOptions {
	o.LogDirectory = directory
	return o
}

func (o *LoggerOptions) SetMaxFileSize(size int) *LoggerOptions {
	o.MaxFileSize = size
	return o
}

func (o *LoggerOptions) SetMaxAge(days int) *LoggerOptions {
	o.MaxAge = days
	return o
}

func (o *LoggerOptions) Validate() error {
	if o.LogLevel == "" {
		return fmt.Errorf("log level is required, ensure LogLevel is set")
	}

	if o.LogOutput == "" {
		return fmt.Errorf("log output is required, ensure LogOutput is set")
	}

	if o.LogFormat == "" {
		return fmt.Errorf("log format is required, ensure LogFormat is set")
	}

	if o.LogOutput == "file" {
		if o.LogDirectory == "" {
			return fmt.Errorf("log directory is required when output is file, ensure LogDirectory is set")
		}
		if o.LogFilePrefix == "" {
			return fmt.Errorf("log file prefix is required when output is file, ensure LogFilePrefix is set")
		}
		if o.MaxFileSize <= 0 {
			return fmt.Errorf("max file size must be greater than 0, ensure MaxFileSize is set")
		}
		if o.MaxAge <= 0 {
			return fmt.Errorf("max age must be greater than 0, ensure MaxAge is set")
		}
	}

	return nil
}
