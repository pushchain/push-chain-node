package main

import (
	"github.com/spf13/cobra"
)

func NewRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "puniversald",
		Short: "Push Universal Client Daemon",
	}

	InitRootCmd(rootCmd) // add subcommands like `start` and `version`

	return rootCmd
}
