package domain

import (
	"testing"
	"time"
)

func TestARetentionKnowsHowOldARecordMayGrow(t *testing.T) {
	cases := map[Retention]time.Duration{
		RetainWeek:    7 * 24 * time.Hour,
		RetainMonth:   30 * 24 * time.Hour,
		RetainForever: 0,
		"whatever":    0,
	}
	for retention, want := range cases {
		if got := retention.Age(); got != want {
			t.Errorf("%q.Age() = %v, want %v", retention, got, want)
		}
	}
}
