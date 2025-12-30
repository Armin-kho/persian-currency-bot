
package utils

import (
	"time"

	_ "time/tzdata"

	"github.com/go-universal/jalaali"
)

// TehranLoc returns the Tehran time zone location.
// Using the jalaali helper keeps behavior consistent even on minimal systems.
func TehranLoc() *time.Location {
	return jalaali.TehranTz()
}

func NowTehran() time.Time {
	return time.Now().In(TehranLoc())
}

// JalaliDateTime returns a string like "1404/10/09 - 16:40" (in Tehran time).
func JalaliDateTime(t time.Time) string {
	j := jalaali.New(t.In(TehranLoc()))
	return j.Format("2006/01/02 - 15:04")
}

// TimeHHMM formats time-of-day in HH:MM (24h).
func TimeHHMM(t time.Time) string {
	return t.In(TehranLoc()).Format("15:04")
}

// ParseHHMM parses "HH:MM" and returns minutes since midnight.
func ParseHHMM(hhmm string) (int, bool) {
	if len(hhmm) != 5 || hhmm[2] != ':' {
		return 0, false
	}
	hh := int(hhmm[0]-'0')*10 + int(hhmm[1]-'0')
	mm := int(hhmm[3]-'0')*10 + int(hhmm[4]-'0')
	if hh < 0 || hh > 23 || mm < 0 || mm > 59 {
		return 0, false
	}
	return hh*60 + mm, true
}

func FormatHHMM(minutes int) string {
	if minutes < 0 {
		minutes = (minutes%1440 + 1440) % 1440
	}
	h := minutes / 60
	m := minutes % 60
	return time.Date(2000, 1, 1, h, m, 0, 0, TehranLoc()).Format("15:04")
}

// InDowntime checks if a given minute-of-day is within the downtime interval.
// If start == end, it means "no downtime" (or full day depending on enabled flag).
// Supports ranges that cross midnight, e.g. 20:00 -> 10:00.
func InDowntime(minuteOfDay int, start int, end int) bool {
	if start == end {
		return false
	}
	if start < end {
		return minuteOfDay >= start && minuteOfDay < end
	}
	// Crosses midnight
	return minuteOfDay >= start || minuteOfDay < end
}
