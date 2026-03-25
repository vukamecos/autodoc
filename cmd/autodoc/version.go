package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	// These are set at build time via ldflags
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("autodoc version %s (commit: %s, built: %s)\n", version, commit, buildDate)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
