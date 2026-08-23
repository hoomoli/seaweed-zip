package retention

import (
	"testing"
	"time"
)

func TestShanghaiScheduleAndCalendar(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	now := time.Date(2026, 10, 13, 4, 9, 0, 0, loc)
	next := Next(now)
	if next.Hour() != 4 || next.Minute() != 10 || next.Day() != 13 {
		t.Fatal(next)
	}
	if Next(next).Day() != 14 {
		t.Fatal("repeated schedule")
	}
	if CalendarExpiry(time.Date(2024, 1, 31, 0, 0, 0, 0, loc)).Format("2006-01-02") != "2024-04-30" {
		t.Fatal("calendar month not clamped")
	}
}

func TestDownloadHardDeadlineCannotBeRenewedForever(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	expiry := time.Date(2026, 10, 13, 3, 0, 0, 0, loc)
	hard := DownloadDeadline(expiry, 2*time.Hour)
	if hard.Format("2006-01-02 15:04") != "2026-10-13 06:10" {
		t.Fatal(hard)
	}
}
