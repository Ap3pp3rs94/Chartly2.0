package engines

import (
	"errors"

	"fmt"

	"math"

	"sort"

	"strings"

	"time"
)
type Anomaly struct {
	Index int `json:"index"`

	Ts string `json:"ts"`

	Value float64 `json:"value"`

	Score float64 `json:"score"`

	Method string `json:"method"`

	Direction string `json:"direction"` // high|low|change

	Reason string `json:"reason"`

	Meta map[string]string `json:"meta,omitempty"`
}
type DetectionSummary struct {
	Method string `json:"method"`

	N int `json:"n"`

	Anomalies int `json:"anomalies"`

	Notes map[string]string `json:"notes,omitempty"`
}
type AnomalyConfig struct {
	Method string `json:"method"` // rolling_zscore|rolling_mad|rolling_iqr|seasonal_residual|cusum|ewma_control|page_hinkley|auto

	Threshold float64 `json:"threshold"`

	Window int `json:"window"`

	SeasonLength int `json:"season_length"`

	MinPoints int `json:"min_points"`

	Warmup int `json:"warmup"`

	MaxAnomalies int `json:"max_anomalies"`

	CUSUMK float64 `json:"cusum_k"`

	CUSUMH float64 `json:"cusum_h"`

	EWMAAlpha float64 `json:"ewma_alpha"`

	ControlL float64 `json:"control_l"`

	PageHinkleyDelta float64 `json:"ph_delta"`

	PageHinkleyLambda float64 `json:"ph_lambda"`
}

func (c *AnomalyConfig) withDefaults() AnomalyConfig {

	cfg := *c

	cfg.Method = strings.ToLower(strings.TrimSpace(cfg.Method))
	if cfg.Method == "" {

		cfg.Method = "auto"

	}
	if cfg.Threshold <= 0 {

		// sensible default across z-like methods

		cfg.Threshold = 3.0

	}
	if cfg.Window <= 0 {

		cfg.Window = 30

	}
	if cfg.Window < 3 {

		cfg.Window = 3

	}
	if cfg.MinPoints <= 0 {

		cfg.MinPoints = 12

	}
	if cfg.MinPoints < 3 {

		cfg.MinPoints = 3

	}
	if cfg.Warmup <= 0 {

		cfg.Warmup = maxInt(8, cfg.Window)

	}
	if cfg.Warmup < 3 {

		cfg.Warmup = 3

	}
	if cfg.MaxAnomalies <= 0 {

		cfg.MaxAnomalies = 500

	}
	if cfg.MaxAnomalies < 1 {

		cfg.MaxAnomalies = 1

	}
	if cfg.SeasonLength < 0 {

		cfg.SeasonLength = 0

	}

	// CUSUM parameters (if not set, auto-derived from warmup std)

	// EWMA alpha

	if cfg.EWMAAlpha <= 0 || cfg.EWMAAlpha > 1 {

		cfg.EWMAAlpha = 0.2

	}

	// Control limit multiplier

	if cfg.ControlL <= 0 {

		cfg.ControlL = 3.0

	}

	// Page-Hinkley defaults

	if cfg.PageHinkleyDelta <= 0 {

		cfg.PageHinkleyDelta = 0.005

	}
	if cfg.PageHinkleyLambda <= 0 {

		cfg.PageHinkleyLambda = 5.0

	}
	return cfg
}
func (c *AnomalyConfig) validate() error {

	m := strings.ToLower(strings.TrimSpace(c.Method))
	switch m {

	case "rolling_zscore", "rolling_mad", "rolling_iqr", "seasonal_residual", "cusum", "ewma_control", "page_hinkley", "auto":
	default:

		return errors.New("unknown anomaly method")

	}
	if c.Window < 3 {

		return errors.New("window must be >=3")

	}
	if c.MinPoints < 3 {

		return errors.New("min_points must be >=3")

	}
	if c.MaxAnomalies < 1 {

		return errors.New("max_anomalies must be >=1")

	}
	if m == "seasonal_residual" || m == "auto" {

		if c.SeasonLength < 0 {

			return errors.New("season_length invalid")

		}

	}
	return nil
}
func DetectSeries(points []SeriesPoint, cfg AnomalyConfig) ([]Anomaly, DetectionSummary, error) {

	ps := make([]Point, 0, len(points))
	for _, p := range points {

		ps = append(ps, Point{Ts: p.Ts, Value: p.Value})

	}
	return Detect(ps, cfg)
}
func Detect(points []Point, cfg AnomalyConfig) ([]Anomaly, DetectionSummary, error) {

	cfg = cfg.withDefaults()
	if err := cfg.validate(); err != nil {

		return nil, DetectionSummary{}, err

	}
	times, tsStrings, xs, err := clean(points, cfg.MinPoints)
	if err != nil {

		return nil, DetectionSummary{}, err

	}
	_ = times // kept for potential future extensions; currently we use canonical tsStrings + xs

	method := cfg.Method

	var out []Anomaly

	notes := map[string]string{}
	switch method {

	case "rolling_zscore":

		out = detectRollingZ(tsStrings, xs, cfg)
	case "rolling_mad":

		out = detectRollingMAD(tsStrings, xs, cfg)
	case "rolling_iqr":

		out = detectRollingIQR(tsStrings, xs, cfg)
	case "seasonal_residual":

		o, n := detectSeasonalResidual(tsStrings, xs, cfg)
		out = o

		for k, v := range n {

			notes[k] = v

		}
	case "cusum":

		o, n := detectCUSUM(tsStrings, xs, cfg)
		out = o

		for k, v := range n {

			notes[k] = v

		}
	case "ewma_control":

		o, n := detectEWMA(tsStrings, xs, cfg)
		out = o

		for k, v := range n {

			notes[k] = v

		}
	case "page_hinkley":

		out = detectPageHinkley(tsStrings, xs, cfg)
	case "auto":

		o, n := detectAuto(tsStrings, xs, cfg)
		out = o

		for k, v := range n {

			notes[k] = v

		}
	default:

		return nil, DetectionSummary{}, errors.New("unknown method")

	}
	out = capAnomalies(out, cfg.MaxAnomalies)
	sum := DetectionSummary{

		Method: method,

		N: len(xs),

		Anomalies: len(out),

		Notes: notes,
	}
	return out, sum, nil
}

