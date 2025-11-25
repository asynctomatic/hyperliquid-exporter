package config

import (
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/validaoxyz/hyperliquid-exporter/internal/logger"
)

type Config struct {
	HomeDir                string
	NodeHome               string
	BinaryHome             string
	NodeBinary             string
	Chain                  string
	EnableEVM              bool
	EnableHIP3Oracle       bool
	EnableContractMetrics  bool
	ContractMetricsLimit   int
	EVMBlockTypeMetrics    bool
	EnableCoreTxMetrics    bool
	UseLiveState           bool
	LiveStateCheckInterval time.Duration // How often to check for updates
	EnableReplicaMetrics   bool
	ReplicaDataDir         string
	ReplicaBufferSize      int
	EnableValidatorRTT     bool
	SpotAssetSymbols       []string // List of spot asset symbols to monitor
	PerpMarketSymbols      []string // List of perpetual market symbols to monitor
	NodeInfoEndpoint       string   // API endpoint for market data
	MetricsAddr            string
	LogLevel               string
	LogFormat              string
}

type Flags struct {
	NodeHome              string
	NodeBinary            string
	Chain                 string
	EnableEVM             bool
	EnableHIP3Oracle      bool
	EnableContractMetrics bool
	ContractMetricsLimit  int
	EVMBlockTypeMetrics   bool
	EnableCoreTxMetrics   bool
	UseLiveState          bool
	EnableReplicaMetrics  bool
	ReplicaDataDir        string
	ReplicaBufferSize     int
	EnableValidatorRTT    *bool // to distinguish between not set and false
	SpotAssetSymbols      string // Comma-separated list of spot asset symbols to monitor
	PerpMarketSymbols     string // Comma-separated list of perpetual market symbols to monitor
	MetricsAddr           string
	LogLevel              string
	LogFormat             string
}

// load env vars and returns a Config struct
func LoadConfig(flags *Flags) Config {
	// load .env first
	if err := godotenv.Load(); err != nil {
		logger.Debug("No .env file found, using environment variables and flags")
	}

	homeDir := os.Getenv("HOME")

	nodeHome := os.Getenv("NODE_HOME")
	if nodeHome == "" {
		nodeHome = homeDir + "/hl" //default fallback
	}

	binaryHome := os.Getenv("BINARY_HOME")
	if binaryHome == "" {
		binaryHome = homeDir
	}

	nodeBinary := os.Getenv("NODE_BINARY")
	if nodeBinary == "" {
		nodeBinary = binaryHome + "/hl-node"
	}

	// always use default replica data dir
	replicaDataDir := nodeHome + "/data/replica_cmds"

	// always default buffer size
	replicaBufferSize := 8 // 8MB default

	// parse spot asset symbols from comma-separated string
	var spotAssetSymbols []string
	if flags.SpotAssetSymbols != "" {
		symbols := strings.Split(flags.SpotAssetSymbols, ",")
		for _, symbol := range symbols {
			trimmed := strings.TrimSpace(symbol)
			if trimmed != "" {
				spotAssetSymbols = append(spotAssetSymbols, trimmed)
			}
		}
	}

	// parse perpetual market symbols from comma-separated string
	var perpMarketSymbols []string
	if flags.PerpMarketSymbols != "" {
		symbols := strings.Split(flags.PerpMarketSymbols, ",")
		for _, symbol := range symbols {
			trimmed := strings.TrimSpace(symbol)
			if trimmed != "" {
				perpMarketSymbols = append(perpMarketSymbols, trimmed)
			}
		}
	}

	// load node info endpoint from env var with default fallback
	nodeInfoEndpoint := os.Getenv("NODE_INFO_ENDPOINT")
	if nodeInfoEndpoint == "" {
		nodeInfoEndpoint = "https://api.hyperliquid.xyz/info"
	}

	config := Config{
		NodeHome:               nodeHome,
		NodeBinary:             nodeBinary,
		Chain:                  flags.Chain,
		EnableEVM:              flags.EnableEVM,
		EnableHIP3Oracle:       flags.EnableHIP3Oracle,
		EnableContractMetrics:  flags.EnableContractMetrics,
		ContractMetricsLimit:   flags.ContractMetricsLimit,
		EVMBlockTypeMetrics:    flags.EVMBlockTypeMetrics,
		EnableCoreTxMetrics:    flags.EnableCoreTxMetrics,
		UseLiveState:           flags.UseLiveState,
		LiveStateCheckInterval: 5 * time.Second,
		EnableReplicaMetrics:   flags.EnableReplicaMetrics,
		ReplicaDataDir:         replicaDataDir,
		ReplicaBufferSize:      replicaBufferSize,
		EnableValidatorRTT:     false,
		SpotAssetSymbols:       spotAssetSymbols,
		PerpMarketSymbols:      perpMarketSymbols,
		NodeInfoEndpoint:       nodeInfoEndpoint,
		MetricsAddr:            flags.MetricsAddr,
		LogLevel:               flags.LogLevel,
		LogFormat:              flags.LogFormat,
	}

	// override with flags if they're provided
	if flags != nil {
		if flags.NodeHome != "" {
			config.NodeHome = flags.NodeHome
		}
		if flags.NodeBinary != "" {
			config.NodeBinary = flags.NodeBinary
		}
		if flags.Chain != "" {
			config.Chain = flags.Chain
		}
		if flags.EnableContractMetrics != config.EnableContractMetrics {
			config.EnableContractMetrics = flags.EnableContractMetrics
		}
		if flags.ContractMetricsLimit != config.ContractMetricsLimit {
			config.ContractMetricsLimit = flags.ContractMetricsLimit
		}
		config.EVMBlockTypeMetrics = config.EnableEVM
		if flags.EnableCoreTxMetrics != config.EnableCoreTxMetrics {
			config.EnableCoreTxMetrics = flags.EnableCoreTxMetrics
		}
		if flags.UseLiveState != config.UseLiveState {
			config.UseLiveState = flags.UseLiveState
		}
		if flags.EnableReplicaMetrics != config.EnableReplicaMetrics {
			config.EnableReplicaMetrics = flags.EnableReplicaMetrics
		}
		if flags.EnableValidatorRTT != nil {
			config.EnableValidatorRTT = *flags.EnableValidatorRTT
		}
	}

	return config
}
