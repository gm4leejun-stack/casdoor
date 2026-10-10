package object

import (
	"strings"
	"testing"
	"time"
)

func TestGenerateRedemptionCodeUsesSafeEntropyAndDigest(t *testing.T) {
	code, err := GenerateRedemptionCode(strings.NewReader(strings.Repeat("x", 64)))
	if err != nil {
		t.Fatal(err)
	}
	if len(code) < 20 {
		t.Fatalf("code is too short: %q", code)
	}
	if strings.ContainsAny(code, "O0Il1") {
		t.Fatalf("code contains ambiguous character: %q", code)
	}
	if got := HashRedemptionCode("test-pepper", code); len(got) != 64 {
		t.Fatalf("expected sha256 hex digest, got %q", got)
	}
}

func TestExtendSubscriptionPreservesSuspensionAndUsesMaxEnd(t *testing.T) {
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	oldEnd := now.Add(24 * time.Hour)
	sub := &Subscription{Owner: "gundam", User: "alice", Plan: "gundam-pro", StartTime: now.Add(-time.Hour).Format(time.RFC3339), EndTime: oldEnd.Format(time.RFC3339), State: SubStateSuspended}
	if err := ExtendSubscription(sub, now, 7*24*time.Hour); err != nil {
		t.Fatal(err)
	}
	if sub.State != SubStateSuspended {
		t.Fatalf("suspended subscription was resumed: %s", sub.State)
	}
	if got, want := sub.EndTime, now.Add(8*24*time.Hour).Format(time.RFC3339); got != want {
		t.Fatalf("end time = %s, want %s", got, want)
	}
}

func TestEvaluateMembershipUsesHalfOpenInterval(t *testing.T) {
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	sub := &Subscription{Owner: "gundam", User: "alice", Plan: "gundam-pro", StartTime: now.Add(-time.Hour).Format(time.RFC3339), EndTime: now.Add(time.Hour).Format(time.RFC3339), State: SubStateActive}
	if got := EvaluateMembership(sub, now).Status; got != EntitlementActive {
		t.Fatalf("status = %s, want Active", got)
	}
	if got := EvaluateMembership(sub, now.Add(time.Hour)).Status; got != EntitlementExpired {
		t.Fatalf("status at end = %s, want Expired", got)
	}
}

func TestNormalizeRedemptionCode(t *testing.T) {
	if got := NormalizeRedemptionCode(" ab-cd ef "); got != "ABCDEF" {
		t.Fatalf("normalized code = %q", got)
	}
}
