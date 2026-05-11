package vcr

import "os"

// Mode controls Transport behaviour. The zero value is ModeReplay so a forgotten
// mode argument fails loud on the first request rather than hitting GitHub.
type Mode int

const (
	// ModeReplay serves responses from the cassette; a missing match is an error.
	ModeReplay Mode = iota
	// ModeRecord forwards to the inner transport and appends each interaction
	// to the cassette. Authorization is redacted; response headers are
	// filtered to a stable allow-list before write.
	ModeRecord
	// ModeAuto replays if the cassette file exists, otherwise records.
	ModeAuto
)

func (m Mode) String() string {
	switch m {
	case ModeReplay:
		return "replay"
	case ModeRecord:
		return "record"
	case ModeAuto:
		return "auto"
	default:
		return "unknown"
	}
}

// ModeFromEnv returns the mode named by TEMPO_VCR (record|replay|auto), or def
// when the variable is unset or unrecognised. Useful for tests that want a
// default of ModeReplay but allow an opt-in re-record via env without
// recompiling.
func ModeFromEnv(def Mode) Mode {
	switch os.Getenv("TEMPO_VCR") {
	case "record":
		return ModeRecord
	case "replay":
		return ModeReplay
	case "auto":
		return ModeAuto
	default:
		return def
	}
}
