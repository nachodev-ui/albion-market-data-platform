package upstream

import "time"

type IngestPricesRequest struct {
	RequestID string        `json:"request_id"`
	Server    string        `json:"server"`
	Entries   []PriceIngest `json:"entries"`
}

type PriceIngest struct {
	ObservedAt     time.Time  `json:"observed_at"`
	LocationID     int16      `json:"location_id"`
	ItemKey        string     `json:"item_key"`
	Quality        int16      `json:"quality"`
	SellPriceMin   *int64     `json:"sell_price_min,omitempty"`
	SellPriceMinAt *time.Time `json:"sell_price_min_at,omitempty"`
	BuyPriceMax    *int64     `json:"buy_price_max,omitempty"`
	BuyPriceMaxAt  *time.Time `json:"buy_price_max_at,omitempty"`
}

type IngestPricesResponse struct {
	RequestID          string `json:"request_id"`
	Accepted           int    `json:"accepted"`
	CurrentRowsTouched int64  `json:"current_rows_touched"`
	Duplicate          bool   `json:"duplicate"`
}

type SendResult struct {
	StatusCode int
	Duration   time.Duration
	Response   IngestPricesResponse
}

// IngestHistoryRequest mirrors the authenticated receiver-to-central-API
// contract. Numeric location IDs remain internal to this trusted hop and are
// never exposed by the public frontend endpoints.
type IngestHistoryRequest struct {
	RequestID string          `json:"request_id"`
	Server    string          `json:"server"`
	Entries   []HistoryIngest `json:"entries"`
}

type HistoryIngest struct {
	ObservedAt time.Time             `json:"observed_at"`
	LocationID int16                 `json:"location_id"`
	ItemKey    string                `json:"item_key"`
	Quality    int16                 `json:"quality"`
	History    []HistoryBucketIngest `json:"history"`
}

type HistoryBucketIngest struct {
	Timestamp        time.Time `json:"timestamp"`
	ItemCount        int64     `json:"item_count"`
	AverageUnitPrice *int64    `json:"average_unit_price"`
}

type IngestHistoryResponse struct {
	RequestID          string `json:"request_id"`
	AcceptedEntries    int    `json:"accepted_entries"`
	AcceptedBuckets    int    `json:"accepted_buckets"`
	HistoryRowsTouched int64  `json:"history_rows_touched"`
	Duplicate          bool   `json:"duplicate"`
}

type HistorySendResult struct {
	StatusCode int
	Duration   time.Duration
	Response   IngestHistoryResponse
}
