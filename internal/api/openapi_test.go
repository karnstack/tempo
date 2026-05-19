package api

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	apiauth "github.com/karnstack/tempo/internal/api/auth"
	"github.com/karnstack/tempo/internal/api/connections"
	"github.com/karnstack/tempo/internal/api/engineers"
	"github.com/karnstack/tempo/internal/api/health"
	"github.com/karnstack/tempo/internal/api/me"
	"github.com/karnstack/tempo/internal/api/orgs"
	"github.com/karnstack/tempo/internal/api/repos"
	apisync "github.com/karnstack/tempo/internal/api/sync"
	"github.com/karnstack/tempo/internal/api/tokens"
	intauth "github.com/karnstack/tempo/internal/auth"
	"github.com/karnstack/tempo/internal/config"
	"github.com/karnstack/tempo/internal/secret"
	"github.com/karnstack/tempo/internal/storage/sqlite"
	"github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb"
	"github.com/karnstack/tempo/migrations"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	"go.uber.org/fx/fxtest"
	"go.uber.org/zap/zaptest"
)

// TestOpenAPISpec_LoadsAndValidates parses the embedded YAML and
// runs the openapi3 validator against it. Catches structural errors
// before they ship.
func TestOpenAPISpec_LoadsAndValidates(t *testing.T) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(openapiYAML)
	if err != nil {
		t.Fatalf("load openapi.yaml: %v", err)
	}
	if err := doc.Validate(loader.Context); err != nil {
		t.Fatalf("validate openapi.yaml: %v", err)
	}
}

// TestOpenAPISpec_CoversAllRoutes builds the real echo router via
// the existing Configure functions and verifies that every /api/v1
// route registered on the router has a matching path + method in
// the OpenAPI document. Drift between code and spec is the most
// common review-time regression; this guards against it cheaply.
func TestOpenAPISpec_CoversAllRoutes(t *testing.T) {
	doc := loadDoc(t)
	e := buildRouter(t)

	for _, r := range e.Routes() {
		path := r.Path
		method := r.Method
		switch {
		case !strings.HasPrefix(path, "/api/v1/"):
			// /api/openapi.yaml, SPA fallback, framework internals.
			continue
		case strings.HasSuffix(path, "/*"):
			// Echo's 404 fallback under the /api/v1 group; not a
			// real route to document.
			continue
		case method == "" || method == http.MethodOptions:
			continue
		}
		oaPath := strings.TrimPrefix(path, "/api/v1")
		oaPath = echoToOpenAPIPath(oaPath)
		pathItem := doc.Paths.Value(oaPath)
		if pathItem == nil {
			t.Errorf("openapi.yaml missing path: %s (echo route: %s %s)", oaPath, method, path)
			continue
		}
		if op := pathItem.GetOperation(method); op == nil {
			t.Errorf("openapi.yaml missing %s %s (echo route: %s %s)", method, oaPath, method, path)
		}
	}
}

// echoToOpenAPIPath rewrites echo's `:param` segments to OpenAPI's
// `{param}` form.
func echoToOpenAPIPath(p string) string {
	parts := strings.Split(p, "/")
	for i, seg := range parts {
		if strings.HasPrefix(seg, ":") {
			parts[i] = "{" + seg[1:] + "}"
		}
	}
	return strings.Join(parts, "/")
}

// buildRouter constructs a full echo instance with every Configure
// wired against a real on-disk sqlite + auth stack. Mirrors the
// production Run() body without the lifecycle hooks.
func buildRouter(t *testing.T) *echo.Echo {
	t.Helper()
	lc := fxtest.NewLifecycle(t)
	l := zaptest.NewLogger(t)
	path := filepath.Join(t.TempDir(), "openapi.db")
	cfg := &config.Config{
		Database: config.Database{Driver: "sqlite", DSN: path, Raw: "sqlite://" + path},
		Rollup:   config.Rollup{Timezone: time.UTC, Hour: 2},
		Poll:     config.Poll{BackfillDays: 90},
	}
	s, err := sqlite.New(lc, l, cfg)
	if err != nil {
		t.Fatalf("sqlite.New: %v", err)
	}
	if err := migrations.Apply(context.Background(), s.DB()); err != nil {
		t.Fatalf("migrations.Apply: %v", err)
	}
	lc.RequireStart()
	t.Cleanup(lc.RequireStop)

	q := sqlitedb.New(s.DB())
	m := intauth.NewManager(q, time.Hour, false)
	r := intauth.NewRegistrar(q)
	a := intauth.NewAuthenticator(q)
	key := make([]byte, 32)
	box, err := secret.NewBox(key)
	if err != nil {
		t.Fatalf("NewBox: %v", err)
	}

	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	health.Configure(e, l)
	apiauth.Configure(e, l, m, r, a)
	me.Configure(e, l, m, q)
	tokens.Configure(e, l, m, q, box)
	connections.Configure(e, l, m, q, cfg)
	repos.Configure(e, l, m, q, cfg)
	orgs.Configure(e, l, m, q, cfg)
	engineers.Configure(e, l, m, q, cfg)
	apisync.Configure(e, l, m, q)
	configureOpenAPI(e)
	return e
}

func loadDoc(t *testing.T) *openapi3.T {
	t.Helper()
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(openapiYAML)
	if err != nil {
		t.Fatalf("load openapi.yaml: %v", err)
	}
	return doc
}
