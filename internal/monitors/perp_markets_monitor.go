package monitors

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/validaoxyz/hyperliquid-exporter/internal/config"
	hyperliquidapi "github.com/validaoxyz/hyperliquid-exporter/internal/hyperliquid-api"
	"github.com/validaoxyz/hyperliquid-exporter/internal/logger"
	"github.com/validaoxyz/hyperliquid-exporter/internal/metrics"
)

var perpMarketsResolver *hyperliquidapi.Resolver

// symbolInfo holds parsed symbol information
type symbolInfo struct {
	fullSymbol string // full symbol as provided (e.g., "flx:BTC" or "BTC")
	dex        string // dex prefix (empty string for native)
	market     string // market name without dex prefix (e.g., "BTC")
}

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
	// parse symbols and group by dex
	symbolsByDex := groupSymbolsByDex(cfg.PerpMarketSymbols)

	// fetch data for each dex
	for dex, symbols := range symbolsByDex {
		dexLabel := dex
		if dexLabel == "" {
			dexLabel = "native"
		}

		logger.Debug("Fetching markets for dex: %s (markets: %v)", dexLabel, getMarketNames(symbols))

		marketData, err := perpMarketsResolver.GetMarketData(ctx, dex)
		if err != nil {
			logger.ErrorComponent("perp_markets", "Failed to fetch market data for dex %s: %v", dexLabel, err)
			continue
		}

		if err := processMarketData(marketData, symbols, dex); err != nil {
			logger.ErrorComponent("perp_markets", "Failed to process market data for dex %s: %v", dexLabel, err)
			continue
		}
	}

	return nil
}

// groupSymbolsByDex parses symbols and groups them by dex
func groupSymbolsByDex(symbols []string) map[string][]symbolInfo {
	result := make(map[string][]symbolInfo)

	for _, fullSymbol := range symbols {
		dex, market := extractDexAndMarket(fullSymbol)

		info := symbolInfo{
			fullSymbol: fullSymbol,
			dex:        dex,
			market:     market,
		}

		result[dex] = append(result[dex], info)
	}

	return result
}

// extractDexAndMarket splits symbol into dex and market parts
// Examples: "flx:BTC" -> ("flx", "BTC"), "BTC" -> ("", "BTC")
func extractDexAndMarket(symbol string) (dex string, market string) {
	parts := strings.Split(symbol, ":")
	if len(parts) >= 2 {
		return parts[0], parts[1]
	}
	return "", symbol
}

// getMarketNames extracts just market names from symbolInfo slice
func getMarketNames(symbols []symbolInfo) []string {
	names := make([]string, len(symbols))
	for i, s := range symbols {
		names[i] = s.market
	}
	return names
}

