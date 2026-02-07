package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Ap3pp3rs94/Chartly2.0/cmd/generator"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "generate":
		if len(os.Args) < 3 {
			usage()
			os.Exit(2)
		}
		switch os.Args[2] {
		case "crypto":
			generateCrypto(os.Args[3:])
		default:
			usage()
			os.Exit(2)
		}
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Println("chartly generate crypto --ids id1,id2 --timeframes live,1h,24h,7d --output ./profiles --register")
}

func generateCrypto(args []string) {
	fs := flag.NewFlagSet("crypto", flag.ExitOnError)
	idsArg := fs.String("ids", "bitcoin,ethereum,cardano,solana,binancecoin,ripple,dogecoin,polkadot,chainlink,litecoin", "Comma-separated CoinGecko IDs")
	tfArg := fs.String("timeframes", "live,1h,24h,7d", "Timeframes to generate")
	outDir := fs.String("output", "./profiles", "Output directory")
	register := fs.Bool("register", false, "Auto-POST profiles to localhost:8090/api/profiles")
	_ = fs.Parse(args)

	ids := splitCSV(*idsArg)
	tfs := make(map[string]bool)
	for _, t := range splitCSV(*tfArg) {
		tfs[strings.ToLower(t)] = true
	}

	if len(ids) == 0 {
		fmt.Fprintln(os.Stderr, "no ids provided")
		os.Exit(2)
	}

	profiles := generator.GenerateCryptoProfiles(ids)
	filtered := make([]generator.Profile, 0, len(profiles))
	for _, p := range profiles {
		if strings.HasSuffix(p.ID, "-live") && tfs["live"] {
			filtered = append(filtered, p)
			continue
		}
		if strings.HasSuffix(p.ID, "-1h") && tfs["1h"] {
			filtered = append(filtered, p)
			continue
		}
		if strings.HasSuffix(p.ID, "-24h") && tfs["24h"] {
			filtered = append(filtered, p)
			continue
		}
		if strings.HasSuffix(p.ID, "-7d") && tfs["7d"] {
			filtered = append(filtered, p)
			continue
		}
	}

	sort.Slice(filtered, func(i, j int) bool { return filtered[i].ID < filtered[j].ID })

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "mkdir failed:", err)
		os.Exit(1)
	}

	for _, p := range filtered {
		yaml := profileYAML(p)
		path := filepath.Join(*outDir, p.ID+".yaml")
		if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "write failed:", err)
			os.Exit(1)
		}
	}

	// Write top10 live profile
	top10 := generator.Top10LiveProfile(ids)
	topPath := filepath.Join(*outDir, "crypto-top10-live.yaml")
	if err := os.WriteFile(topPath, []byte(profileYAML(top10)), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "write failed:", err)
		os.Exit(1)
	}

	// Validate first ID with lightweight checks
	validateCoinGecko(ids[0])

	if *register {
		key := strings.TrimSpace(os.Getenv("CHARTLY_API_KEY"))
		if key == "" {
			fmt.Fprintln(os.Stderr, "CHARTLY_API_KEY missing; cannot register")
			os.Exit(1)
		}
		errs := 0
		for _, p := range filtered {
			if err := postProfile(p, key); err != nil {
				errs++
			}
		}
		if err := postProfile(top10, key); err != nil {
			errs++
		}
		if errs > 0 {
			fmt.Fprintln(os.Stderr, "register completed with errors:", errs)
		}
	}

	fmt.Printf("generated %d profiles + crypto-top10-live\n", len(filtered))
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

func profileYAML(p generator.Profile) string {
	var b strings.Builder
	b.WriteString("id: ")
	b.WriteString(p.ID)
	b.WriteString("\n")
	b.WriteString("name: ")
	b.WriteString(p.Name)
	b.WriteString("\n")
	b.WriteString("version: ")
	b.WriteString(p.Version)
	b.WriteString("\n")
	b.WriteString("description: ")
	b.WriteString(p.Description)
	b.WriteString("\n")
	b.WriteString("schedule:\n")
	b.WriteString("  enabled: ")
	b.WriteString(fmtBool(p.Schedule.Enabled))
	b.WriteString("\n")
	b.WriteString("  interval: ")
	b.WriteString(p.Schedule.Interval)
	b.WriteString("\n")
	b.WriteString("  jitter: ")
	b.WriteString(p.Schedule.Jitter)
	b.WriteString("\n")
	b.WriteString("limits:\n")
	b.WriteString(fmt.Sprintf("  max_records: %d\n", p.Limits.MaxRecords))
	b.WriteString(fmt.Sprintf("  max_pages: %d\n", p.Limits.MaxPages))
	b.WriteString(fmt.Sprintf("  max_bytes: %d\n", p.Limits.MaxBytes))
	b.WriteString("source:\n")
	b.WriteString("  type: ")
	b.WriteString(p.Source.Type)
	b.WriteString("\n")
	b.WriteString("  url: ")
	b.WriteString(p.Source.URL)
	b.WriteString("\n")
	b.WriteString("  auth: ")
	b.WriteString(p.Source.Auth)
	b.WriteString("\n")

	b.WriteString("mapping:\n")
	keys := make([]string, 0, len(p.Mapping))
	for k := range p.Mapping {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		b.WriteString("  ")
		b.WriteString(k)
		b.WriteString(": ")
		b.WriteString(p.Mapping[k])
		b.WriteString("\n")
	}
	return b.String()
}

func fmtBool(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func postProfile(p generator.Profile, apiKey string) error {
	body := map[string]any{
		"id":      p.ID,
		"name":    p.Name,
		"version": p.Version,
		"content": profileYAML(p),
	}
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, "http://localhost:8090/api/profiles", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", apiKey)
	c := &http.Client{Timeout: 10 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, "register failed:", p.ID, err)
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		fmt.Fprintln(os.Stderr, "register failed:", p.ID, string(buf))
		return fmt.Errorf("register_failed")
	}
	return nil
}

func validateCoinGecko(id string) {
	// Live endpoint check
	liveURL := "https://api.coingecko.com/api/v3/simple/price?ids=" + id + "&vs_currencies=usd&include_last_updated_at=true"
	if !hasKey(liveURL, id) {
		fmt.Fprintln(os.Stderr, "warning: live endpoint missing expected fields for", id)
	}
	chartURL := "https://api.coingecko.com/api/v3/coins/" + id + "/market_chart?vs_currency=usd&days=1"
	if !hasKey(chartURL, "prices") {
		fmt.Fprintln(os.Stderr, "warning: market_chart endpoint missing prices for", id)
	}
}

func hasKey(url, key string) bool {
	c := &http.Client{Timeout: 10 * time.Second}
	resp, err := c.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return false
	}
	var obj map[string]any
	if err := json.Unmarshal(b, &obj); err != nil {
		return false
	}
	_, ok := obj[key]
	return ok
}
