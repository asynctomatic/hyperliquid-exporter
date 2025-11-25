package abci

import (
	"bufio"
	"fmt"
	"os"
	"sync"

	"github.com/vmihailenco/msgpack/v5"
)

// provides reading of ABCI state files
type Reader struct {
	bufferSize int
	cache      *Cache
	mu         sync.RWMutex
}

// creates a new reader with specified buffer size
func NewReader(bufferSizeMB int) *Reader {
	return &Reader{
		bufferSize: bufferSizeMB * 1024 * 1024,
		cache:      NewCache(),
	}
}

// extracts context information from an ABCI state file
func (r *Reader) ReadContext(filePath string) (*ContextInfo, error) {
	// file info for cache validation
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("stat file: %w", err)
	}

	// cache first
	if cached := r.cache.Get(filePath, fileInfo.ModTime()); cached != nil {
		return cached, nil
	}

	// Open file
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	defer file.Close()

	// buffered reader for efficiency
	reader := bufio.NewReaderSize(file, r.bufferSize)

	// minimal structure for context only
	var data struct {
		Exchange struct {
			Context struct {
				Height     int64  `msgpack:"height"`
				TxIndex    int64  `msgpack:"tx_index"`
				Time       string `msgpack:"time"`
				NextOid    int64  `msgpack:"next_oid"`
				NextLid    int64  `msgpack:"next_lid"`
				NextTwapId int64  `msgpack:"next_twap_id"`
				Hardfork   struct {
					Version int64 `msgpack:"version"`
				} `msgpack:"hardfork"`
			} `msgpack:"context"`
		} `msgpack:"exchange"`
	}

	// decoder and decode
	decoder := msgpack.NewDecoder(reader)
	decoder.SetCustomStructTag("msgpack")

	if err := decoder.Decode(&data); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	// Create context info
	ctx := &ContextInfo{
		Height:          data.Exchange.Context.Height,
		TxIndex:         data.Exchange.Context.TxIndex,
		Time:            data.Exchange.Context.Time,
		NextOid:         data.Exchange.Context.NextOid,
		NextLid:         data.Exchange.Context.NextLid,
		NextTwapId:      data.Exchange.Context.NextTwapId,
		HardforkVersion: data.Exchange.Context.Hardfork.Version,
	}

	// Cache result
	r.cache.Set(filePath, ctx, fileInfo.ModTime())

	return ctx, nil
}

// read context and EVM account count
func (r *Reader) ReadContextWithAccounts(filePath string) (*ContextInfo, int64, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, 0, fmt.Errorf("open file: %w", err)
	}
	defer file.Close()

	reader := bufio.NewReaderSize(file, r.bufferSize)

	// structure including EVM accounts
	var data struct {
		Exchange struct {
			Context struct {
				Height     int64  `msgpack:"height"`
				TxIndex    int64  `msgpack:"tx_index"`
				Time       string `msgpack:"time"`
				NextOid    int64  `msgpack:"next_oid"`
				NextLid    int64  `msgpack:"next_lid"`
				NextTwapId int64  `msgpack:"next_twap_id"`
				Hardfork   struct {
					Version int64 `msgpack:"version"`
				} `msgpack:"hardfork"`
			} `msgpack:"context"`
			HyperEvm struct {
				State2 struct {
					EvmDb struct {
						InMemory struct {
							Accounts []interface{} `msgpack:"accounts"`
						} `msgpack:"InMemory"`
					} `msgpack:"evm_db"`
				} `msgpack:"state2"`
			} `msgpack:"hyper_evm"`
		} `msgpack:"exchange"`
	}

	decoder := msgpack.NewDecoder(reader)
	decoder.SetCustomStructTag("msgpack")

	if err := decoder.Decode(&data); err != nil {
		return nil, 0, fmt.Errorf("decode: %w", err)
	}

	ctx := &ContextInfo{
		Height:          data.Exchange.Context.Height,
		TxIndex:         data.Exchange.Context.TxIndex,
		Time:            data.Exchange.Context.Time,
		NextOid:         data.Exchange.Context.NextOid,
		NextLid:         data.Exchange.Context.NextLid,
		NextTwapId:      data.Exchange.Context.NextTwapId,
		HardforkVersion: data.Exchange.Context.Hardfork.Version,
	}

	accountCount := int64(len(data.Exchange.HyperEvm.State2.EvmDb.InMemory.Accounts))

	return ctx, accountCount, nil
}

