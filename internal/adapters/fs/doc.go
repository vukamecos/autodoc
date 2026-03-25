// Package fs implements [domain.DocumentStorePort] and
// [domain.DocumentWriterPort] using the local filesystem.
//
// [Writer.WriteDocument] writes files atomically: content is first written to
// a temporary file in the same directory, then renamed into place. This
// prevents partial writes from corrupting documentation files.
//
// The allowed-paths list (from documentation.allowed_paths) is enforced at
// write time: any path that does not match one of the configured glob patterns
// is rejected with [domain.ErrForbiddenPath].
package fs
