package hyperliquidapi

// asset metadata from universe array
type AssetInfo struct {
	Name         string `json:"name"`
	SzDecimals   int    `json:"szDecimals"`
	MaxLeverage  int    `json:"maxLeverage"`
	OnlyIsolated bool   `json:"onlyIsolated"`
}

// market data from contexts array
type AssetContext struct {
	Funding      string   `json:"funding"`
	OpenInterest string   `json:"openInterest"`
	PrevDayPx    string   `json:"prevDayPx"`
	DayNtlVlm    string   `json:"dayNtlVlm"`
	Premium      string   `json:"premium"`
	OraclePx     string   `json:"oraclePx"`
	MarkPx       string   `json:"markPx"`
	MidPx        string   `json:"midPx,omitempty"`
	ImpactPxs    []string `json:"impactPxs,omitempty"`
}

// combined market data response
type MarketData struct {
	Universe []AssetInfo
	Contexts []AssetContext
}

// orderbook level
type OrderLevel struct {
	Px string `json:"px"` // price
	Sz string `json:"sz"` // size
	N  int    `json:"n"`  // number of orders
}

// L2 orderbook response
type L2Book struct {
	Coin   string          `json:"coin"`
	Levels [2][]OrderLevel `json:"levels"` // [0] = bids, [1] = asks
	Time   int64           `json:"time"`
}
