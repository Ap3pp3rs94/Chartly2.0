package engines

import (

"errors"

"sort"

"time"
)

type Point struct {

Ts    string  `json:"ts"`

Value float64 `json:"value"`
}

func ParseTime(ts string) (time.Time, error) {

// RFC3339Nano parses RFC3339 too, but be explicit for clarity

if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {


return t.UTC(), nil

}

return time.Parse(time.RFC3339, ts)
}

func Bucket(points []Point, interval time.Duration) map[int64][]Point {

if interval <= 0 {


interval = time.Minute

}

out := make(map[int64][]Point)

for _, p := range points {


t, err := ParseTime(p.Ts)


if err != nil {



continue


}


sec := t.Unix()


b := (sec / int64(interval.Seconds())) * int64(interval.Seconds())


out[b] = append(out[b], p)

}

return out
}

func Aggregate(points []Point, interval time.Duration) ([]Point, error) {

if interval <= 0 {


interval = time.Minute

}


buckets := Bucket(points, interval)

if len(buckets) == 0 {


return nil, errors.New("no valid points")

}


keys := make([]int64, 0, len(buckets))

for k := range buckets {


keys = append(keys, k)

}

sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })


out := make([]Point, 0, len(keys))

for _, k := range keys {


ps := buckets[k]


sum := 0.0


for _, p := range ps {



sum += p.Value


}


mean := sum / float64(len(ps))


out = append(out, Point{



Ts:    time.Unix(k, 0).UTC().Format(time.RFC3339Nano),



Value: mean,


})

}

return out, nil
}
