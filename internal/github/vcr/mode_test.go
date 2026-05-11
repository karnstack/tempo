package vcr

import "testing"

func TestModeString(t *testing.T) {
	cases := []struct {
		m    Mode
		want string
	}{
		{ModeReplay, "replay"},
		{ModeRecord, "record"},
		{ModeAuto, "auto"},
		{Mode(99), "unknown"},
	}
	for _, tc := range cases {
		if got := tc.m.String(); got != tc.want {
			t.Errorf("Mode(%d).String() = %q, want %q", tc.m, got, tc.want)
		}
	}
}

func TestModeFromEnv(t *testing.T) {
	cases := []struct {
		env  string
		def  Mode
		want Mode
	}{
		{"record", ModeReplay, ModeRecord},
		{"replay", ModeRecord, ModeReplay},
		{"auto", ModeReplay, ModeAuto},
		{"", ModeReplay, ModeReplay},
		{"", ModeRecord, ModeRecord},
		{"bogus", ModeReplay, ModeReplay},
	}
	for _, tc := range cases {
		t.Setenv("TEMPO_VCR", tc.env)
		if got := ModeFromEnv(tc.def); got != tc.want {
			t.Errorf("ModeFromEnv(env=%q, def=%v) = %v, want %v", tc.env, tc.def, got, tc.want)
		}
	}
}
