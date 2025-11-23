package monitors

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestParseOracleTimestamp tests the timestamp parsing function
func TestParseOracleTimestamp(t *testing.T) {
	tests := []struct {
		name      string
		timestamp string
		wantErr   bool
	}{
		{
			name:      "RFC3339",
			timestamp: "2025-01-15T10:30:00Z",
			wantErr:   false,
		},
		{
			name:      "RFC3339Nano",
			timestamp: "2025-01-15T10:30:00.123456789Z",
			wantErr:   false,
		},
		{
			name:      "Custom format",
			timestamp: "2025-01-15T10:30:00.999999999",
			wantErr:   false,
		},
		{
			name:      "Invalid format",
			timestamp: "invalid-timestamp",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseOracleTimestamp(tt.timestamp)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseOracleTimestamp() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestParseOracleUpdateLine tests parsing of individual oracle update lines
func TestParseOracleUpdateLine(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		wantErr bool
	}{
		{
			name:    "Valid oracle update with mark prices",
			line:    `{"update_class":"Deployer","mark_px_inputs":[["flx:NVDA","180.0"]],"spot_px_inputs":[["flx:NVDA","180.0"]],"external_perp_px_inputs":[["flx:NVDA","178.88"]],"oracle_pxs":{"coin_to_mark_px":[["flx:NVDA",{"px":"180.0","last_update_time":"2025-11-23T01:46:08.618318721","daily_px":"180.0"}]],"coin_to_oracle_px":[["flx:NVDA",{"px":"180.0","last_update_time":"2025-11-23T01:46:08.618318721","daily_px":"180.0"}]],"coin_to_external_perp_px":[["flx:NVDA",{"px":"178.88","last_update_time":"2025-11-23T01:46:08.618318721","daily_px":"178.88"}]]}}`,
			wantErr: false,
		},
		{
			name:    "Minimal valid update",
			line:    `{"update_class":"Deployer","mark_px_inputs":[],"spot_px_inputs":[],"external_perp_px_inputs":[],"oracle_pxs":{"coin_to_mark_px":[],"coin_to_oracle_px":[],"coin_to_external_perp_px":[]}}`,
			wantErr: false,
		},
		{
			name:    "Invalid JSON",
			line:    `{invalid json}`,
			wantErr: true,
		},
		{
			name:    "Empty line",
			line:    ``,
			wantErr: true,
		},
	}

	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := parseOracleUpdateLine(ctx, tt.line)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseOracleUpdateLine() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestExtractDeployerFromCoin tests the deployer extraction function
func TestExtractDeployerFromCoin(t *testing.T) {
	tests := []struct {
		name     string
		coin     string
		expected string
	}{
		{
			name:     "flx deployer",
			coin:     "flx:NVDA",
			expected: "flx",
		},
		{
			name:     "vntl deployer",
			coin:     "vntl:SPACEX",
			expected: "vntl",
		},
		{
			name:     "xyz deployer",
			coin:     "xyz:AAPL",
			expected: "xyz",
		},
		{
			name:     "no deployer prefix",
			coin:     "TSLA",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractDeployerFromCoin(tt.coin)
			if result != tt.expected {
				t.Errorf("extractDeployerFromCoin() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// TestExtractMarketFromCoin tests the market extraction function
func TestExtractMarketFromCoin(t *testing.T) {
	tests := []struct {
		name     string
		coin     string
		expected string
	}{
		{
			name:     "flx market",
			coin:     "flx:NVDA",
			expected: "NVDA",
		},
		{
			name:     "vntl market",
			coin:     "vntl:SPACEX",
			expected: "SPACEX",
		},
		{
			name:     "xyz market",
			coin:     "xyz:AAPL",
			expected: "AAPL",
		},
		{
			name:     "no deployer prefix",
			coin:     "TSLA",
			expected: "TSLA",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractMarketFromCoin(tt.coin)
			if result != tt.expected {
				t.Errorf("extractMarketFromCoin() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// TestParseFloat tests the float parsing function
func TestParseFloat(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    float64
		wantErr bool
	}{
		{
			name:    "integer string",
			input:   "180",
			want:    180.0,
			wantErr: false,
		},
		{
			name:    "float string",
			input:   "180.52",
			want:    180.52,
			wantErr: false,
		},
		{
			name:    "large number",
			input:   "24305.0",
			want:    24305.0,
			wantErr: false,
		},
		{
			name:    "invalid string",
			input:   "invalid",
			want:    0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseFloat(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseFloat() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && result != tt.want {
				t.Errorf("parseFloat() = %v, want %v", result, tt.want)
			}
		})
	}
}

// TestFindLatestOracleFile tests finding the most recent oracle file
func TestFindLatestOracleFile(t *testing.T) {
	// Create temporary directory structure
	tmpDir := t.TempDir()

	// Create some test files with different timestamps
	files := []string{
		filepath.Join(tmpDir, "2025-01-15", "10", "updates.log"),
		filepath.Join(tmpDir, "2025-01-15", "11", "updates.log"),
		filepath.Join(tmpDir, "2025-01-15", "09", "updates.log"),
	}

	for _, file := range files {
		if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
			t.Fatalf("Failed to create directory: %v", err)
		}
		if err := os.WriteFile(file, []byte("test"), 0644); err != nil {
			t.Fatalf("Failed to create file: %v", err)
		}
		// Add small delay to ensure different mod times
		time.Sleep(10 * time.Millisecond)
	}

	latest, err := findLatestOracleFile(tmpDir)
	if err != nil {
		t.Fatalf("findLatestOracleFile() error = %v", err)
	}

	if latest == "" {
		t.Error("findLatestOracleFile() returned empty path")
	}

	// Should return the last file we created
	expectedLatest := files[len(files)-1]
	if latest != expectedLatest {
		t.Errorf("findLatestOracleFile() = %v, want %v", latest, expectedLatest)
	}
}

// TestParseRealOracleFile is a helper test to parse a real oracle file from your node
// To use: go test -v -run TestParseRealOracleFile
// Set the ORACLE_FILE environment variable to point to your actual file
func TestParseRealOracleFile(t *testing.T) {
	oracleFile := os.Getenv("ORACLE_FILE")
	if oracleFile == "" {
		t.Skip("Set ORACLE_FILE environment variable to test with real data")
	}

	data, err := os.ReadFile(oracleFile)
	if err != nil {
		t.Fatalf("Failed to read oracle file: %v", err)
	}

	ctx := context.Background()
	lines := 0
	errors := 0

	// Parse each line
	content := string(data)
	for i, line := range splitLines(content) {
		if line == "" {
			continue
		}
		lines++

		if err := parseOracleUpdateLine(ctx, line); err != nil {
			t.Logf("Line %d error: %v", i+1, err)
			t.Logf("Line content: %s", line)
			errors++
		}
	}

	t.Logf("Parsed %d lines, %d errors", lines, errors)

	if lines == 0 {
		t.Error("No lines parsed from file")
	}
}

// Helper function to split content by newlines
func splitLines(content string) []string {
	var lines []string
	var current string

	for _, ch := range content {
		if ch == '\n' {
			lines = append(lines, current)
			current = ""
		} else {
			current += string(ch)
		}
	}

	if current != "" {
		lines = append(lines, current)
	}

	return lines
}
