// Package version exposes the build version string. It is overridden at build
// time via -ldflags and lives in its own leaf package to avoid import cycles.
package version

var Version = "dev"
