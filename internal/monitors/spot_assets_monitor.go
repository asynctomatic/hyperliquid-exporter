package monitors

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/validaoxyz/hyperliquid-exporter/internal/abci"
	"github.com/validaoxyz/hyperliquid-exporter/internal/config"
	"github.com/validaoxyz/hyperliquid-exporter/internal/logger"
	"github.com/validaoxyz/hyperliquid-exporter/internal/metrics"
)

// monitors ABCI state files for spot asset metrics
func StartSpotAssetsMonitor(ctx context.Context, cfg config.Config, errCh chan<- error) {
	// check if any spot assets are configured
	if len(cfg.SpotAssetSymbols) == 0 {
		logger.InfoComponent("spot_assets", "Spot assets monitor disabled - no symbols specified via --spot-assets")
		return
	}

	logger.InfoComponent("spot_assets", "Monitoring spot assets: %v", cfg.SpotAssetSymbols)

	// create ABCI reader with 10MB buffer (larger for spot asset data)
	reader := abci.NewReader(10)

	// monitor live state file (preferred)
	stateFile := filepath.Join(cfg.NodeHome, "hyperliquid_data/abci_state.rmp")

	var lastModTime time.Time

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// check if file exists
				info, err := os.Stat(stateFile)
				if err != nil {
					if os.IsNotExist(err) {
						// try fallback to periodic snapshots if live state doesn't exist
						if err := processPeriodicSnapshotForAssets(cfg, reader, cfg.SpotAssetSymbols); err != nil {
							errCh <- fmt.Errorf("spot assets monitor: %w", err)
						}
						continue
					}
					errCh <- fmt.Errorf("spot assets monitor: stat file: %w", err)
					continue
				}

				// skip if file hasn't been modified
				if info.ModTime().Equal(lastModTime) {
					continue
				}

				if err := processStateForAssets(stateFile, reader, cfg.SpotAssetSymbols); err != nil {
					errCh <- fmt.Errorf("spot assets monitor: %w", err)
					continue
				}

				lastModTime = info.ModTime()
			}
		}
	}()
}

// falls back to periodic snapshots if live state is unavailable
func processPeriodicSnapshotForAssets(cfg config.Config, reader *abci.Reader, enabledSymbols []string) error {
	stateDir := filepath.Join(cfg.NodeHome, "data/periodic_abci_states")

	// find the latest snapshot
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		return fmt.Errorf("read state dir: %w", err)
	}

	var latestFile string
	var latestTime time.Time

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".rmp") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		if info.ModTime().After(latestTime) {
			latestTime = info.ModTime()
			latestFile = filepath.Join(stateDir, entry.Name())
		}
	}

	if latestFile == "" {
		return fmt.Errorf("no snapshot files found")
	}

	return processStateForAssets(latestFile, reader, enabledSymbols)
}

func processStateForAssets(path string, reader *abci.Reader, enabledSymbols []string) error {
	// read spot asset states from ABCI state
	assetStates, err := reader.ReadSpotAssetStates(path)
	if err != nil {
		return fmt.Errorf("read spot asset states: %w", err)
	}

	if len(assetStates) == 0 {
		logger.Debug("No spot asset states found")
		return nil
	}

	// create map for fast symbol lookup
	enabledMap := make(map[string]bool)
	for _, symbol := range enabledSymbols {
		enabledMap[symbol] = true
	}

	// process each asset
	for _, asset := range assetStates {
		// skip assets with empty symbol
		if asset.Symbol == "" {
			continue
		}

		// skip assets not in the enabled list
		if !enabledMap[asset.Symbol] {
			continue
		}

		// calculate total supply (normalized by decimals)
		divisor := math.Pow10(int(asset.Decimals))
		normalizedSupply := float64(asset.TotalSupply) / divisor

		// set metrics
		metrics.SetSpotAssetTotalSupply(asset.Symbol, normalizedSupply)
		metrics.SetSpotAssetHoldersCount(asset.Symbol, int64(len(asset.Holders)))

		// calculate supply distribution
		calculateAndSetDistribution(asset)

		logger.Debug("Spot asset %s (ID: %d): supply=%.2f, holders=%d",
			asset.Symbol, asset.AssetID, normalizedSupply, len(asset.Holders))
	}

	return nil
}

// calculates supply distribution across holders and sets distribution metrics
func calculateAndSetDistribution(asset abci.SpotAssetState) {
	if len(asset.Holders) == 0 {
		return
	}

	// sort holders by balance (descending)
	holders := make([]abci.SpotAssetHolder, len(asset.Holders))
	copy(holders, asset.Holders)
	sort.Slice(holders, func(i, j int) bool {
		return holders[i].Balance > holders[j].Balance
	})

	totalSupply := float64(asset.TotalSupply)

	// define distribution buckets by percentage of supply held
	// ranges: <0.01%, 0.01-0.1%, 0.1-1%, 1-5%, 5-10%, 10-25%, 25%+
	buckets := map[string]int{
		"lt_0.01_pct":  0, // less than 0.01%
		"0.01_0.1_pct": 0, // 0.01% to 0.1%
		"0.1_1_pct":    0, // 0.1% to 1%
		"1_5_pct":      0, // 1% to 5%
		"5_10_pct":     0, // 5% to 10%
		"10_25_pct":    0, // 10% to 25%
		"gt_25_pct":    0, // greater than 25%
	}

	for _, holder := range holders {
		percentage := (float64(holder.Balance) / totalSupply) * 100.0

		switch {
		case percentage < 0.01:
			buckets["lt_0.01_pct"]++
		case percentage < 0.1:
			buckets["0.01_0.1_pct"]++
		case percentage < 1.0:
			buckets["0.1_1_pct"]++
		case percentage < 5.0:
			buckets["1_5_pct"]++
		case percentage < 10.0:
			buckets["5_10_pct"]++
		case percentage < 25.0:
			buckets["10_25_pct"]++
		default:
			buckets["gt_25_pct"]++
		}
	}

	// set distribution metrics
	for bucket, count := range buckets {
		metrics.SetSpotAssetSupplyDistribution(asset.Symbol, bucket, float64(count))
	}
}