func (r *Reader) ReadSpotAssetStates(filePath string) ([]SpotAssetState, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	defer file.Close()

	reader := bufio.NewReaderSize(file, r.bufferSize)

	// Structure for spot clearing house user states
	var data struct {
		Exchange struct {
			ClearingHouse struct {
				Meta struct {
					TokenInfos []interface{} `msgpack:"token_infos"`
				} `msgpack:"meta"`
				UserStates [][]interface{} `msgpack:"user_states"`
			} `msgpack:"spot_clearinghouse"`
		} `msgpack:"exchange"`
	}

	decoder := msgpack.NewDecoder(reader)
	decoder.SetCustomStructTag("msgpack")

	if err := decoder.Decode(&data); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	// Parse token infos to get asset metadata
	assetMetadata := make(map[int64]struct {
		Symbol   string
		Decimals int64
	})

	for assetID, tokenInfo := range data.Exchange.ClearingHouse.Meta.TokenInfos {
		if tokenInfoMap, ok := tokenInfo.(map[string]interface{}); ok {
			metadata := struct {
				Symbol   string
				Decimals int64
			}{}

			// Extract spec object
			if specMap, ok := tokenInfoMap["spec"].(map[string]interface{}); ok {
				// Extract symbol from spec.name
				if name, ok := specMap["name"].(string); ok {
					metadata.Symbol = name
				}

				// Extract decimals from spec.weiDecimals (note camelCase)
				if weiDecimals, ok := specMap["weiDecimals"].(int64); ok {
					metadata.Decimals = weiDecimals
				} else if weiDecimals, ok := specMap["weiDecimals"].(uint8); ok {
					metadata.Decimals = int64(weiDecimals)
				} else if weiDecimals, ok := specMap["weiDecimals"].(int8); ok {
					metadata.Decimals = int64(weiDecimals)
				}
			}

			assetMetadata[int64(assetID)] = metadata
		}
	}

	// Group balances by asset ID
	assetBalances := make(map[int64][]SpotAssetHolder)

	// Parse user states
	for _, entry := range data.Exchange.ClearingHouse.UserStates {
		if len(entry) != 2 {
			continue
		}

		// 1st element is the address
		address, ok := entry[0].(string)
		if !ok {
			continue
		}

		// 2nd element is the spot balances map
		balancesMap, ok := entry[1].(map[string]interface{})
		if !ok {
			continue
		}

		// Extract balances
		balancesRaw, ok := balancesMap["b"].([]interface{})
		if !ok {
			continue
		}

		// Parse balances
		for _, balanceRaw := range balancesRaw {
			balance, ok := balanceRaw.([]interface{})
			if !ok || len(balance) != 2 {
				continue
			}

			// 1st element is the asset ID
			var assetID int64
			switch val := balance[0].(type) {
			case uint8:
				assetID = int64(val)
			case int8:
				assetID = int64(val)
			case uint16:
				assetID = int64(val)
			case int16:
				assetID = int64(val)
			default:
				continue
			}

			// 2nd element is the balance map
			userBalanceMap, ok := balance[1].(map[string]interface{})
			if !ok {
				continue
			}

			// Extract total balance
			var balanceValue int64
			switch val := userBalanceMap["t"].(type) {
			case int64:
				balanceValue = val
			case int32:
				balanceValue = int64(val)
			case int16:
				balanceValue = int64(val)
			case int8:
				balanceValue = int64(val)
			case uint8:
				balanceValue = int64(val)
			case uint16:
				balanceValue = int64(val)
			case uint32:
				balanceValue = int64(val)
			case uint64:
				balanceValue = int64(val)
			case nil:
				continue
			default:
				continue
			}

			// Skip zero balances
			if balanceValue <= 0 {
				continue
			}

			// Add holder to asset
			assetBalances[assetID] = append(assetBalances[assetID], SpotAssetHolder{
				Address: address,
				Balance: balanceValue,
			})
		}
	}

	// Build SpotAssetState array
	var assetStates []SpotAssetState
	for assetID, holders := range assetBalances {
		// Calculate total supply
		var totalSupply int64
		for _, holder := range holders {
			totalSupply += holder.Balance
		}

		metadata := assetMetadata[assetID]
		assetStates = append(assetStates, SpotAssetState{
			AssetID:     assetID,
			Symbol:      metadata.Symbol,
			Decimals:    metadata.Decimals,
			TotalSupply: totalSupply,
			Holders:     holders,
		})
	}

	return assetStates, nil
}

