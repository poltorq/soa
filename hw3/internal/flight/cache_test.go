package flight

import (
	"testing"
	"time"
)

func TestSearchKey(t *testing.T) {
	date := time.Date(2026, 4, 1, 15, 30, 0, 0, time.FixedZone("test", 3*60*60))
	if got := searchKey(SearchFilter{Origin: "SVO", Destination: "LED", Date: &date}); got != "search:SVO:LED:2026-04-01" {
		t.Fatalf("unexpected dated key: %s", got)
	}
	if got := searchKey(SearchFilter{Origin: "SVO", Destination: "LED"}); got != "search:SVO:LED:any" {
		t.Fatalf("unexpected undated key: %s", got)
	}
}
