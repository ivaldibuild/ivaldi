package env

import "os"

// Options configures the behavior of Parse and Load.
type Options struct {
	// Source is the lookup function for environment variables.
	// Defaults to os.LookupEnv if nil.
	Source func(key string) (string, bool)

	// Prefix is prepended to every env tag value before lookup.
	Prefix string

	// TagName is the struct tag key to read. Defaults to "env".
	TagName string

	// DefaultTagName is the struct tag key for default values.
	// Defaults to "envDefault".
	DefaultTagName string

	// SliceSeparator is the delimiter for parsing slices from env strings.
	// Defaults to ",".
	SliceSeparator string

	// YAMLPath is a file path to read YAML config from.
	// Used by Load to read YAML before overlaying env vars.
	YAMLPath string

	// DotEnvPaths is a list of .env file paths to load before parsing.
	// Variables already set in the environment are NOT overwritten.
	// Files that do not exist are silently skipped.
	DotEnvPaths []string
}

// withDefaults returns a copy of o with zero values replaced by defaults.
func (o Options) withDefaults() Options {
	if o.Source == nil {
		o.Source = os.LookupEnv
	}
	if o.TagName == "" {
		o.TagName = "env"
	}
	if o.DefaultTagName == "" {
		o.DefaultTagName = "envDefault"
	}
	if o.SliceSeparator == "" {
		o.SliceSeparator = ","
	}
	return o
}

// mergeOptions returns the first Options from the variadic slice, or zero Options.
func mergeOptions(opts []Options) Options {
	if len(opts) == 0 {
		return Options{}
	}
	return opts[0]
}
