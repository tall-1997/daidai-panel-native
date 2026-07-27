package cron

import (
	"strings"
	"testing"
	"time"
)

func TestValidateExpressionsRejectsInvalidRuleWithIndex(t *testing.T) {
	err := ValidateExpressions("0 0 * * * *\ninvalid cron")
	if err == nil {
		t.Fatal("expected invalid expression list to return an error")
	}
	if got := err.Error(); !strings.HasPrefix(got, "第 2 条") {
		t.Fatalf("expected indexed validation error, got %q", got)
	}
}

func TestNextRunTimesForExpressionsReturnsEarliestMatches(t *testing.T) {
	times := NextRunTimesForExpressions("0 */30 * * * *\n0 0 */2 * * *", 3)
	if len(times) != 3 {
		t.Fatalf("expected three upcoming run times, got %d", len(times))
	}
	if !times[0].Before(times[1]) && !times[0].Equal(times[1]) {
		t.Fatalf("expected sorted run times, got %v then %v", times[0], times[1])
	}
}

func TestNextRunTimesFromSupportsQuartzQuestionMarkAndHourLists(t *testing.T) {
	base := time.Date(2026, 5, 18, 7, 1, 0, 0, time.Local)

	firstExprTimes := NextRunTimesFrom("0 58 9,17 * * ?", 4, base)
	if len(firstExprTimes) != 4 {
		t.Fatalf("expected 4 next run times for first expr, got %d", len(firstExprTimes))
	}
	firstWant := []time.Time{
		time.Date(2026, 5, 18, 9, 58, 0, 0, time.Local),
		time.Date(2026, 5, 18, 17, 58, 0, 0, time.Local),
		time.Date(2026, 5, 19, 9, 58, 0, 0, time.Local),
		time.Date(2026, 5, 19, 17, 58, 0, 0, time.Local),
	}
	for i, want := range firstWant {
		if !firstExprTimes[i].Equal(want) {
			t.Fatalf("unexpected first expr time at %d: got %v want %v", i, firstExprTimes[i], want)
		}
	}

	secondExprTimes := NextRunTimesFrom("0 0 7,20 * * ?", 4, base)
	if len(secondExprTimes) != 4 {
		t.Fatalf("expected 4 next run times for second expr, got %d", len(secondExprTimes))
	}
	secondWant := []time.Time{
		time.Date(2026, 5, 18, 20, 0, 0, 0, time.Local),
		time.Date(2026, 5, 19, 7, 0, 0, 0, time.Local),
		time.Date(2026, 5, 19, 20, 0, 0, 0, time.Local),
		time.Date(2026, 5, 20, 7, 0, 0, 0, time.Local),
	}
	for i, want := range secondWant {
		if !secondExprTimes[i].Equal(want) {
			t.Fatalf("unexpected second expr time at %d: got %v want %v", i, secondExprTimes[i], want)
		}
	}
}
