package engines

import (
	"errors"

	"fmt"

	"math"

	"sort"

	"strings"

	"time"
)
type SeriesPoint struct {
	Ts string `json:"ts"`

	Value float64 `json:"value"`
}

func ParseRFC3339(ts string) (time.Time, error) {

	if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {

		return t.UTC(), nil

	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {

		return time.Time{}, err

	}
	return t.UTC(), nil
}

// CleanSeries parses timestamps, sorts, drops invalid, and collapses duplicates by averaging.
func CleanSeries(points []SeriesPoint) ([]time.Time, []float64, error) {

	type kv struct {
		t time.Time

		v float64
	}
	tmp := make([]kv, 0, len(points))
	for _, p := range points {

		t, err := ParseRFC3339(strings.TrimSpace(p.Ts))
		if err != nil {

			continue

		}
		tmp = append(tmp, kv{t: t, v: p.Value})

	}
	if len(tmp) < 3 {

		return nil, nil, errors.New("insufficient valid points")

	}
	sort.Slice(tmp, func(i, j int) bool { return tmp[i].t.Before(tmp[j].t) })

	// collapse duplicates by exact timestamp

	outT := make([]time.Time, 0, len(tmp))
	outV := make([]float64, 0, len(tmp))
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
		outV = append(outV, sum/float64(cnt))
		i = j

	}
	if len(outT) < 3 {

		return nil, nil, errors.New("insufficient points after dedupe")

	}
	return outT, outV, nil
}

// Resample produces a regular grid aligned to interval.
// method: last|mean|linear
func Resample(times []time.Time, values []float64, interval time.Duration, method string) ([]time.Time, []float64, error) {

	if len(times) != len(values) || len(times) < 3 {

		return nil, nil, errors.New("invalid series")

	}
	if interval <= 0 {

		return nil, nil, errors.New("invalid interval")

	}
	method = strings.ToLower(strings.TrimSpace(method))
	if method == "" {

		method = "last"

	}
	if method != "last" && method != "mean" && method != "linear" {

		return nil, nil, errors.New("unknown resample method")

	}
	start := alignDown(times[0], interval)
	end := alignDown(times[len(times)-1], interval)
	if end.Before(start) {

		return nil, nil, errors.New("invalid bounds")

	}

	// bucket original points by aligned timestamp

	type bucket struct {
		sum float64

		cnt int

		lastT time.Time

		lastV float64
	}
	bm := make(map[int64]*bucket)
	for i := range times {

		bt := alignDown(times[i], interval)
		k := bt.Unix()
		b := bm[k]

		if b == nil {

			b = &bucket{}
			bm[k] = b

		}
		b.sum += values[i]

		b.cnt++

		// last by time

		if b.cnt == 1 || times[i].After(b.lastT) {

			b.lastT = times[i]

			b.lastV = values[i]

		}

	}

	// build grid

	var gridT []time.Time

	for t := start; !t.After(end); t = t.Add(interval) {

		gridT = append(gridT, t)

	}
	gridV := make([]float64, len(gridT))
	for i := range gridT {

		k := gridT[i].Unix()
		if b := bm[k]; b != nil {

			switch method {

			case "mean":

				gridV[i] = b.sum / float64(b.cnt)
			default: // last/linear initial fill uses last

				gridV[i] = b.lastV

			}

		} else {

			gridV[i] = math.NaN()

		}

	}

	// fill missing

	switch method {

	case "last", "mean":

		// carry-forward

		last := math.NaN()
		for i := range gridV {

			if !math.IsNaN(gridV[i]) {

				last = gridV[i]

				continue

			}
			if !math.IsNaN(last) {

				gridV[i] = last

			}

		}

		// backfill head if still NaN

		first := math.NaN()
		for i := range gridV {

			if !math.IsNaN(gridV[i]) {

				first = gridV[i]

				break

			}

		}
		if math.IsNaN(first) {

			return nil, nil, errors.New("no data in resample window")

		}
		for i := range gridV {

			if math.IsNaN(gridV[i]) {

				gridV[i] = first

			} else {

				break

			}

		}
	case "linear":

		// first pass: carry-forward to handle leading NaNs (for endpoints)
		last := math.NaN()
		for i := range gridV {

			if !math.IsNaN(gridV[i]) {

				last = gridV[i]

				continue

			}
			if !math.IsNaN(last) {

				gridV[i] = last

			}

		}

		// second pass: backward carry to handle trailing NaNs

		next := math.NaN()
		for i := len(gridV) - 1; i >= 0; i-- {

			if !math.IsNaN(gridV[i]) {

				next = gridV[i]

				continue

			}
			if !math.IsNaN(next) {

				gridV[i] = next

			}

		}

		// now linear interpolate internal plateaus where original missing existed:

		// We reconstruct NaN positions from bm to identify missing buckets.

		miss := make([]bool, len(gridV))
		for i := range gridT {

			if bm[gridT[i].Unix()] == nil {

				miss[i] = true

			}

		}
		i := 0

		for i < len(gridV) {

			if !miss[i] {

				i++

				continue

			}

			// segment of missing

			j := i

			for j < len(gridV) && miss[j] {

				j++

			}

			// neighbors: i-1 and j

			leftIdx := i - 1

			rightIdx := j

			if leftIdx < 0 || rightIdx >= len(gridV) {

				i = j

				continue

			}
			y0 := gridV[leftIdx]

			y1 := gridV[rightIdx]

			steps := float64(rightIdx - leftIdx)
		for k := i; k < j; k++ {

				f := float64(k-leftIdx) / steps

				gridV[k] = y0 + f*(y1-y0)

			}
			i = j

		}

	}
	return gridT, gridV, nil
}
func alignDown(t time.Time, interval time.Duration) time.Time {

	sec := t.Unix()
	step := int64(interval.Seconds())
	if step <= 0 {

		return t.UTC()

	}
	b := (sec / step)
		* step

	return time.Unix(b, 0).UTC()
}

