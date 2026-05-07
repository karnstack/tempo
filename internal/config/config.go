// Package config loads tempo's runtime configuration from environment variables.
package config

import "os"

type Config struct {
	Listen string
	Env    string
}

func Load() *Config {
	return &Config{
		Listen: getenv("TEMPO_LISTEN", ":8080"),
		Env:    getenv("TEMPO_ENV", "development"),
	}
}

func IsDev() bool {
	return getenv("TEMPO_ENV", "development") == "development"
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
