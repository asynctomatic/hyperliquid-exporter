package monitors

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/validaoxyz/hyperliquid-exporter/internal/config"
	"github.com/validaoxyz/hyperliquid-exporter/internal/logger"
	"github.com/validaoxyz/hyperliquid-exporter/internal/metrics"
)

// PriceInfo represents price information for a coin
type PriceInfo struct {
	Px             string `json:"px"`
	LastUpdateTime string `json:"last_update_time"`
	DailyPx        string `json:"daily_px"`
}

// OraclePrices contains all price mappings
type OraclePrices struct {
	CoinToMarkPx         [][]interface{} `json:"coin_to_mark_px"`
	CoinToOraclePx       [][]interface{} `json:"coin_to_oracle_px"`
	CoinToExternalPerpPx [][]interface{} `json:"coin_to_external_perp_px"`
}

// HIP3OracleUpdate represents a single oracle update from a deployer
type HIP3OracleUpdate struct {
	UpdateClass          string          `json:"update_class"`
	MarkPxInputs         [][]interface{} `json:"mark_px_inputs"`
	SpotPxInputs         [][]interface{} `json:"spot_px_inputs"`
	ExternalPerpPxInputs [][]interface{} `json:"external_perp_px_inputs"`
	OraclePxs            OraclePrices    `json:"oracle_pxs"`
}

// StartHIP3OracleUpdatesMonitor monitors HIP3 oracle updates from hourly logs
func StartHIP3OracleUpdatesMonitor(ctx context.Context, cfg config.Config, errCh chan<- error) {
	if !cfg.EnableHIP3Oracle {
		logger.InfoComponent("oracle", "HIP3 oracle monitoring disabled")
		return
	}

	oracleDir := filepath.Join(cfg.NodeHome, "data", "hip3_oracle_updates", "hourly")
	logger.InfoComponent("oracle", "Starting HIP3 oracle updates monitor for directory: %s", oracleDir)

	if _, err := os.Stat(oracleDir); os.IsNotExist(err) {
		logger.WarningComponent("oracle", "HIP3 oracle updates directory %s does not exist (node may need --write-hip3-oracle-updates flag)", oracleDir)
		return
	}

	var currentFile string
	var fileReader *bufio.Reader
	isFirstRun := true

	for {
		select {
		case <-ctx.Done():
			return
		default:
			// find the latest file in the hourly organized directory
			latestFile, err := findLatestOracleFile(oracleDir)
			if err != nil {
				errCh <- fmt.Errorf("error finding latest HIP3 oracle file: %w", err)
				time.Sleep(5 * time.Second)
				continue
			}

			if latestFile == "" {
				logger.DebugComponent("oracle", "No HIP3 oracle update files found yet")
				time.Sleep(5 * time.Second)
				continue
			}

			// if a new file is found, switch to it
			if latestFile != currentFile {
				logger.InfoComponent("oracle", "Switching to new HIP3 oracle file: %s", latestFile)
				if fileReader != nil {
					fileReader = nil // allow garbage collection
				}

				file, err := os.Open(latestFile)
				if err != nil {
					errCh <- fmt.Errorf("error opening HIP3 oracle file: %w", err)
					time.Sleep(1 * time.Second)
					continue
				}

				if isFirstRun {
					// on first run, seek to end to only capture new updates
					_, err = file.Seek(0, io.SeekEnd)
					if err != nil {
						errCh <- fmt.Errorf("error seeking to end of oracle file: %w", err)
						file.Close()
						time.Sleep(1 * time.Second)
						continue
					}
					logger.InfoComponent("oracle", "First run: streaming from end of file %s", latestFile)
				} else {
					logger.InfoComponent("oracle", "Reading entire oracle file %s", latestFile)
				}

				fileReader = bufio.NewReader(file)
				currentFile = latestFile
				isFirstRun = false
			}

			// read and process lines
			for {
				line, err := fileReader.ReadString('\n')
				if err != nil {
					if err == io.EOF {
						// end of file, wait before checking for more data
						time.Sleep(100 * time.Millisecond)
						break
					}
					errCh <- fmt.Errorf("error reading HIP3 oracle file: %w", err)
					break
				}

				// skip empty lines
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}

				if err := parseOracleUpdateLine(ctx, line); err != nil {
					logger.DebugComponent("oracle", "Skipping potentially incomplete oracle update line: %v", err)
				}
			}
		}
	}
}

// findLatestOracleFile walks the hourly organized directory structure to find the latest file
func findLatestOracleFile(baseDir string) (string, error) {
	// walk the directory structure to find the most recent file
	// structure is: baseDir/{date}/{hour}
	var latestFile string
	var latestTime time.Time

	err := filepath.Walk(baseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip errors, continue walking
		}

		if info.IsDir() {
			return nil
		}

		// only consider files (hourly logs are typically named by hour like "00", "01", etc.)
		if info.ModTime().After(latestTime) {
			latestTime = info.ModTime()
			latestFile = path
		}

		return nil
	})

	if err != nil {
		return "", err
	}

	return latestFile, nil
}