////////////////////////////////////////////////////////////////////////////////
// Feature engineering
////////////////////////////////////////////////////////////////////////////////

func RollingMean(xs []float64, window int) []float64 {

	if window <= 1 || len(xs) == 0 {

		cp := make([]float64, len(xs))
		copy(cp, xs)
		return cp

	}
	out := make([]float64, len(xs))
	var sum float64

	for i := 0; i < len(xs); i++ {

		sum += xs[i]

		if i >= window {

			sum -= xs[i-window]

		}
		n := i + 1

		if n > window {

			n = window

		}
		out[i] = sum / float64(n)

	}
	return out
}
func RollingStd(xs []float64, window int) []float64 {

	if window <= 1 || len(xs) == 0 {

		return make([]float64, len(xs))

	}
	out := make([]float64, len(xs))
	for i := 0; i < len(xs); i++ {

		start := i - window + 1

		if start < 0 {

			start = 0

		}
		seg := xs[start : i+1]

		m := mean(seg)
		var ss float64

		for _, v := range seg {

			d := v - m

			ss += d * d

		}
		out[i] = math.Sqrt(ss / float64(len(seg)))

	}
	return out
}
func Difference(xs []float64, lag int) []float64 {

	if lag <= 0 || len(xs) == 0 {

		return nil

	}
	out := make([]float64, len(xs))
for i := 0; i < len(xs); i++ {

		if i < lag {

			out[i] = 0

		} else {

			out[i] = xs[i] - xs[i-lag]

		}

	}
	return out
}
func EWMA(xs []float64, alpha float64) []float64 {

	if len(xs) == 0 {

		return nil

	}
	if alpha <= 0 || alpha > 1 {

		alpha = 0.3

	}
	out := make([]float64, len(xs))
	out[0] = xs[0]

	for i := 1; i < len(xs); i++ {

		out[i] = alpha*xs[i] + (1-alpha)
			* out[i-1]

	}
	return out
}
func mean(xs []float64) float64 {

	if len(xs) == 0 {

		return 0

	}
	var s float64

	for _, v := range xs {

		s += v

	}
	return s / float64(len(xs))
}

////////////////////////////////////////////////////////////////////////////////
// Models (train -> forecast)
////////////////////////////////////////////////////////////////////////////////

