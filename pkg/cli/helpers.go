package cli

import (
	"github.com/spf13/cobra"
)

// addPersistentFlags adds the provided flags to the given command as persistent flags.
func AddPersistentFlags(cmd *cobra.Command, flags map[string]Flag) {
	for key, flag := range flags {
		switch flag.Type {
		case "string":
			if flag.Short != "" {
				cmd.PersistentFlags().StringP(key, flag.Short, flag.Default.(string), flag.Description)
			} else {
				cmd.PersistentFlags().String(key, flag.Default.(string), flag.Description)
			}
		case "stringSlice":
			if flag.Short != "" {
				cmd.PersistentFlags().StringSliceP(key, flag.Short, flag.Default.([]string), flag.Description)
			} else {
				cmd.PersistentFlags().StringSlice(key, flag.Default.([]string), flag.Description)
			}
		case "int":
			if flag.Short != "" {
				cmd.PersistentFlags().IntP(key, flag.Short, flag.Default.(int), flag.Description)
			} else {
				cmd.PersistentFlags().Int(key, flag.Default.(int), flag.Description)
			}
		case "bool":
			if flag.Short != "" {
				cmd.PersistentFlags().BoolP(key, flag.Short, flag.Default.(bool), flag.Description)
			} else {
				cmd.PersistentFlags().Bool(key, flag.Default.(bool), flag.Description)
			}
		}
	}
}
