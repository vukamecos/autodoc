// Package usecase implements the autodoc business logic.
//
// The central type is [RunDocUpdateUseCase], which orchestrates the full
// documentation update pipeline:
//
//  1. Load persisted run state.
//  2. Fetch the remote repository and check for new commits.
//  3. Check whether an open bot MR/PR already exists (skip if so).
//  4. Compute the diff between the last processed SHA and HEAD.
//  5. Analyse changes ([ChangeAnalyzer]) to classify files by category and impact.
//  6. Map changed files to target documentation paths ([DocumentMapper]).
//  7. Deduplicate via a context hash — skip if this exact change-set was
//     already processed in a previous run.
//  8. For each target document: read, call the LLM ([generateWithChunking]),
//     apply a section-aware patch ([markdown.PatchDocument]), validate, write.
//  9. Create a branch, commit the updated documents, and open an MR/PR.
//  10. Save the updated run state.
//
// Supporting types:
//   - [ChangeAnalyzer] — classifies [domain.FileDiff] values into categories
//     (code, config, infrastructure, test, documentation) and impact zones.
//   - [DocumentMapper] — maps analyzed changes to documentation file paths
//     using the rules defined in the YAML configuration.
//   - [ModelSelector] — selects the best Ollama model for a given diff size
//     when acp.model is not explicitly configured.
//
// Chunking
//
// When the total diff size exceeds the configured context budget,
// [generateWithChunking] splits changes into sequential chunks. Each chunk
// is processed in order and the updated document content is fed forward so
// every chunk sees the latest version.
package usecase