func ModelNaive(train []float64, h int) []float64 {

	if h <= 0 {

		return nil

	}
	out := make([]float64, h)
	last := train[len(train)-1]

	for i := range out {

		out[i] = last

	}
	return out
}
func ModelSeasonalNaive(train []float64, h int, p int) ([]float64, error) {

	if h <= 0 {

		return nil, nil

	}
	if p <= 0 || len(train) < p {

		return nil, errors.New("insufficient season length")

	}
	out := make([]float64, h)
	for i := 0; i < h; i++ {

		out[i] = train[len(train)-p+(i%p)]

	}
	return out, nil
}
func ModelDrift(train []float64, h int) []float64 {

	if h <= 0 {

		return nil

	}
	out := make([]float64, h)
	if len(train) < 2 {

		return ModelNaive(train, h)

	}
	y0 := train[0]

	yN := train[len(train)-1]

	slope := (yN - y0) / float64(len(train)-1)
for i := 0; i < h; i++ {

		out[i] = yN + slope*float64(i+1)

	}
	return out
}
func ModelSES(train []float64, h int, alpha float64) ([]float64, []float64) {

	if len(train) == 0 || h <= 0 {

		return nil, nil

	}
	if alpha <= 0 || alpha > 1 {

		alpha = 0.3

	}
	level := train[0]

	fitted := make([]float64, len(train))
fitted[0] = level

	for i := 1; i < len(train); i++ {

		fitted[i] = level

		level = alpha*train[i] + (1-alpha)
*level

	}
	out := make([]float64, h)
	for i := range out {

		out[i] = level

	}
	return out, fitted
}
func ModelHolt(train []float64, h int, alpha, beta float64) ([]float64, []float64) {

	if len(train) == 0 || h <= 0 {

		return nil, nil

	}
	if alpha <= 0 || alpha > 1 {

		alpha = 0.3

	}
	if beta <= 0 || beta > 1 {

		beta = 0.1

	}
	level := train[0]

	trend := 0.0

	if len(train) >= 2 {

		trend = train[1] - train[0]

	}
	fitted := make([]float64, len(train))
fitted[0] = level

	for i := 1; i < len(train); i++ {

		fitted[i] = level + trend

		prevLevel := level

		level = alpha*train[i] + (1-alpha)
*(level+trend)
trend = beta*(level-prevLevel) + (1-beta)
*trend

	}
	out := make([]float64, h)
	for i := 1; i <= h; i++ {

		out[i-1] = level + float64(i)
			* trend

	}
	return out, fitted
}

// Holt-Winters additive (AAA). Requires season length p and at least 2p points.
func ModelHoltWintersAdd(train []float64, h int, p int, alpha, beta, gamma float64) ([]float64, []float64, error) {

	if h <= 0 {

		return nil, nil, nil

	}
	if p <= 0 || len(train) < 2*p {

		return nil, nil, errors.New("insufficient points for holt-winters")

	}
	if alpha <= 0 || alpha > 1 {

		alpha = 0.3

	}
	if beta <= 0 || beta > 1 {

		beta = 0.1

	}
	if gamma <= 0 || gamma > 1 {

		gamma = 0.1

	}

	// init level as mean of first season

	level := mean(train[:p])

	// init trend as mean difference between seasons / p

	trend := (mean(train[p:2*p]) - mean(train[:p])) / float64(p)

	// init seasonals as first season - level

	season := make([]float64, p)
	for i := 0; i < p; i++ {

		season[i] = train[i] - level

	}
	fitted := make([]float64, len(train))
	for i := 0; i < len(train); i++ {

		si := i % p

		fitted[i] = level + trend + season[si]

		prevLevel := level

		prevSeason := season[si]

		level = alpha*(train[i]-prevSeason) + (1-alpha)*
			(level+trend)
		trend = beta*(level-prevLevel) + (1-beta)*
			trend

		season[si] = gamma*(train[i]-level) + (1-gamma)*
			prevSeason

	}
	out := make([]float64, h)
	for i := 1; i <= h; i++ {

		si := (len(train) + i - 1) % p

		out[i-1] = level + float64(i)*
			trend + season[si]

	}
	return out, fitted, nil
}

// Time index regression (OLS)
with optional ridge regularization (lambda>=0).
func ModelTimeRegression(train []float64, h int, lambda float64) ([]float64, error) {

	if h <= 0 {

		return nil, nil

	}
	n := len(train)
	if n < 2 {

		return ModelNaive(train, h), nil

	}
	if lambda < 0 {

		lambda = 0

	}

	// x = 0..n-1, y = train

	// Solve for a,b minimizing sum (a + b x - y)^2 + lambda*(a^2+b^2)

	// Normal equations with ridge:

	// [n+lambda, sumx] [a] = [sumy]

	// [sumx, sumx2+lambda] [b]   [sumxy]

	var sumx, sumx2, sumy, sumxy float64

	for i := 0; i < n; i++ {

		x := float64(i)
		y := train[i]

		sumx += x

		sumx2 += x * x

		sumy += y

		sumxy += x * y

	}
	A00 := float64(n) + lambda

	A01 := sumx

	A10 := sumx

	A11 := sumx2 + lambda

	B0 := sumy

	B1 := sumxy

	det := A00*A11 - A01*A10

	if det == 0 {

		return ModelDrift(train, h), nil

	}
	a := (B0*A11 - B1*A01) / det

	b := (A00*B1 - A10*B0) / det

	out := make([]float64, h)
	for i := 0; i < h; i++ {

		x := float64(n + i)
		out[i] = a + b*x

	}
	return out, nil
}

