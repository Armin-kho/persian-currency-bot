
package sources

import "time"

type Provider string
type Method string

const (
	ProviderBonbast Provider = "bonbast"
	ProviderNavasan Provider = "navasan"

	MethodAPI    Method = "api"
	MethodScrape Method = "scrape"
)

type Quote struct {
	Sell *float64
	Buy  *float64
	Unit string // "toman" or "usd"
}

type Snapshot struct {
	Provider Provider
	Method   Method
	FetchedAt time.Time
	Quotes   map[string]Quote // itemID -> Quote
}
