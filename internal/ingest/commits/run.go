package commits

import (
	"github.com/karnstack/tempo/internal/ingest"
	"go.uber.org/fx"
)

// Module is the fx wiring for the commits ingest runner. It provides
// *Runner as an ingest.Runner into the "ingest.runners" value group
// consumed by the scheduler.
var Module = fx.Module("ingest.commits",
	fx.Provide(
		fx.Annotate(
			New,
			fx.As(new(ingest.Runner)),
			fx.ResultTags(`group:"ingest.runners"`),
		),
	),
)
