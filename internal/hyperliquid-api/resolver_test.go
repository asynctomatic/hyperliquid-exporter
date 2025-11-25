package hyperliquidapi

import (
	"context"
	"testing"
	"time"
)

func TestGetMarketData(t *testing.T) {
	resolver := NewResolver("mainnet")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// test native dex (empty string)
	data, err := resolver.GetMarketData(ctx, "")
	if err != nil {
		t.Fatalf("GetMarketData() error = %v", err)
	}

	if len(data.Universe) != len(data.Contexts) {
		t.Errorf("Universe length %d != Contexts length %d", len(data.Universe), len(data.Contexts))
	}
}
