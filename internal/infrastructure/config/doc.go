// Package config defines the autodoc configuration schema and loading logic.
//
// Configuration is loaded from a YAML file (typically autodoc.yaml) and can
// be overridden via environment variables:
//
//   - AUTODOC_GITLAB_TOKEN — GitLab private token (takes precedence over config).
//   - AUTODOC_GITHUB_TOKEN — GitHub personal access token.
//   - AUTODOC_ACP_TOKEN   — bearer token for the remote ACP endpoint.
//
// Tokens must never appear in config files committed to source control.
//
// Loading
//
// [Load] reads a file at a given path directly with the yaml package.
// [LoadFromViper] integrates with the Cobra/Viper CLI layer and is the
// preferred entry point when running via the autodoc command.
//
// Validation
//
// [Config.Validate] is called automatically after loading and returns a
// consolidated error listing all validation failures. Use
// [Config.ValidateAndSetDefaults] when you want to apply auto-corrections
// (provider defaults, language defaults) before final validation.
package config