// reads validator profiles from ABCI state
func (r *Reader) ReadValidatorProfiles(filePath string) ([]ValidatorProfile, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	defer file.Close()

	reader := bufio.NewReaderSize(file, r.bufferSize)

	// Strct for validator profiles
	var data struct {
		Exchange struct {
			Consensus struct {
				ValidatorToProfile [][]interface{} `msgpack:"validator_to_profile"`
			} `msgpack:"consensus"`
		} `msgpack:"exchange"`
	}

	decoder := msgpack.NewDecoder(reader)
	decoder.SetCustomStructTag("msgpack")

	if err := decoder.Decode(&data); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	var profiles []ValidatorProfile

	// Parse val profiles
	for _, entry := range data.Exchange.Consensus.ValidatorToProfile {
		if len(entry) != 2 {
			continue
		}

		// 1st element is val address
		address, ok := entry[0].(string)
		if !ok {
			continue
		}

		// 2nd is profile map
		profileData, ok := entry[1].(map[string]interface{})
		if !ok {
			continue
		}

		// extract node IP
		var ip string
		if nodeIPData, ok := profileData["node_ip"].(map[string]interface{}); ok {
			if ipValue, ok := nodeIPData["Ip"].(string); ok {
				ip = ipValue
			}
		}

		// extract name (moniker)
		var moniker string
		if nameValue, ok := profileData["name"].(string); ok {
			moniker = nameValue
		}

		if ip != "" && moniker != "" {
			profiles = append(profiles, ValidatorProfile{
				Address: address,
				Moniker: moniker,
				IP:      ip,
			})
		}
	}

	return profiles, nil
}

// reads perp user leverage data from ABCI state
func (r *Reader) ReadPerpLeverageData(filePath string) (map[int64][]int64, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	defer file.Close()

	reader := bufio.NewReaderSize(file, r.bufferSize)

	// Structure for clearinghouse user states
	var data struct {
		Exchange struct {
			PerpDexs []struct {
				Clearinghouse struct {
					UserStates struct {
						UserToState [][]interface{} `msgpack:"user_to_state"`
					} `msgpack:"user_states"`
				} `msgpack:"clearinghouse"`
			} `msgpack:"perp_dexs"`
		} `msgpack:"exchange"`
	}

	decoder := msgpack.NewDecoder(reader)
	decoder.SetCustomStructTag("msgpack")

	if err := decoder.Decode(&data); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	// Map of market ID -> list of leverage values
	marketLeverages := make(map[int64][]int64)

	// Iterate through perp dexes (index is dex ID)
	for dexID, perpDex := range data.Exchange.PerpDexs {
		fmt.Printf("dex ID: %d\n", dexID)

		// Parse user states for this dex
		for _, entry := range perpDex.Clearinghouse.UserStates.UserToState {
			if len(entry) != 2 {
				continue
			}

			// 2nd element is the state object
			stateMap, ok := entry[1].(map[string]interface{})
			if !ok || len(stateMap) == 0 {
				continue
			}

			// Navigate to positions: state.p.p
			pMap, ok := stateMap["p"].(map[string]interface{})
			if !ok {
				continue
			}

			positions, ok := pMap["p"].([]interface{})
			if !ok {
				continue
			}

			// Parse positions
			for _, posRaw := range positions {
				position, ok := posRaw.([]interface{})
				if !ok || len(position) != 2 {
					continue
				}

				// 1st element is market ID
				var marketID int64
				switch val := position[0].(type) {
				case uint16:
					marketID = int64(val)
				case int16:
					marketID = int64(val)
				case uint32:
					marketID = int64(val)
				case int32:
					marketID = int64(val)
				case int64:
					marketID = val
				default:
					continue
				}

				// 2nd element is position data
				posData, ok := position[1].(map[string]interface{})
				if !ok {
					continue
				}

				// Navigate to leverage: posData.l.I.l
				lMap, ok := posData["l"].(map[string]interface{})
				if !ok {
					continue
				}

				iMap, ok := lMap["I"].(map[string]interface{})
				if !ok {
					continue
				}

				// Extract leverage value
				var leverage int64
				switch val := iMap["l"].(type) {
				case uint8:
					leverage = int64(val)
				case int8:
					leverage = int64(val)
				case uint16:
					leverage = int64(val)
				case int16:
					leverage = int64(val)
				case uint32:
					leverage = int64(val)
				case int32:
					leverage = int64(val)
				case int64:
					leverage = val
				default:
					continue
				}

				// Skip if leverage is 0 or negative
				if leverage <= 0 {
					continue
				}

				marketLeverages[marketID] = append(marketLeverages[marketID], leverage)
			}
		}
	}

	return marketLeverages, nil
}
