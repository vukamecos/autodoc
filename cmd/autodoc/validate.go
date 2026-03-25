package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/vukamecos/autodoc/internal/config"
	"github.com/vukamecos/autodoc/internal/observability"
)

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate configuration file",
	Long: `Validate the autodoc configuration without running the service.

This command checks:
- Required fields are present
- Provider names are valid
- File paths are accessible
- Language codes are valid
- Configuration relationships are consistent`,
	RunE: validateConfig,
}

func init() {
	rootCmd.AddCommand(validateCmd)
}

func validateConfig(cmd *cobra.Command, args []string) error {
	log := observability.NewLogger(viper.GetString("log_level"))

	// Load configuration from viper
	cfg, err := config.LoadFromViper()
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Configuration validation failed:\n  %v\n", err)
		os.Exit(1)
	}

	fmt.Println("✅ Configuration is valid")

	if cfgFile := viper.ConfigFileUsed(); cfgFile != "" {
		fmt.Printf("   Config file: %s\n", cfgFile)
	}
	fmt.Printf("   Repository: %s (%s)\n", cfg.Repository.ProjectID, cfg.Repository.Provider)
	fmt.Printf("   ACP Provider: %s\n", cfg.ACP.Provider)
	if cfg.ACP.Provider == "ollama" {
		fmt.Printf("   Model: %s\n", cfg.ACP.Model)
	}
	fmt.Printf("   Documentation paths: %v\n", cfg.Documentation.AllowedPaths)
	fmt.Printf("   Primary language: %s\n", cfg.Documentation.PrimaryLanguage)

	if dryRun := viper.GetBool("dry_run"); dryRun {
		fmt.Println("   Dry-run mode: enabled")
	}

	log.Info("validate: configuration validated successfully")
	return nil
}
