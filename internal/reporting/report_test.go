package reporting

import (
	"testing"
	"time"
)

func TestBuild(t *testing.T) {
	r := Build("s1", time.Time{}, time.Now(), []bool{true, true, false, true})
	if r.UptimePercent != 75 {
		t.Fatalf("got %v", r.UptimePercent)
	}
}