// parseOracleUpdateLine parses a single line from the oracle updates file
func parseOracleUpdateLine(ctx context.Context, line string) error {
	var update HIP3OracleUpdate
	if err := json.Unmarshal([]byte(line), &update); err != nil {
		return fmt.Errorf("error parsing oracle update JSON: %w", err)
	}

	// increment total oracle updates counter
	metrics.IncrementHIP3OracleUpdatesTotal()

	// extract deployer from first coin (e.g., "flx:NVDA" -> "flx")
	deployer := extractDeployerFromUpdate(&update)
	if deployer != "" {
		metrics.IncrementHIP3OracleUpdatesByDeployer(deployer)

		// track updates by update_class and deployer
		if update.UpdateClass != "" {
			metrics.IncrementHIP3OracleUpdatesByClassAndDeployer(update.UpdateClass, deployer)
		}
	}

	// track unique markets for this update
	uniqueMarkets := make(map[string]bool)

	// process mark prices
	markets := processPriceData(update.OraclePxs.CoinToMarkPx, "mark_px", deployer)
	for market := range markets {
		uniqueMarkets[market] = true
	}

	// process oracle prices
	markets = processPriceData(update.OraclePxs.CoinToOraclePx, "oracle_px", deployer)
	for market := range markets {
		uniqueMarkets[market] = true
	}

	// process external perp prices
	markets = processPriceData(update.OraclePxs.CoinToExternalPerpPx, "external_perp_px", deployer)
	for market := range markets {
		uniqueMarkets[market] = true
	}

	// set markets per deployer count
	if deployer != "" {
		metrics.SetHIP3OracleMarketsPerDeployer(deployer, int64(len(uniqueMarkets)))
	}

	logger.DebugComponent("oracle", "Processed HIP3 oracle update: deployer=%s, update_class=%s, markets=%d",
		deployer, update.UpdateClass, len(uniqueMarkets))

	return nil
}

// extractDeployerFromUpdate extracts the deployer prefix from the first coin in the update
func extractDeployerFromUpdate(update *HIP3OracleUpdate) string {
	// try to get deployer from mark_px data
	if len(update.OraclePxs.CoinToMarkPx) > 0 {
		if len(update.OraclePxs.CoinToMarkPx[0]) > 0 {
			if coin, ok := update.OraclePxs.CoinToMarkPx[0][0].(string); ok {
				return extractDeployerFromCoin(coin)
			}
		}
	}

	// fallback to oracle_px data
	if len(update.OraclePxs.CoinToOraclePx) > 0 {
		if len(update.OraclePxs.CoinToOraclePx[0]) > 0 {
			if coin, ok := update.OraclePxs.CoinToOraclePx[0][0].(string); ok {
				return extractDeployerFromCoin(coin)
			}
		}
	}

	return ""
}

// extractDeployerFromCoin extracts deployer prefix from coin string (e.g., "flx:NVDA" -> "flx")
func extractDeployerFromCoin(coin string) string {
	parts := strings.Split(coin, ":")
	if len(parts) >= 2 {
		return parts[0]
	}
	return ""
}

// processPriceData processes a price data array and updates metrics
// Returns a map of unique markets (coin names without deployer prefix)
func processPriceData(priceData [][]interface{}, priceType string, deployer string) map[string]bool {
	metrics.IncrementHIP3OracleUpdatesByType(priceType)

	markets := make(map[string]bool)

	for _, entry := range priceData {
		if len(entry) < 2 {
			continue
		}

		coin, ok := entry[0].(string)
		if !ok {
			continue
		}

		priceInfoMap, ok := entry[1].(map[string]interface{})
		if !ok {
			continue
		}

		// extract price
		pxStr, ok := priceInfoMap["px"].(string)
		if !ok {
			continue
		}

		px, err := parseFloat(pxStr)
		if err != nil {
			logger.DebugComponent("oracle", "Failed to parse price %s: %v", pxStr, err)
			continue
		}

		// set latest value metric with coin and type labels
		metricKey := fmt.Sprintf("%s:%s", coin, priceType)
		metrics.SetHIP3OracleLatestValue(metricKey, px)

		// extract market name (coin without deployer prefix)
		market := extractMarketFromCoin(coin)
		if market != "" {
			markets[market] = true
		}

		// track latest update timestamp
		if lastUpdateTimeStr, ok := priceInfoMap["last_update_time"].(string); ok {
			parsedTime, err := parseOracleTimestamp(lastUpdateTimeStr)
			if err == nil {
				metrics.SetHIP3OracleLatestUpdateTime(parsedTime.Unix())

				// track last update time per deployer and market
				if deployer != "" && market != "" {
					metrics.SetHIP3OracleMarketLastUpdateTime(deployer, market, parsedTime.Unix())
				}
			}
		}
	}

	return markets
}

// extractMarketFromCoin extracts market name from coin string (e.g., "flx:NVDA" -> "NVDA")
func extractMarketFromCoin(coin string) string {
	parts := strings.Split(coin, ":")
	if len(parts) >= 2 {
		return parts[1]
	}
	return coin
}

// parseFloat parses a string to float64
func parseFloat(s string) (float64, error) {
	var f float64
	_, err := fmt.Sscanf(s, "%f", &f)
	return f, err
}

// parseOracleTimestamp attempts to parse common timestamp formats
func parseOracleTimestamp(timestamp string) (time.Time, error) {
	// try RFC3339 format first (most common)
	if t, err := time.Parse(time.RFC3339, timestamp); err == nil {
		return t, nil
	}

	// try RFC3339Nano format
	if t, err := time.Parse(time.RFC3339Nano, timestamp); err == nil {
		return t, nil
	}

	// try custom format used elsewhere in the codebase
	if t, err := time.Parse("2006-01-02T15:04:05.999999999", timestamp); err == nil {
		return t.UTC(), nil
	}

	return time.Time{}, fmt.Errorf("unable to parse timestamp: %s", timestamp)
}
