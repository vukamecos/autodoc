package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	cfgFile string
	logLevel string
	dryRun bool

	rootCmd = &cobra.Command{
		Use:   "autodoc",
		Short: "Auto-update documentation for code changes",
		Long: `autodoc watches a Git repository for code changes and automatically 
keeps documentation up to date. It creates merge/pull requests instead of 
pushing directly to protected branches.

The tool runs on a configurable schedule, analyzes code diffs, sends context
to an LLM (ACP or Ollama), and applies generated documentation updates.`,
	}
)

func init() {
	cobra.OnInitialize(initConfig)

	// Global flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: ./autodoc.yaml)")
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "log level: debug, info, warn, error")
	rootCmd.PersistentFlags().BoolVar(&dryRun, "dry-run", false, "analyze and generate docs but do not write or create MRs")

	// Bind flags to viper
	_ = viper.BindPFlag("log_level", rootCmd.PersistentFlags().Lookup("log-level"))
	_ = viper.BindPFlag("dry_run", rootCmd.PersistentFlags().Lookup("dry-run"))

	// Environment variable prefix
	viper.SetEnvPrefix("AUTODOC")
	viper.AutomaticEnv()
}

func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag
		viper.SetConfigFile(cfgFile)
	} else {
		// Search for config in current directory
		viper.AddConfigPath(".")
		viper.SetConfigName("autodoc")
		viper.SetConfigType("yaml")
	}

	// If a config file is found, read it in
	if err := viper.ReadInConfig(); err != nil {
		// It's okay if config file doesn't exist when we have env vars
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			fmt.Fprintf(os.Stderr, "Error reading config file: %v\n", err)
			os.Exit(1)
		}
	}
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
