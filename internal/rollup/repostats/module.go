package repostats

import (
	"github.com/karnstack/tempo/internal/rollup"
	"go.uber.org/fx"
)

// Module provides *Aggregator into the "rollup.aggregators" value group
// consumed by rollup.Run. Mirrors engineerstats.Module so a new
// aggregator is one fx.Module per package.
var Module = fx.Module("rollup.repo_stats",
	fx.Provide(
		fx.Annotate(
			New,
			fx.As(new(rollup.Aggregator)),
			fx.ResultTags(`group:"rollup.aggregators"`),
		),
	),
)
