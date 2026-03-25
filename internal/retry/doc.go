// Package retry provides exponential-backoff retry logic for HTTP adapters.
//
// [Do] executes an HTTP request produced by a factory function, retrying on
// transient errors (connection failures and 5xx responses) up to
// [Options.MaxRetries] times with exponential backoff starting at
// [Options.RetryDelay].
//
// The factory function pattern (makeReq func() (*http.Request, error)) ensures
// that the request body reader is fresh on every attempt, which is required
// because http.Request.Body is consumed after the first send.
package retry
