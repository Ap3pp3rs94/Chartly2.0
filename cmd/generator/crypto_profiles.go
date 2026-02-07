package generator

import (
	"sort"
	"strings"
)

type Profile struct {
	ID          string
	Name        string
	Version     string
	Description string
	Schedule    Schedule
	Limits      Limits
	Source      Source
	Mapping     map[string]string
}

type Schedule struct {
	Enabled  bool
	Interval string
	Jitter   string
}

type Limits struct {
	MaxRecords int
	MaxPages   int
	MaxBytes   int
}

type Source struct {
	Type string
	URL  string
	Auth string
}

// GenerateCryptoProfiles creates 4 profiles per crypto (live/1h/24h/7d).
func GenerateCryptoProfiles(ids []string) []Profile {
	clean := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		clean = append(clean, id)
	}
	sort.Strings(clean)

	out := make([]Profile, 0, len(clean)*4)
	for _, id := range clean {
		out = append(out,
			makeCryptoProfile(id, "live"),
			makeCryptoProfile(id, "1h"),
			makeCryptoProfile(id, "24h"),
			makeCryptoProfile(id, "7d"),
		)
	}
	return out
}

func makeCryptoProfile(id, timeframe string) Profile {
	baseName := strings.Title(strings.ReplaceAll(id, "-", " "))
	name := baseName + " " + strings.ToUpper(timeframe)
	desc := "CoinGecko " + baseName + " " + timeframe + " timeframe"

	url := ""
	sched := Schedule{Enabled: true, Interval: "60s", Jitter: "5s"}
	switch timeframe {
	case "live":
		url = "https://api.coingecko.com/api/v3/simple/price?ids=" + id + "&vs_currencies=usd&include_market_cap=true&include_24hr_vol=true&include_24hr_change=true&include_last_updated_at=true"
		sched = Schedule{Enabled: true, Interval: "60s", Jitter: "5s"}
	case "1h":
		url = "https://api.coingecko.com/api/v3/coins/" + id + "/market_chart?vs_currency=usd&days=0.042"
		sched = Schedule{Enabled: true, Interval: "5m", Jitter: "10s"}
	case "24h":
		url = "https://api.coingecko.com/api/v3/coins/" + id + "/market_chart?vs_currency=usd&days=1"
		sched = Schedule{Enabled: true, Interval: "15m", Jitter: "20s"}
	case "7d":
		url = "https://api.coingecko.com/api/v3/coins/" + id + "/market_chart?vs_currency=usd&days=7"
		sched = Schedule{Enabled: true, Interval: "30m", Jitter: "30s"}
	}

	mapping := map[string]string{}
	if timeframe == "live" {
		mapping["crypto_id"] = "dims.crypto_id"
		mapping["last_updated_at"] = "dims.time.timestamp"
		mapping["usd"] = "measures.price_usd"
		mapping["usd_market_cap"] = "measures.market_cap_usd"
		mapping["usd_24h_vol"] = "measures.volume_24h"
		mapping["usd_24h_change"] = "measures.change_24h_pct"
	} else {
		mapping["crypto_id"] = "dims.crypto_id"
		mapping["timestamp"] = "dims.time.timestamp"
		mapping["price"] = "measures.price_usd"
		mapping["market_cap"] = "measures.market_cap_usd"
		mapping["volume"] = "measures.volume_24h"
	}

	return Profile{
		ID:          "crypto-" + id + "-" + timeframe,
		Name:        name,
		Version:     "1.0.0",
		Description: desc,
		Schedule:    sched,
		Limits: Limits{
			MaxRecords: 5000,
			MaxPages:   50,
			MaxBytes:   1048576,
		},
		Source: Source{
			Type: "http_rest",
			URL:  url,
			Auth: "none",
		},
		Mapping: mapping,
	}
}

// Top10LiveProfile creates a single profile that pulls top 10 in one call.
func Top10LiveProfile(ids []string) Profile {
	sort.Strings(ids)
	joined := strings.Join(ids, ",")
	url := "https://api.coingecko.com/api/v3/simple/price?ids=" + joined + "&vs_currencies=usd&include_market_cap=true&include_24hr_vol=true&include_24hr_change=true&include_last_updated_at=true"

	mapping := map[string]string{
		"crypto_id":       "dims.crypto_id",
		"last_updated_at": "dims.time.timestamp",
		"usd":             "measures.price_usd",
		"usd_market_cap":  "measures.market_cap_usd",
		"usd_24h_vol":     "measures.volume_24h",
		"usd_24h_change":  "measures.change_24h_pct",
	}

	return Profile{
		ID:          "crypto-top10-live",
		Name:        "Crypto Top 10 Live",
		Version:     "1.0.0",
		Description: "Top 10 crypto live prices (CoinGecko)",
		Schedule:    Schedule{Enabled: true, Interval: "60s", Jitter: "5s"},
		Limits: Limits{
			MaxRecords: 5000,
			MaxPages:   50,
			MaxBytes:   1048576,
		},
		Source: Source{Type: "http_rest", URL: url, Auth: "none"},
		Mapping: mapping,
	}
}
