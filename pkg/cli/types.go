package cli

// Flag represents a command-line flag with its type, short name, description, and default value.
type Flag struct {
	Type        string
	Short       string
	Description string
	Default     any
}
