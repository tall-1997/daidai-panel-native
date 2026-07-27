package cron

import (
	"strings"
	"testing"
	"time"
)

func TestParseMatchesSchedulerParser(t *testing.T) {
	cases := []string{
		"0 */5 * * * *",
		"0 9 * JAN MON",
		"0 0 L * *",
		"0 0 15W * *",
		"0 0 * * MON#2",
	}

	for _, expr := range cases {
		parser, _, err := parserForParts(strings.Fields(expr))
		_, parseErr := parser.Parse(expr)
		expectValid := err == nil && parseErr == nil

		result := Parse(expr)
		if result.Valid != expectValid {
			t.Fatalf("unexpected validity for %q: got %v want %v", expr, result.Valid, expectValid)
		}
	}
}

func TestNextRunTimesMatchesSchedule(t *testing.T) {
	expr := "0 */5 * * * *"
	schedule, err := parseSchedule(expr)
	if err != nil {
		t.Fatalf("parseSchedule error: %v", err)
	}

	got := NextRunTimes(expr, 3)
	if len(got) != 3 {
		t.Fatalf("expected 3 next run times, got %d", len(got))
	}

	cursor := time.Now()
	for i, next := range got {
		expected := schedule.Next(cursor)
		if !next.Equal(expected) {
			t.Fatalf("unexpected next run at index %d: got %v want %v", i, next, expected)
		}
		cursor = expected
	}
}

func TestParseInvalidFieldCount(t *testing.T) {
	result := Parse("* * * *")
	if result.Valid {
		t.Fatalf("expected invalid field count to be rejected")
	}
}
