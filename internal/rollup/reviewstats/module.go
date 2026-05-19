package reviewstats

import (
	"github.com/karnstack/tempo/internal/rollup"
	"go.uber.org/fx"
)

// Module provides *Aggregator into the "rollup.aggregators" value
// group consumed by rollup.Run. Mirrors engineerstats.Module,
// repostats.Module, and cycletime.Module.
var Module = fx.Module("rollup.review_stats",
	fx.Provide(
		fx.Annotate(
			New,
			fx.As(new(rollup.Aggregator)),
			fx.ResultTags(`group:"rollup.aggregators"`),
		),
	),
)