////////////////////////////////////////////////////////////////////////////////
// Cleaning / canonicalization
////////////////////////////////////////////////////////////////////////////////

func clean(points []Point, minPoints int) ([]time.Time, []string, []float64, error) {

	type kv struct {
		t time.Time

		ts string

		v float64
	}
	tmp := make([]kv, 0, len(points))
	for _, p := range points {

		ts := strings.TrimSpace(p.Ts)
		if ts == "" {

			continue

		}
		t, err := parseRFC3339(ts)
		if err != nil {

			continue

		}
		tmp = append(tmp, kv{t: t, ts: t.Format(time.RFC3339Nano), v: p.Value})

	}
	if len(tmp) < maxInt(3, minPoints) {

		return nil, nil, nil, errors.New("insufficient valid points")

	}
	sort.Slice(tmp, func(i, j int) bool { return tmp[i].t.Before(tmp[j].t) })

	// collapse duplicates by exact timestamp: average values

	outT := make([]time.Time, 0, len(tmp))
	outTS := make([]string, 0, len(tmp))
	outX := make([]float64, 0, len(tmp))
	i := 0

	for i < len(tmp) {

		t := tmp[i].t

		sum := tmp[i].v

		cnt := 1

		j := i + 1

		for j < len(tmp) && tmp[j].t.Equal(t) {

			sum += tmp[j].v

			cnt++

			j++

		}
		outT = append(outT, t)
		outTS = append(outTS, t.Format(time.RFC3339Nano))
		outX = append(outX, sum/float64(cnt))
		i = j

	}
	if len(outX) < maxInt(3, minPoints) {

		return nil, nil, nil, errors.New("insufficient points after dedupe")

	}
	return outT, outTS, outX, nil
}
func parseRFC3339(ts string) (time.Time, error) {

	if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {

		return t.UTC(), nil

	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {

		return time.Time{}, err

	}
	return t.UTC(), nil
}

////////////////////////////////////////////////////////////////////////////////
// Rolling Z-score (mean/std) with O(n) running sums
////////////////////////////////////////////////////////////////////////////////

func detectRollingZ(ts []string, xs []float64, cfg AnomalyConfig) []Anomaly {

	w := cfg.Window

	th := cfg.Threshold

	out := make([]Anomaly, 0)

	// running sums over trailing window excluding current point

	var sum, sumsq float64

	start := 0

	for i := 0; i < len(xs); i++ {

		// ensure window is [start, i)
if i-start > w {

			// remove xs[start]

			sum -= xs[start]

			sumsq -= xs[start] * xs[start]

			start++

		}

		// compute stats on prior window

		n := i - start

		if n >= 3 {

			mean := sum / float64(n)
			variance := (sumsq/float64(n) - mean*mean)
			if variance < 0 {

				variance = 0

			}
			std := math.Sqrt(variance)
			if std > 1e-12 {

				z := (xs[i] - mean) / std

				az := math.Abs(z)
				if az >= th {

					out = append(out, Anomaly{

						Index: i,

						Ts: ts[i],

						Value: xs[i],

						Score: az,

						Method: "rolling_zscore",

						Direction: dirSign(z),

						Reason: fmt.Sprintf("abs(z)=%.4f >= %.4f", az, th),

						Meta: map[string]string{

							"window": fmt.Sprintf("%d", w),

							"mean": fmt.Sprintf("%.6f", mean),

							"stddev": fmt.Sprintf("%.6f", std),
						},
					})

				}

			}

		}

		// add current into running sums for future points

		sum += xs[i]

		sumsq += xs[i] * xs[i]

	}
	return out
}

////////////////////////////////////////////////////////////////////////////////
// Rolling MAD (robust z)
////////////////////////////////////////////////////////////////////////////////

func detectRollingMAD(ts []string, xs []float64, cfg AnomalyConfig) []Anomaly {

	w := cfg.Window

	th := cfg.Threshold

	out := make([]Anomaly, 0)
	for i := 0; i < len(xs); i++ {

		start := i - w

		if start < 0 {

			start = 0

		}
		seg := xs[start:i] // prior window only

		if len(seg) < 5 {

			continue

		}
		med := median(seg)
		mad := medianAbsDev(seg, med)
		if mad <= 1e-12 {

			continue

		}

		// 0.6745 makes MAD comparable to std under normality

		rz := 0.6745 * (xs[i] - med) / mad

		arz := math.Abs(rz)
		if arz >= th {

			out = append(out, Anomaly{

				Index: i,

				Ts: ts[i],

				Value: xs[i],

				Score: arz,

				Method: "rolling_mad",

				Direction: dirSign(rz),

				Reason: fmt.Sprintf("abs(rz)=%.4f >= %.4f", arz, th),

				Meta: map[string]string{

					"window": fmt.Sprintf("%d", w),

					"median": fmt.Sprintf("%.6f", med),

					"mad": fmt.Sprintf("%.6f", mad),
				},
			})

		}

	}
	return out
}

////////////////////////////////////////////////////////////////////////////////
// Rolling IQR (Tukey fences)
////////////////////////////////////////////////////////////////////////////////

func detectRollingIQR(ts []string, xs []float64, cfg AnomalyConfig) []Anomaly {

	w := cfg.Window

	out := make([]Anomaly, 0)
	for i := 0; i < len(xs); i++ {

		start := i - w

		if start < 0 {

			start = 0

		}
		seg := xs[start:i]

		if len(seg) < 8 {

			continue

		}
		q1, q3 := quartilesTukey(seg)
		iqr := q3 - q1

		low := q1 - 1.5*iqr

		high := q3 + 1.5*iqr

		x := xs[i]

		if x < low || x > high {

			score := 0.0

			dir := "change"

			if x < low {

				score = (low - x) / maxFloat(1e-12, iqr)
				dir = "low"

			} else {

				score = (x - high) / maxFloat(1e-12, iqr)
				dir = "high"

			}
			out = append(out, Anomaly{

				Index: i,

				Ts: ts[i],

				Value: x,

				Score: score,

				Method: "rolling_iqr",

				Direction: dir,

				Reason: fmt.Sprintf("value outside fences [%.6f, %.6f]", low, high),

				Meta: map[string]string{

					"window": fmt.Sprintf("%d", w),

					"q1": fmt.Sprintf("%.6f", q1),

					"q3": fmt.Sprintf("%.6f", q3),

					"iqr": fmt.Sprintf("%.6f", iqr),
				},
			})

		}

	}
	return out
}

////////////////////////////////////////////////////////////////////////////////
// Seasonal residual detection (phase baseline + residual z-score)
////////////////////////////////////////////////////////////////////////////////

func detectSeasonalResidual(ts []string, xs []float64, cfg AnomalyConfig) ([]Anomaly, map[string]string) {

	p := cfg.SeasonLength

	w := cfg.Window

	th := cfg.Threshold

	notes := map[string]string{}
	if p <= 1 {

		notes["skipped"] = "season_length<=1"

		return nil, notes

	}
	if len(xs) < 2*p {

		notes["skipped"] = "insufficient history for seasonality"

		return nil, notes

	}

	// baseline for each phase: mean of prior values with same phase

	out := make([]Anomaly, 0)
	resid := make([]float64, len(xs))
	phaseSum := make([]float64, p)
	phaseCnt := make([]int, p)
	for i := 0; i < len(xs); i++ {

		ph := i % p

		if phaseCnt[ph] > 0 {

			base := phaseSum[ph] / float64(phaseCnt[ph])
			r := xs[i] - base

			resid[i] = r

			// compute rolling std of residuals over last w residuals (excluding current)
			start := i - w

			if start < 0 {

				start = 0

			}
			seg := resid[start:i]

			if len(seg) >= 8 {

				m := mean(seg)
				s := stddev(seg, m)
				if s > 1e-12 {

					z := r / s

					az := math.Abs(z)
					if az >= th {

						out = append(out, Anomaly{

							Index: i,

							Ts: ts[i],

							Value: xs[i],

							Score: az,

							Method: "seasonal_residual",

							Direction: dirSign(z),

							Reason: fmt.Sprintf("abs(resid_z)=%.4f >= %.4f", az, th),

							Meta: map[string]string{

								"season_length": fmt.Sprintf("%d", p),

								"phase": fmt.Sprintf("%d", ph),

								"baseline": fmt.Sprintf("%.6f", base),

								"residual": fmt.Sprintf("%.6f", r),

								"resid_std": fmt.Sprintf("%.6f", s),
							},
						})

					}

				}

			}

		} else {

			resid[i] = 0

		}
		phaseSum[ph] += xs[i]

		phaseCnt[ph]++

	}
	return out, notes
}

////////////////////////////////////////////////////////////////////////////////
// CUSUM (two-sided)
////////////////////////////////////////////////////////////////////////////////

func detectCUSUM(ts []string, xs []float64, cfg AnomalyConfig) ([]Anomaly, map[string]string) {

	notes := map[string]string{}
	warm := cfg.Warmup

	if warm >= len(xs) {

		warm = len(xs) - 1

	}
	if warm < 3 {

		notes["skipped"] = "warmup<3"

		return nil, notes

	}
	base := xs[:warm]

	mu := mean(base)
	sd := stddev(base, mu)
	if sd < 1e-12 {

		notes["skipped"] = "warmup stddev ~ 0"

		return nil, notes

	}
	k := cfg.CUSUMK

	h := cfg.CUSUMH

	if k <= 0 {

		k = 0.5 * sd

	}
	if h <= 0 {

		h = 5.0 * sd

	}
	var pos, neg float64

	out := make([]Anomaly, 0)
	for i := warm; i < len(xs); i++ {

		x := xs[i]

		pos = math.Max(0, pos+(x-mu-k))
		neg = math.Min(0, neg+(x-mu+k))
		if pos > h {

			out = append(out, Anomaly{

				Index: i,

				Ts: ts[i],

				Value: x,

				Score: pos / h,

				Method: "cusum",

				Direction: "high",

				Reason: "positive CUSUM exceeded threshold",

				Meta: map[string]string{

					"mu": fmt.Sprintf("%.6f", mu),

					"sd": fmt.Sprintf("%.6f", sd),

					"k": fmt.Sprintf("%.6f", k),

					"h": fmt.Sprintf("%.6f", h),
				},
			})
			pos = 0

		}
		if -neg > h {

			out = append(out, Anomaly{

				Index: i,

				Ts: ts[i],

				Value: x,

				Score: (-neg) / h,

				Method: "cusum",

				Direction: "low",

				Reason: "negative CUSUM exceeded threshold",

				Meta: map[string]string{

					"mu": fmt.Sprintf("%.6f", mu),

					"sd": fmt.Sprintf("%.6f", sd),

					"k": fmt.Sprintf("%.6f", k),

					"h": fmt.Sprintf("%.6f", h),
				},
			})
			neg = 0

		}

	}
	notes["warmup_mean"] = fmt.Sprintf("%.6f", mu)
	notes["warmup_std"] = fmt.Sprintf("%.6f", sd)
	notes["k"] = fmt.Sprintf("%.6f", k)
	notes["h"] = fmt.Sprintf("%.6f", h)
	return out, notes
}

////////////////////////////////////////////////////////////////////////////////
// EWMA control chart
////////////////////////////////////////////////////////////////////////////////

func detectEWMA(ts []string, xs []float64, cfg AnomalyConfig) ([]Anomaly, map[string]string) {

	notes := map[string]string{}
	warm := cfg.Warmup

	if warm >= len(xs) {

		warm = len(xs) - 1

	}
	if warm < 3 {

		notes["skipped"] = "warmup<3"

		return nil, notes

	}
	base := xs[:warm]

	mu := mean(base)
	sd := stddev(base, mu)
	if sd < 1e-12 {

		notes["skipped"] = "warmup stddev ~ 0"

		return nil, notes

	}
	alpha := cfg.EWMAAlpha

	L := cfg.ControlL

	// steady-state sigma for EWMA

	sigmaZ := sd * math.Sqrt(alpha/(2.0-alpha))
	limit := L * sigmaZ

	z := base[len(base)-1] // initialize from last warmup point

	out := make([]Anomaly, 0)
	for i := warm; i < len(xs); i++ {

		z = alpha*xs[i] + (1-alpha)*z

		d := z - mu

		ad := math.Abs(d)
		if ad > limit {

			dir := "change"

			if d > 0 {

				dir = "high"

			} else {

				dir = "low"

			}
			out = append(out, Anomaly{

				Index: i,

				Ts: ts[i],

				Value: xs[i],

				Score: ad / limit,

				Method: "ewma_control",

				Direction: dir,

				Reason: "EWMA exceeded control limit",

				Meta: map[string]string{

					"mu": fmt.Sprintf("%.6f", mu),

					"sd": fmt.Sprintf("%.6f", sd),

					"alpha": fmt.Sprintf("%.6f", alpha),

					"L": fmt.Sprintf("%.6f", L),

					"limit": fmt.Sprintf("%.6f", limit),

					"ewma": fmt.Sprintf("%.6f", z),

					"ewma_diff": fmt.Sprintf("%.6f", d),
				},
			})

		}

	}
	notes["warmup_mean"] = fmt.Sprintf("%.6f", mu)
	notes["warmup_std"] = fmt.Sprintf("%.6f", sd)
	notes["alpha"] = fmt.Sprintf("%.6f", alpha)
	notes["L"] = fmt.Sprintf("%.6f", L)
	return out, notes
}

////////////////////////////////////////////////////////////////////////////////
// Page-Hinkley change detection
////////////////////////////////////////////////////////////////////////////////

func detectPageHinkley(ts []string, xs []float64, cfg AnomalyConfig) []Anomaly {

	delta := cfg.PageHinkleyDelta

	lambda := cfg.PageHinkleyLambda

	out := make([]Anomaly, 0)

	// incremental mean

	mean := xs[0]

	var cum float64

	minCum := 0.0

	for i := 1; i < len(xs); i++ {

		// update mean

		mean = mean + (xs[i]-mean)/float64(i+1)
		cum += (xs[i] - mean - delta)
		if cum < minCum {

			minCum = cum

		}
		ph := cum - minCum

		if ph > lambda {

			out = append(out, Anomaly{

				Index: i,

				Ts: ts[i],

				Value: xs[i],

				Score: ph / lambda,

				Method: "page_hinkley",

				Direction: "change",

				Reason: "Page-Hinkley change detected",

				Meta: map[string]string{

					"delta": fmt.Sprintf("%.6f", delta),

					"lambda": fmt.Sprintf("%.6f", lambda),

					"ph": fmt.Sprintf("%.6f", ph),

					"mean": fmt.Sprintf("%.6f", mean),
				},
			})

			// reset after detection to avoid cascading duplicates

			cum = 0

			minCum = 0

			mean = xs[i]

		}

	}
	return out
}

////////////////////////////////////////////////////////////////////////////////
// AUTO (merge methods)
////////////////////////////////////////////////////////////////////////////////

func detectAuto(ts []string, xs []float64, cfg AnomalyConfig) ([]Anomaly, map[string]string) {

	notes := map[string]string{}
	candidates := []string{"rolling_zscore", "rolling_mad", "rolling_iqr", "cusum", "ewma_control", "page_hinkley"}
	if cfg.SeasonLength > 1 {

		candidates = append(candidates, "seasonal_residual")

	}
	merged := make(map[int]*Anomaly)

	// run each and merge by index

	for _, m := range candidates {

		sub := cfg

		sub.Method = m

		var anoms []Anomaly

		switch m {

		case "rolling_zscore":

			anoms = detectRollingZ(ts, xs, sub)
		case "rolling_mad":

			anoms = detectRollingMAD(ts, xs, sub)
		case "rolling_iqr":

			anoms = detectRollingIQR(ts, xs, sub)
		case "seasonal_residual":

			a, n := detectSeasonalResidual(ts, xs, sub)
			anoms = a

			if s := n["skipped"]; s != "" {

				notes["seasonal_residual"] = "skipped: " + s

			}
		case "cusum":

			a, n := detectCUSUM(ts, xs, sub)
			anoms = a

			if s := n["skipped"]; s != "" {

				notes["cusum"] = "skipped: " + s

			}
		case "ewma_control":

			a, n := detectEWMA(ts, xs, sub)
			anoms = a

			if s := n["skipped"]; s != "" {

				notes["ewma_control"] = "skipped: " + s

			}
	case "page_hinkley":

		anoms = detectPageHinkley(ts, xs, sub)

		}
	for _, a := range anoms {

			cur := merged[a.Index]

			if cur == nil {

				cp := a // copy

				cp.Method = "auto"

				if cp.Meta == nil {

					cp.Meta = map[string]string{}

				}
				cp.Meta["methods"] = m

				cp.Reason = "flagged by: " + m

				merged[a.Index] = &cp

			} else {

				// merge: keep max score, concat methods deterministically later

				if a.Score > cur.Score {

					cur.Score = a.Score

					cur.Direction = a.Direction

					cur.Value = a.Value

					cur.Ts = a.Ts

				}
				cur.Meta["methods"] = mergeMethods(cur.Meta["methods"], m)
				cur.Reason = "flagged by: " + cur.Meta["methods"]

			}

		}

	}
	out := make([]Anomaly, 0, len(merged))
	for _, a := range merged {

		out = append(out, *a)

	}
	sort.SliceStable(out, func(i, j int) bool {

		if out[i].Index == out[j].Index {

			return out[i].Score > out[j].Score

		}
		return out[i].Index < out[j].Index

	})
	notes["candidates"] = strings.Join(candidates, ",")
	return out, notes
}
func mergeMethods(existing string, add string) string {

	parts := make([]string, 0)
	if strings.TrimSpace(existing) != "" {

		for _, p := range strings.Split(existing, ",") {

			p = strings.TrimSpace(p)
			if p != "" {

				parts = append(parts, p)

			}

		}

	}
	parts = append(parts, strings.TrimSpace(add))
	parts = dedupeAndSort(parts)
	return strings.Join(parts, ",")
}
func dedupeAndSort(in []string) []string {

	set := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {

		s = strings.TrimSpace(s)
		if s == "" {

			continue

		}
		if _, ok := set[s]; ok {

			continue

		}
		set[s] = struct{}{}
		out = append(out, s)

	}
	sort.Strings(out)
	return out
}

////////////////////////////////////////////////////////////////////////////////
// Helpers: stats / quantiles (deterministic)
////////////////////////////////////////////////////////////////////////////////

func stddev(xs []float64, mean float64) float64 {

	if len(xs) == 0 {

		return 0

	}
	var ss float64

	for _, v := range xs {

		d := v - mean

		ss += d * d

	}
	return math.Sqrt(ss / float64(len(xs)))
}
func median(xs []float64) float64 {

	if len(xs) == 0 {

		return 0

	}
	cp := make([]float64, len(xs))
	copy(cp, xs)
	sort.Float64s(cp)
	n := len(cp)
	if n%2 == 1 {

		return cp[n/2]

	}
	return (cp[n/2-1] + cp[n/2]) / 2
}
func medianAbsDev(xs []float64, med float64) float64 {

	if len(xs) == 0 {

		return 0

	}
	cp := make([]float64, len(xs))
	for i := range xs {

		cp[i] = math.Abs(xs[i] - med)

	}
	return median(cp)
}

// Tukey quartiles: median-of-halves
func quartilesTukey(xs []float64) (q1, q3 float64) {

	if len(xs) == 0 {

		return 0, 0

	}
	cp := make([]float64, len(xs))
	copy(cp, xs)
	sort.Float64s(cp)
	n := len(cp)
	if n < 2 {

		return cp[0], cp[0]

	}
	var lower, upper []float64

	if n%2 == 0 {

		lower = cp[:n/2]

		upper = cp[n/2:]

	} else {

		lower = cp[:n/2]

		upper = cp[n/2+1:]

	}
	return median(lower), median(upper)
}
func dirSign(z float64) string {

	if z > 0 {

		return "high"

	}
	if z < 0 {

		return "low"

	}
	return "change"
}

////////////////////////////////////////////////////////////////////////////////
// Caps / determinism
////////////////////////////////////////////////////////////////////////////////

func capAnomalies(in []Anomaly, maxN int) []Anomaly {

	if len(in) <= maxN {

		// stable sort by index asc, then method asc (deterministic)
		sort.SliceStable(in, func(i, j int) bool {

			if in[i].Index == in[j].Index {

				return in[i].Method < in[j].Method

			}
			return in[i].Index < in[j].Index

		})
		return in

	}

	// select top maxN by Score desc (stable deterministic tie-breakers)
	cp := make([]Anomaly, len(in))
	copy(cp, in)
	sort.SliceStable(cp, func(i, j int) bool {

		if cp[i].Score == cp[j].Score {

			if cp[i].Index == cp[j].Index {

				return cp[i].Method < cp[j].Method

			}
			return cp[i].Index < cp[j].Index

		}
		return cp[i].Score > cp[j].Score

	})
	cp = cp[:maxN]

	// final stable ordering by index asc

	sort.SliceStable(cp, func(i, j int) bool {

		if cp[i].Index == cp[j].Index {

			return cp[i].Method < cp[j].Method

		}
		return cp[i].Index < cp[j].Index

	})
	return cp
}
func maxInt(a, b int) int {

	if a > b {

		return a

	}
	return b
}
func maxFloat(a, b float64) float64 {

	if a > b {

		return a

	}
	return b
}
