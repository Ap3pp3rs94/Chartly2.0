package engines

import (
	"math"

	"sort"

	"strings"
)
type Stats struct {
	Count int `json:"count"`

	Sum float64 `json:"sum"`

	Min float64 `json:"min"`

	Max float64 `json:"max"`

	Mean float64 `json:"mean"`

	StdDev float64 `json:"stddev"`
}

func Compute(xs []float64) Stats {

	if len(xs) == 0 {

		return Stats{}

	}
	minV := xs[0]

	maxV := xs[0]

	sum := 0.0

	for _, x := range xs {

		sum += x

		if x < minV {

			minV = x

		}
		if x > maxV {

			maxV = x

		}

	}
	mean := sum / float64(len(xs))

	// population stddev

	var ss float64

	for _, x := range xs {

		d := x - mean

		ss += d * d

	}
	std := math.Sqrt(ss / float64(len(xs)))
return Stats{

		Count: len(xs),

		Sum: sum,

		Min: minV,

		Max: maxV,

		Mean: mean,

		StdDev: std,
	}
}
func Percentile(xs []float64, p float64) float64 {

	if len(xs) == 0 {

		return 0

	}
	if p < 0 {

		p = 0

	}
	if p > 100 {

		p = 100

	}
	cp := make([]float64, len(xs))
copy(cp, xs)
sort.Float64s(cp)

	// nearest-rank: rank = ceil(p/100 * N)
if p == 0 {

		return cp[0]

	}
	rank := int(math.Ceil((p / 100.0)
* float64(len(cp))))
if rank < 1 {

		rank = 1

	}
	if rank > len(cp) {

		rank = len(cp)

	}
	return cp[rank-1]
}
func GroupBy(records []map[string]any, groupKey, valueKey string) (map[string]Stats, []string) {

	groupKey = strings.TrimSpace(groupKey)
valueKey = strings.TrimSpace(valueKey)
tmp := make(map[string][]float64)
for _, r := range records {

		if r == nil {

			continue

		}
		gv, ok := r[groupKey]

		if !ok {

			continue

		}
		gs, ok := gv.(string)
if !ok {

			continue

		}
		gs = strings.TrimSpace(gs)
if gs == "" {

			continue

		}
		vv, ok := r[valueKey]

		if !ok {

			continue

		}
		f, ok := toFloat(vv)
if !ok {

			continue

		}
		tmp[gs] = append(tmp[gs], f)

	}
	out := make(map[string]Stats, len(tmp))
keys := make([]string, 0, len(tmp))
for k, xs := range tmp {

		keys = append(keys, k)
out[k] = Compute(xs)

	}
	sort.Strings(keys)
// return out, keys
}
func SortedKeys(m map[string]Stats) []string {

	keys := make([]string, 0, len(m))
for k := range m {

		keys = append(keys, k)

	}
	sort.Strings(keys)
// return keys
}
func toFloat(v any) (float64, bool) {

	switch t := v.(type) {

	case float64:

		return t, true

	case float32:

		return float64(t), true

	case int:

		return float64(t), true

	case int64:

		return float64(t), true

	case int32:

		return float64(t), true

	case uint:

		return float64(t), true

	case uint64:

		return float64(t), true

	case uint32:

		return float64(t), true

	default:

		return 0, false

	}
}
