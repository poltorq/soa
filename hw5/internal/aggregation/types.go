package aggregation

import "time"

type Metric struct {
	Date           time.Time
	Name           string
	DimensionKey   string
	DimensionValue string
	Value          *float64
	ComputedAt     time.Time
}
