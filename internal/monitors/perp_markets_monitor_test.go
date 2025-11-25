package monitors

import (
	"context"
	"testing"
	"time"

	"github.com/validaoxyz/hyperliquid-exporter/internal/config"
	hyperliquidapi "github.com/validaoxyz/hyperliquid-exporter/internal/hyperliquid-api"
)

func TestUpdatePerpMarketMetrics(t *testing.T) {
	tests := []struct {
		name    string
		symbols []string
		wantErr bool
	}{
		{
			name:    "native markets",
			symbols: []string{"BTC", "ETH"},
			wantErr: false,
		},
		{
			name:    "flx dex markets",
			symbols: []string{"flx:TSLA"},
			wantErr: false,
		},
		{
			name:    "mixed dexes",
			symbols: []string{"BTC", "flx:TSLA"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// initialize resolver
			perpMarketsResolver = hyperliquidapi.NewResolver("mainnet")
			// initialize market ID to symbol map
			marketIDToSymbol = make(map[int64]string)

			cfg := config.Config{
				Chain:             "mainnet",
				PerpMarketSymbols: tt.symbols,
			}

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			err := updatePerpMarketMetrics(ctx, cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("updatePerpMarketMetrics() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
