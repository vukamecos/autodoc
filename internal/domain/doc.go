// Package domain contains the core business entities, port interfaces, and
// sentinel errors for autodoc.
//
// Nothing in this package imports external packages — it is the innermost
// layer of the Clean Architecture and the stable foundation that all other
// layers depend on.
//
// Entities
//
// The primary data types are:
//   - [FileDiff] — a single file change returned by the VCS.
//   - [AnalyzedChange] — a diff enriched with category and impact information.
//   - [Document] — a documentation file (path + content).
//   - [MergeRequest] — a pull/merge request with ID, URL, and metadata.
//   - [RunState] — persisted state of the last pipeline execution.
//   - [ACPRequest] / [ACPResponse] — messages exchanged with the LLM provider.
//
// Port interfaces
//
// All interactions with external systems are expressed as interfaces:
//   - [RepositoryPort] — fetch and diff a remote VCS project.
//   - [MRCreatorPort] — create branches, commit files, and open MRs/PRs.
//   - [ACPClientPort] — call the LLM (ACP agent or Ollama) to generate updates.
//   - [StateStorePort] — load/save run state to persistent storage.
//   - [DocumentStorePort] / [DocumentWriterPort] — read and write documentation files.
//   - [ChangeAnalyzerPort] — classify file diffs by category and impact zone.
//   - [DocumentMapperPort] — map changed files to target documentation paths.
//   - [ValidationPort] — validate proposed document updates before committing.
package domain
