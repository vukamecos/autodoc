// Package gitlab implements [domain.RepositoryPort] and [domain.MRCreatorPort]
// using the GitLab REST API v4.
//
// No local git clone is required: all operations (fetch, diff, branch creation,
// file commits, MR creation) are performed through the API, making the adapter
// suitable for running in ephemeral environments without persistent disk storage.
//
// Authentication uses a PRIVATE-TOKEN header. The token must be supplied via
// the AUTODOC_GITLAB_TOKEN environment variable or the repository.token config
// field — it must never appear in committed config files.
package gitlab
