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
