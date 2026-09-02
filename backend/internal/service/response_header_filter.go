package service

import (
	"github.com/JnyRoad/RelayDeck/internal/config"
	"github.com/JnyRoad/RelayDeck/internal/util/responseheaders"
)

func compileResponseHeaderFilter(cfg *config.Config) *responseheaders.CompiledHeaderFilter {
	if cfg == nil {
		return nil
	}
	return responseheaders.CompileHeaderFilter(cfg.Security.ResponseHeaders)
}
