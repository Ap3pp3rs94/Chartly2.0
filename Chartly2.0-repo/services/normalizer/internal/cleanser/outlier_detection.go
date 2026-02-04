package cleanser

import (
	"math"
)

type Stats struct {
	Count int `json:"count"`

	Mean float64 `json:"mean"`

	StdDev float64 `json:"stddev"`

	Min float64 `json:"min"`

	Max float64 `json:"max"`

	Median float64 `json:"median"`

	Q1 float64 `json:"q1"`

	Q3 float64 `json:"q3"`
}

func ComputeStats(xs []float64) Stats {

	n := len(xs)
	if n == 0 {

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
	mean := sum / float64(n)

	// population stddev

	var ss float64

	for _, x := range xs {

		d := x - mean

		ss += d * d

	}
	std := math.Sqrt(ss / float64(n))

	// quantiles (Tukey)
	cp := make([]float64, n)
	copy(cp, xs)
	sortFloat64s(cp)
	med := median(cp)
	var lower, upper []float64

	if n%2 == 0 {

		lower = cp[:n/2]

		upper = cp[n/2:]

	} else {

		lower = cp[:n/2]

		upper = cp[n/2+1:]

	}
	q1 := median(lower)
	q3 := median(upper)
	return Stats{

		Count: n,

		Mean: mean,

		StdDev: std,

		Min: minV,

		Max: maxV,

		Median: med,

		Q1: q1,

		Q3: q3,
	}
}
func OutliersZ(xs []float64, threshold float64) []int {

	if threshold <= 0 {

		threshold = 3.0

	}
	st := ComputeStats(xs)
	if st.Count == 0 || st.StdDev == 0 {

		return nil

	}
	out := make([]int, 0)
	for i, x := range xs {

		z := (x - st.Mean) / st.StdDev

		if math.Abs(z) >= threshold {

			out = append(out, i)

		}

	}
	return out
}
func OutliersIQR(xs []float64) []int {

	st := ComputeStats(xs)
	if st.Count == 0 {

		return nil

	}
	iqr := st.Q3 - st.Q1

	low := st.Q1 - 1.5*iqr

	high := st.Q3 + 1.5*iqr

	out := make([]int, 0)
	for i, x := range xs {

		if x < low || x > high {

			out = append(out, i)

		}

	}
	return out
}
func median(sorted []float64) float64 {

	n := len(sorted)
	if n == 0 {

		return 0

	}
	if n%2 == 1 {

		return sorted[n/2]

	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}
func sortFloat64s(a []float64) {

	// insertion sort (deterministic, no imports)
	for i := 1; i < len(a); i++ {

		j := i

		for j > 0 && a[j] < a[j-1] {

			a[j], a[j-1] = a[j-1], a[j]

			j--

		}

	}
}
