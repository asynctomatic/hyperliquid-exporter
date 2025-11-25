package abci

import (
	"math"
	"os"
	"sort"
	"strings"
	"testing"
)

// TestReadSpotAssetStates tests reading spot asset states from a state dump file
// To use: go test -v -run TestReadSpotAssetStates
// Set the STATE_FILE environment variable to point to your actual state dump file
func TestReadSpotAssetStates(t *testing.T) {
	stateFile := os.Getenv("STATE_FILE")
	if stateFile == "" {
		t.Skip("Set STATE_FILE environment variable to test with real data")
	}

	reader := NewReader(10) // 10MB buffer

	assetStates, err := reader.ReadSpotAssetStates(stateFile)
	if err != nil {
		t.Fatalf("ReadSpotAssetStates() error = %v", err)
	}

	if len(assetStates) == 0 {
		t.Error("ReadSpotAssetStates() returned no asset states")
	}

	// Log some basic statistics
	t.Logf("Parsed %d assets", len(assetStates))

	// Count total holders across all assets
	totalHolders := 0
	for _, asset := range assetStates {
		totalHolders += len(asset.Holders)
	}
	t.Logf("Found %d total holders across all assets", totalHolders)

	// Log first few assets as examples
	maxExamples := 5
	if len(assetStates) < maxExamples {
		maxExamples = len(assetStates)
	}
	for i := 0; i < maxExamples; i++ {
		asset := assetStates[i]
		t.Logf("Example %d: AssetID=%d, Symbol=%s, Decimals=%d, TotalSupply=%d, Holders=%d",
			i+1, asset.AssetID, asset.Symbol, asset.Decimals, asset.TotalSupply, len(asset.Holders))
	}
}

// TestPrintUserBalances tests reading and printing balances for a specific user address
// To use: go test -v -run TestPrintUserBalances
// Set the STATE_FILE environment variable to point to your actual state dump file
func TestPrintUserBalances(t *testing.T) {
	stateFile := os.Getenv("STATE_FILE")
	if stateFile == "" {
		t.Skip("Set STATE_FILE environment variable to test with real data")
	}

	// convert to lowercase
	targetAddress := strings.ToLower("0x1C37C8805afAAbea0aE347595A40BF40b5435123")

	reader := NewReader(10) // 10MB buffer

	assetStates, err := reader.ReadSpotAssetStates(stateFile)
	if err != nil {
		t.Fatalf("ReadSpotAssetStates() error = %v", err)
	}

	// Find balances for the target address across all assets
	type assetBalance struct {
		AssetID  int64
		Symbol   string
		Decimals int64
		Balance  int64
	}
	var userBalances []assetBalance

	for _, asset := range assetStates {
		for _, holder := range asset.Holders {
			if strings.EqualFold(holder.Address, targetAddress) {
				userBalances = append(userBalances, assetBalance{
					AssetID:  asset.AssetID,
					Symbol:   asset.Symbol,
					Decimals: asset.Decimals,
					Balance:  holder.Balance,
				})
				break
			}
		}
	}

	if len(userBalances) == 0 {
		t.Logf("No balances found for address: %s", targetAddress)
		return
	}

	// Print all balances for this user
	t.Logf("Balances for address: %s", targetAddress)
	t.Logf("Found %d asset balances:", len(userBalances))
	for _, balance := range userBalances {
		divisor := math.Pow10(int(balance.Decimals))
		balanceFloat := float64(balance.Balance) / divisor
		t.Logf("  AssetID=%d, Symbol=%s, Balance=%d (%.6f)",
			balance.AssetID, balance.Symbol, balance.Balance, balanceFloat)
	}
}

// TestAggregateAssetMetrics tests aggregating metrics for a specific asset ID
// To use: go test -v -run TestAggregateAssetMetrics
// Set the STATE_FILE environment variable to point to your actual state dump file
func TestAggregateAssetMetrics(t *testing.T) {
	stateFile := os.Getenv("STATE_FILE")
	if stateFile == "" {
		t.Skip("Set STATE_FILE environment variable to test with real data")
	}

	targetAssetID := int64(360) // Change this to the asset ID you want to analyze

	reader := NewReader(10) // 10MB buffer

	assetStates, err := reader.ReadSpotAssetStates(stateFile)
	if err != nil {
		t.Fatalf("ReadSpotAssetStates() error = %v", err)
	}

	// Find the target asset
	var targetAsset *SpotAssetState
	for i := range assetStates {
		if assetStates[i].AssetID == targetAssetID {
			targetAsset = &assetStates[i]
			break
		}
	}

	if targetAsset == nil {
		t.Logf("No data found for AssetID: %d", targetAssetID)
		return
	}

	if len(targetAsset.Holders) == 0 {
		t.Logf("No holders found for AssetID: %d", targetAssetID)
		return
	}

	// Sort holders by balance (descending)
	holders := make([]SpotAssetHolder, len(targetAsset.Holders))
	copy(holders, targetAsset.Holders)
	sort.Slice(holders, func(i, j int) bool {
		return holders[i].Balance > holders[j].Balance
	})

	// Calculate divisor for decimal formatting
	divisor := math.Pow10(int(targetAsset.Decimals))

	// Print aggregated metrics
	t.Logf("=== Asset Metrics for AssetID: %d ===", targetAssetID)
	t.Logf("Symbol: %s", targetAsset.Symbol)
	t.Logf("Decimals: %d", targetAsset.Decimals)
	totalBalanceFloat := float64(targetAsset.TotalSupply) / divisor
	t.Logf("Total Supply: %.6f", totalBalanceFloat)
	t.Logf("Total Holders: %d", len(holders))

	// Print top holders (top 10 by default)
	topN := 10
	if len(holders) < topN {
		topN = len(holders)
	}
	t.Logf("\nTop %d Holders:", topN)
	for i := 0; i < topN; i++ {
		balanceFloat := float64(holders[i].Balance) / divisor
		percentage := float64(holders[i].Balance) * 100.0 / float64(targetAsset.TotalSupply)
		t.Logf("  %d. Address: %s, Balance: %.6f (%.2f%%)",
			i+1, holders[i].Address, balanceFloat, percentage)
	}
}

