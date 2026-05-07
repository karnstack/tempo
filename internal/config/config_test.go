package config

import "testing"

func TestParseDB(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		raw       string
		wantDrv   string
		wantDSN   string
		wantError bool
	}{
		{name: "sqlite_filesystem", raw: "sqlite://./data/tempo.db", wantDrv: "sqlite", wantDSN: "./data/tempo.db"},
		{name: "sqlite_memory", raw: "sqlite://:memory:", wantDrv: "sqlite", wantDSN: ":memory:"},
		{name: "postgres_url", raw: "postgres://u:p@h/db", wantDrv: "postgres", wantDSN: "postgres://u:p@h/db"},
		{name: "postgresql_alias", raw: "postgresql://u:p@h/db", wantDrv: "postgres", wantDSN: "postgresql://u:p@h/db"},
		{name: "unsupported", raw: "mysql://x", wantError: true},
		{name: "empty", raw: "", wantError: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseDB(tc.raw)
			if tc.wantError {
				if err == nil {
					t.Fatalf("parseDB(%q): expected error, got %+v", tc.raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseDB(%q): unexpected error %v", tc.raw, err)
			}
			if got.Driver != tc.wantDrv {
				t.Errorf("driver = %q, want %q", got.Driver, tc.wantDrv)
			}
			if got.DSN != tc.wantDSN {
				t.Errorf("dsn = %q, want %q", got.DSN, tc.wantDSN)
			}
			if got.Raw != tc.raw {
				t.Errorf("raw = %q, want %q", got.Raw, tc.raw)
			}
		})
	}
}
