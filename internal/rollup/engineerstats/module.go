package engineerstats

import (
	"github.com/karnstack/tempo/internal/rollup"
	"go.uber.org/fx"
)

// Module provides *Aggregator into the "rollup.aggregators" value group
// consumed by rollup.Run. Mirrors the per-runner pattern used by the
// ingest modules (e.g. internal/ingest/commits/run.go).
var Module = fx.Module("rollup.engineer_stats",
	fx.Provide(
		fx.Annotate(
			New,
			fx.As(new(rollup.Aggregator)),
			fx.ResultTags(`group:"rollup.aggregators"`),
		),
	),
)
