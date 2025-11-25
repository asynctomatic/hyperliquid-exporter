package monitors

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/validaoxyz/hyperliquid-exporter/internal/config"
	hyperliquidapi "github.com/validaoxyz/hyperliquid-exporter/internal/hyperliquid-api"
	"github.com/validaoxyz/hyperliquid-exporter/internal/logger"
	"github.com/validaoxyz/hyperliquid-exporter/internal/metrics"
)

var perpMarketsResolver *hyperliquidapi.Resolver

// monitors perpetual market data from the info API endpoint
func StartPerpMarketsMonitor(ctx context.Context, cfg config.Config, errCh chan<- error) {
	// check if any perpetual markets are configured
	if len(cfg.PerpMarketSymbols) == 0 {
		logger.InfoComponent("perp_markets", "Perpetual markets monitor disabled - no symbols specified via --perp-markets")
		return
	}

	// initialize resolver
	perpMarketsResolver = hyperliquidapi.NewResolver(cfg.Chain)

	logger.InfoComponent("perp_markets", "Monitoring perpetual markets: %v", cfg.PerpMarketSymbols)
	logger.InfoComponent("perp_markets", "Using API endpoint: %s", perpMarketsResolver.GetBaseURL())

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		// fetch immediately on start
		if err := updatePerpMarketMetrics(ctx, cfg); err != nil {
			errCh <- fmt.Errorf("perp markets monitor: %w", err)
		}

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := updatePerpMarketMetrics(ctx, cfg); err != nil {
					errCh <- fmt.Errorf("perp markets monitor: %w", err)
				}
			}
		}
	}()
}

// fetches and processes perpetual market data
func updatePerpMarketMetrics(ctx context.Context, cfg config.Config) error {
	// fetch market data using resolver
	marketData, err := perpMarketsResolver.GetMarketData(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch market data: %w", err)
	}

	return processMarketData(marketData, cfg.PerpMarketSymbols)
}

// processes market data and updates metrics
func processMarketData(data *hyperliquidapi.MarketData, enabledSymbols []string) error {
	// create map for fast symbol lookup
	enabledMap := make(map[string]bool)
	for _, symbol := range enabledSymbols {
		enabledMap[symbol] = true
	}

	// iterate through markets (universe and contexts have matching indices)
	for i, asset := range data.Universe {
		// skip if not in enabled list
		if !enabledMap[asset.Name] {
			continue
		}

		context := data.Contexts[i]

		// parse string values to float64
		markPx, err := parseFloatValue(context.MarkPx)
		if err != nil {
			logger.Debug("Failed to parse mark price for %s: %v", asset.Name, err)
			continue
		}

		funding, err := parseFloatValue(context.Funding)
		if err != nil {
			logger.Debug("Failed to parse funding for %s: %v", asset.Name, err)
			funding = 0 // non-fatal, use 0
		}

		openInterest, err := parseFloatValue(context.OpenInterest)
		if err != nil {
			logger.Debug("Failed to parse open interest for %s: %v", asset.Name, err)
			openInterest = 0
		}

		volume, err := parseFloatValue(context.DayNtlVlm)
		if err != nil {
			logger.Debug("Failed to parse 24h volume for %s: %v", asset.Name, err)
			volume = 0
		}

		premium, err := parseFloatValue(context.Premium)
		if err != nil {
			logger.Debug("Failed to parse premium for %s: %v", asset.Name, err)
			premium = 0
		}

		oraclePx, err := parseFloatValue(context.OraclePx)
		if err != nil {
			logger.Debug("Failed to parse oracle price for %s: %v", asset.Name, err)
			oraclePx = markPx // fallback to mark price
		}

		// optional fields
		midPx := markPx // default to mark price
		if context.MidPx != "" {
			if parsed, err := parseFloatValue(context.MidPx); err == nil {
				midPx = parsed
			}
		}

		var impactBid, impactAsk float64
		if len(context.ImpactPxs) >= 2 {
			if parsed, err := parseFloatValue(context.ImpactPxs[0]); err == nil {
				impactBid = parsed
			}
			if parsed, err := parseFloatValue(context.ImpactPxs[1]); err == nil {
				impactAsk = parsed
			}
		}

		// set metrics
		metrics.SetPerpMarketMarkPrice(asset.Name, markPx)
		metrics.SetPerpMarketFundingRate(asset.Name, funding)
		metrics.SetPerpMarketOpenInterest(asset.Name, openInterest)
		metrics.SetPerpMarket24hVolume(asset.Name, volume)
		metrics.SetPerpMarketPremium(asset.Name, premium)
		metrics.SetPerpMarketOraclePrice(asset.Name, oraclePx)
		metrics.SetPerpMarketMidPrice(asset.Name, midPx)

		if impactBid > 0 {
			metrics.SetPerpMarketImpactBid(asset.Name, impactBid)
		}
		if impactAsk > 0 {
			metrics.SetPerpMarketImpactAsk(asset.Name, impactAsk)
		}

		logger.Debug("Perpetual market %s: mark=%.2f, funding=%.6f, OI=%.2f, vol=%.2f",
			asset.Name, markPx, funding, openInterest, volume)
	}

	return nil
}

// helper to parse string to float64
func parseFloatValue(s string) (float64, error) {
	if s == "" {
		return 0, fmt.Errorf("empty string")
	}
	return strconv.ParseFloat(s, 64)
}