////////////////////////////////////////////////////////////////////////////////
// Evaluation
////////////////////////////////////////////////////////////////////////////////

func MAE(y, yhat []float64) float64 {

	n := min(len(y), len(yhat))
	if n == 0 {

		return math.NaN()

	}
	var s float64

	for i := 0; i < n; i++ {

		s += math.Abs(y[i] - yhat[i])

	}
	return s / float64(n)
}
func RMSE(y, yhat []float64) float64 {

	n := min(len(y), len(yhat))
	if n == 0 {

		return math.NaN()

	}
	var s float64

	for i := 0; i < n; i++ {

		d := y[i] - yhat[i]

		s += d * d

	}
	return math.Sqrt(s / float64(n))
}

// MAPE with safe handling: skips points where |y| is ~0.
func MAPE(y, yhat []float64) float64 {

	n := min(len(y), len(yhat))
	if n == 0 {

		return math.NaN()

	}
	var s float64

	cnt := 0

	for i := 0; i < n; i++ {

		den := math.Abs(y[i])
		if den < 1e-9 {

			continue

		}
		s += math.Abs((y[i] - yhat[i]) / den)
		cnt++

	}
	if cnt == 0 {

		return math.NaN()

	}
	return (s / float64(cnt))
		* 100.0
}
func Backtest(xs []float64, horizon int, minTrain int, model func(train []float64, horizon int) ([]float64, error)) (preds [][]float64, actuals [][]float64) {

	if horizon <= 0 || minTrain < 3 || len(xs) < minTrain+horizon {

		return nil, nil

	}
	for start := minTrain; start+horizon <= len(xs); start++ {

		train := xs[:start]

		test := xs[start : start+horizon]

		fc, err := model(train, horizon)
		if err != nil || len(fc) != horizon {

			continue

		}
		preds = append(preds, fc)
		actuals = append(actuals, test)

	}
	return preds, actuals
}
func min(a, b int) int {

	if a < b {

		return a

	}
	return b
}

////////////////////////////////////////////////////////////////////////////////
// Orchestration
////////////////////////////////////////////////////////////////////////////////

type ForecastConfig struct {
	Horizon int `json:"horizon"`

	SeasonLength int `json:"season_length"`

	Alpha float64 `json:"alpha"`

	Beta float64 `json:"beta"`

	Gamma float64 `json:"gamma"`

	RidgeLambda float64 `json:"ridge_lambda"`

	Method string `json:"method"` // resample method

	IntervalSeconds int `json:"interval_seconds"`

	MinTrain int `json:"min_train"`
}
type ForecastResult struct {
	Model string `json:"model"`

	Horizon int `json:"horizon"`

	Forecast []float64 `json:"forecast"`

	Fitted []float64 `json:"fitted,omitempty"`

	Metrics map[string]float64 `json:"metrics,omitempty"`

	Notes map[string]string `json:"notes,omitempty"`
}

