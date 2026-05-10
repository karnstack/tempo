package secret

import "github.com/karnstack/tempo/internal/config"

// NewBoxFx is the fx adapter that builds a *Box from cfg.Secret.Key.
func NewBoxFx(cfg *config.Config) (*Box, error) {
	return NewBox(cfg.Secret.Key)
}
