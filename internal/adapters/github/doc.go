// Package github implements [domain.RepositoryPort] and [domain.MRCreatorPort]
// using the GitHub REST API v3.
//
// All operations are API-only (no local clone). Authentication uses an
// Authorization: Bearer header with the token from AUTODOC_GITHUB_TOKEN or the
// repository.token config field. The X-GitHub-Api-Version header is always
// sent to opt into a stable API version.
//
// Pull requests created by the bot are tagged with the "autodoc-bot" label so
// that [OpenBotMRs] can identify them for the open-MR deduplication check.
package github