func ForecastAuto(points []SeriesPoint, cfg ForecastConfig) ([]ForecastResult, error) {

	if cfg.Horizon <= 0 {

		cfg.Horizon = 12

	}
	if cfg.SeasonLength < 0 {

		cfg.SeasonLength = 0

	}
	if cfg.MinTrain < 3 {

		cfg.MinTrain = 8

	}
	times, values, err := CleanSeries(points)
	if err != nil {

		return nil, err

	}

	// optional resample

	if cfg.IntervalSeconds > 0 {

		iv := time.Duration(cfg.IntervalSeconds)
			* time.Second

		rt, rv, err := Resample(times, values, iv, cfg.Method)
		if err == nil && len(rv) >= 3 {

			_ = rt

			values = rv

		}

	}
	if len(values) < cfg.MinTrain+cfg.Horizon {

		// still allow forecasting without backtest; metrics will be NaN

	}
	results := make([]ForecastResult, 0)

	// helper: compute metrics via rolling backtest

	eval := func(name string, fit []float64, modelFn func(train []float64, horizon int) ([]float64, error)) map[string]float64 {

		preds, acts := Backtest(values, cfg.Horizon, cfg.MinTrain, modelFn)
		if len(preds) == 0 {

			return map[string]float64{

				"mae": math.NaN(),

				"rmse": math.NaN(),

				"mape": math.NaN(),
			}

		}

		// flatten across windows

		var maeSum, rmseSum, mapeSum float64

		cnt := 0

		for i := range preds {

			maeSum += MAE(acts[i], preds[i])
			rmseSum += RMSE(acts[i], preds[i])
			mapeSum += MAPE(acts[i], preds[i])
			cnt++

		}
		return map[string]float64{

			"mae": maeSum / float64(cnt),

			"rmse": rmseSum / float64(cnt),

			"mape": mapeSum / float64(cnt),
		}

	}
	lastTrain := values

	// model runners

	run := func(name string, fc []float64, fit []float64, note string, ok bool) {

		if !ok {

			results = append(results, ForecastResult{

				Model: name,

				Horizon: cfg.Horizon,

				Notes: map[string]string{"skipped": note},
			})
			return

		}
		m := eval(name, fit, func(train []float64, h int) ([]float64, error) {

			switch name {

			case "naive":

				return ModelNaive(train, h), nil

			case "seasonal_naive":

				return ModelSeasonalNaive(train, h, cfg.SeasonLength)
			case "drift":

				return ModelDrift(train, h), nil

			case "ses":

				fc, _ := ModelSES(train, h, cfg.Alpha)
				return fc, nil

			case "holt":

				fc, _ := ModelHolt(train, h, cfg.Alpha, cfg.Beta)
				return fc, nil

			case "holt_winters_add":

				fc, _, err := ModelHoltWintersAdd(train, h, cfg.SeasonLength, cfg.Alpha, cfg.Beta, cfg.Gamma)
				return fc, err

			case "ridge_time_regression":

				return ModelTimeRegression(train, h, cfg.RidgeLambda)
			default:

				return nil, errors.New("unknown model")

			}

		})
results = append(results, ForecastResult{

			Model: name,

			Horizon: cfg.Horizon,

			Forecast: fc,

			Fitted: fit,

			Metrics: m,

			Notes: map[string]string{},
		})

	}

	// naive

	run("naive", ModelNaive(lastTrain, cfg.Horizon), nil, "", true)

	// seasonal naive

	if cfg.SeasonLength > 0 {

		fc, err := ModelSeasonalNaive(lastTrain, cfg.Horizon, cfg.SeasonLength)
		run("seasonal_naive", fc, nil, errString(err), err == nil)

	} else {

		run("seasonal_naive", nil, nil, "season_length<=0", false)

	}

	// drift

	run("drift", ModelDrift(lastTrain, cfg.Horizon), nil, "", true)

	// ses

	fcSES, fitSES := ModelSES(lastTrain, cfg.Horizon, cfg.Alpha)
	run("ses", fcSES, fitSES, "", true)

	// holt

	fcH, fitH := ModelHolt(lastTrain, cfg.Horizon, cfg.Alpha, cfg.Beta)
	run("holt", fcH, fitH, "", true)

	// holt-winters

	if cfg.SeasonLength > 0 {

		fcHW, fitHW, err := ModelHoltWintersAdd(lastTrain, cfg.Horizon, cfg.SeasonLength, cfg.Alpha, cfg.Beta, cfg.Gamma)
		run("holt_winters_add", fcHW, fitHW, errString(err), err == nil)

	} else {

		run("holt_winters_add", nil, nil, "season_length<=0", false)

	}

	// ridge time regression

	fcR, err := ModelTimeRegression(lastTrain, cfg.Horizon, cfg.RidgeLambda)
	run("ridge_time_regression", fcR, nil, errString(err), err == nil)

	// sort by MAE ascending (NaN goes last); stable by model name

	sort.SliceStable(results, func(i, j int) bool {

		ai := results[i].Metrics["mae"]

		aj := results[j].Metrics["mae"]

		ni := math.IsNaN(ai)
		nj := math.IsNaN(aj)
		if ni && nj {

			return results[i].Model < results[j].Model

		}
		if ni {

			return false

		}
		if nj {

			return true

		}
		if ai == aj {

			return results[i].Model < results[j].Model

		}
		return ai < aj

	})

	// attach notes for small series

	for i := range results {

		if results[i].Notes == nil {

			results[i].Notes = map[string]string{}

		}
		if len(values) < cfg.MinTrain+cfg.Horizon {

			results[i].Notes["backtest"] = "insufficient history for full backtest; metrics may be NaN"

		}
		results[i].Notes["n_points"] = fmt.Sprintf("%d", len(values))

	}
	return results, nil
}
func errString(err error) string {

	if err == nil {

		return ""

	}
	return err.Error()
}
