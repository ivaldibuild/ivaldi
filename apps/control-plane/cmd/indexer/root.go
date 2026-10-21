package indexer

import "github.com/spf13/cobra"

func NewIndexerCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "indexer",
		Short: "Start the Ivaldi code indexer",
	}
}