// processes market data and updates metrics
func processMarketData(data *hyperliquidapi.MarketData, symbols []symbolInfo, dex string) error {
	// create map for fast market name lookup
	// API returns full name with dex prefix for dex markets (e.g., "flx:TSLA")
	// and just market name for native markets (e.g., "BTC")
	symbolMap := make(map[string]symbolInfo)
	for _, s := range symbols {
		apiName := s.market
		if dex != "" {
			apiName = dex + ":" + s.market
		}
		symbolMap[apiName] = s
	}

	// iterate through markets (universe and contexts have matching indices)
	for i, asset := range data.Universe {
		// skip if not in enabled list
		symbolInfo, found := symbolMap[asset.Name]
		if !found {
			continue
		}

		context := data.Contexts[i]

		// parse string values to float64
		markPx, err := parseFloatValue(context.MarkPx)
		if err != nil {
			logger.Debug("Failed to parse mark price for %s: %v", symbolInfo.fullSymbol, err)
			continue
		}

		funding, err := parseFloatValue(context.Funding)
		if err != nil {
			logger.Debug("Failed to parse funding for %s: %v", symbolInfo.fullSymbol, err)
			funding = 0 // non-fatal, use 0
		}

		openInterest, err := parseFloatValue(context.OpenInterest)
		if err != nil {
			logger.Debug("Failed to parse open interest for %s: %v", symbolInfo.fullSymbol, err)
			openInterest = 0
		}

		volume, err := parseFloatValue(context.DayNtlVlm)
		if err != nil {
			logger.Debug("Failed to parse 24h volume for %s: %v", symbolInfo.fullSymbol, err)
			volume = 0
		}

		premium, err := parseFloatValue(context.Premium)
		if err != nil {
			logger.Debug("Failed to parse premium for %s: %v", symbolInfo.fullSymbol, err)
			premium = 0
		}

		oraclePx, err := parseFloatValue(context.OraclePx)
		if err != nil {
			logger.Debug("Failed to parse oracle price for %s: %v", symbolInfo.fullSymbol, err)
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

		// set metrics using full symbol name (with dex prefix if applicable)
		metrics.SetPerpMarketMarkPrice(symbolInfo.fullSymbol, markPx)
		metrics.SetPerpMarketFundingRate(symbolInfo.fullSymbol, funding)
		metrics.SetPerpMarketOpenInterest(symbolInfo.fullSymbol, openInterest)
		metrics.SetPerpMarket24hVolume(symbolInfo.fullSymbol, volume)
		metrics.SetPerpMarketPremium(symbolInfo.fullSymbol, premium)
		metrics.SetPerpMarketOraclePrice(symbolInfo.fullSymbol, oraclePx)
		metrics.SetPerpMarketMidPrice(symbolInfo.fullSymbol, midPx)

		if impactBid > 0 {
			metrics.SetPerpMarketImpactBid(symbolInfo.fullSymbol, impactBid)
		}
		if impactAsk > 0 {
			metrics.SetPerpMarketImpactAsk(symbolInfo.fullSymbol, impactAsk)
		}

		logger.Debug("Perpetual market %s: mark=%.2f, funding=%.6f, OI=%.2f, vol=%.2f",
			symbolInfo.fullSymbol, markPx, funding, openInterest, volume)

		// fetch orderbook and calculate liquidity depth
		go fetchAndCalculateLiquidity(symbolInfo.fullSymbol, midPx)
	}

	return nil
}

// fetchAndCalculateLiquidity fetches orderbook and calculates liquidity depth at different bps levels
func fetchAndCalculateLiquidity(symbol string, currentPrice float64) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// fetch L2 book
	book, err := perpMarketsResolver.GetL2Book(ctx, symbol)
	if err != nil {
		logger.Debug("Failed to fetch L2 book for %s: %v", symbol, err)
		return
	}

	// calculate liquidity for each bps level
	bpsLevels := []int{5, 10, 50, 100}
	for _, bps := range bpsLevels {
		// calculate bid liquidity (selling into bids)
		bidLiquidity := calculateLiquidityDepth(book.Levels[0], currentPrice, bps, true)

		// calculate ask liquidity (buying from asks)
		askLiquidity := calculateLiquidityDepth(book.Levels[1], currentPrice, bps, false)

		// set metrics
		switch bps {
		case 5:
			metrics.SetPerpMarketLiquidityBid5bps(symbol, bidLiquidity)
			metrics.SetPerpMarketLiquidityAsk5bps(symbol, askLiquidity)
		case 10:
			metrics.SetPerpMarketLiquidityBid10bps(symbol, bidLiquidity)
			metrics.SetPerpMarketLiquidityAsk10bps(symbol, askLiquidity)
		case 50:
			metrics.SetPerpMarketLiquidityBid50bps(symbol, bidLiquidity)
			metrics.SetPerpMarketLiquidityAsk50bps(symbol, askLiquidity)
		case 100:
			metrics.SetPerpMarketLiquidityBid100bps(symbol, bidLiquidity)
			metrics.SetPerpMarketLiquidityAsk100bps(symbol, askLiquidity)
		}
	}

	logger.Debug("Calculated liquidity depth for %s", symbol)
}

// calculateLiquidityDepth calculates notional amount to move price by X bps
// isBid: true for bids (selling), false for asks (buying)
func calculateLiquidityDepth(levels []hyperliquidapi.OrderLevel, currentPrice float64, bps int, isBid bool) float64 {
	if currentPrice == 0 {
		return 0
	}

	// calculate target price
	bpsFactor := float64(bps) / 10000.0
	var targetPrice float64
	if isBid {
		// selling into bids: target is lower price
		targetPrice = currentPrice * (1 - bpsFactor)
	} else {
		// buying from asks: target is higher price
		targetPrice = currentPrice * (1 + bpsFactor)
	}

	// accumulate notional until reaching target price
	var totalNotional float64

	for _, level := range levels {
		px, err := parseFloatValue(level.Px)
		if err != nil {
			continue
		}

		sz, err := parseFloatValue(level.Sz)
		if err != nil {
			continue
		}

		// check if we've reached the target price
		if isBid && px < targetPrice {
			break
		}
		if !isBid && px > targetPrice {
			break
		}

		// accumulate notional (price * size)
		totalNotional += px * sz
	}

	return totalNotional
}

// helper to parse string to float64
func parseFloatValue(s string) (float64, error) {
	if s == "" {
		return 0, fmt.Errorf("empty string")
	}
	return strconv.ParseFloat(s, 64)
}
