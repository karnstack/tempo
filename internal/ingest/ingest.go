// Package ingest hosts the periodic GitHub ingest worker. The Scheduler in
// this package owns one goroutine, ticks at TEMPO_POLL_INTERVAL, and walks
// every active connection. Per-resource fetching is delegated to Runner
// implementations registered via the fx value group "ingest.runners".
package ingest
