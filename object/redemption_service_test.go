package object

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSmyIdentityIgnoresMobileChanges(t *testing.T) {
	now := time.Date(2026, 10, 15, 2, 0, 0, 0, time.UTC)
	input := SmyMembershipInput{OrderNo: "A", UniqueID: "stable", Mobile: "13800000001", ProductType: "1", ProductCode: "pro", PeriodTime: "3", OrderStatus: SmyOrderActive, Now: now}
	first, err := ApplySmyMembershipRequest(MembershipSnapshot{SubscriptionName: "test"}, input)
	if err != nil {
		t.Fatal(err)
	}
	input.Mobile = "13900000001"
	duplicate, err := ApplySmyMembershipRequest(first.After, input)
	if err != nil || !duplicate.Duplicate {
		t.Fatalf("mobile change must not invalidate duplicate: %v", err)
	}
	input.OrderNo = "B"
	second, err := ApplySmyMembershipRequest(first.After, input)
	if err != nil {
		t.Fatal(err)
	}
	input.OrderNo, input.OrderStatus, input.Mobile = "A", SmyOrderRefunded, "13700000001"
	input.Now = now.AddDate(0, 0, 10)
	refund, err := ApplySmyMembershipRequest(second.After, input)
	if err != nil {
		t.Fatal(err)
	}
	if refund.After.Orders[0].Mobile != "13800000001" || refund.After.Orders[1].Mobile != "13900000001" {
		t.Fatal("original order phone history was rewritten")
	}
	input.UniqueID = "different"
	if _, err := ApplySmyMembershipRequest(refund.After, input); err == nil {
		t.Fatal("order identity changed")
	}
}

func TestSmyCalendarRefundLifecycle(t *testing.T) {
	at := func(value string) time.Time {
		t.Helper()
		result, err := time.Parse(time.RFC3339, value)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	now := at("2026-10-15T02:00:00Z")
	base := MembershipSnapshot{SubscriptionName: "test", StartsAt: smyFormat(now.AddDate(0, -1, 0)), ExpiresAt: smyFormat(now), BaseStartsAt: smyFormat(now.AddDate(0, -1, 0)), BaseExpiresAt: smyFormat(now)}
	input := func(order, status string, now time.Time) SmyMembershipInput {
		return SmyMembershipInput{OrderNo: order, OrderStatus: status, ProductType: "YHK", ProductCode: "YHK01", PeriodTime: "1", Mobile: "13800000001", UniqueID: "u", Now: now}
	}
	apply := func(s MembershipSnapshot, req SmyMembershipInput) MembershipSnapshot {
		t.Helper()
		change, err := ApplySmyMembershipRequest(s, req)
		if err != nil {
			t.Fatal(err)
		}
		return change.After
	}
	for _, order := range []string{"A", "B", "C"} {
		base = apply(base, input(order, SmyOrderActive, now))
	}
	if base.ExpiresAt != "2027-01-15T02:00:00Z" {
		t.Fatal(base.ExpiresAt)
	}
	future := apply(base, input("B", SmyOrderRefunded, now))
	if future.ExpiresAt != "2026-12-15T02:00:00Z" {
		t.Fatal(future.ExpiresAt)
	}
	future = apply(future, input("C", SmyOrderRefunded, now))
	if future.ExpiresAt != "2026-11-15T02:00:00Z" {
		t.Fatal(future.ExpiresAt)
	}
	active := apply(base, input("B", SmyOrderRefunded, at("2026-11-25T02:00:00Z")))
	if active.ExpiresAt != "2026-12-25T02:00:00Z" || active.Orders[2].StartsAt != "2026-11-25T02:00:00Z" {
		t.Fatal(active)
	}
	ended := apply(base, input("A", SmyOrderRefunded, at("2026-12-01T02:00:00Z")))
	if ended.ExpiresAt != base.ExpiresAt {
		t.Fatal("ended refund chased other rights")
	}
	duplicate, err := ApplySmyMembershipRequest(active, input("B", SmyOrderActive, now))
	if err != nil || !duplicate.Duplicate || !reflect.DeepEqual(duplicate.After, active) {
		t.Fatal("late grant revived order", err)
	}
	empty := MembershipSnapshot{SubscriptionName: "new"}
	cancelled := apply(empty, input("X", SmyOrderRefunded, now))
	if cancelled.StartsAt != "" || cancelled.Orders[0].Granted {
		t.Fatal(cancelled)
	}
	granted := apply(empty, input("X", SmyOrderActive, now))
	cancelled = apply(granted, input("X", SmyOrderRefunded, now))
	if cancelled.StartsAt != cancelled.ExpiresAt {
		t.Fatal("immediate refund left rights")
	}
	for _, pair := range [][2]string{{"2026-01-31T02:00:00Z", "2026-02-28T02:00:00Z"}, {"2028-01-31T02:00:00Z", "2028-02-29T02:00:00Z"}, {"2026-01-30T18:00:00Z", "2026-02-27T18:00:00Z"}} {
		if got := smyFormat(AddSmyCalendarMonth(at(pair[0]))); got != pair[1] {
			t.Fatalf("month: %s want %s", got, pair[1])
		}
	}
	expired := apply(base, input("D", SmyOrderActive, at("2027-02-01T02:00:00Z")))
	if expired.StartsAt != "2027-02-01T02:00:00Z" || expired.ExpiresAt != "2027-03-01T02:00:00Z" {
		t.Fatal(expired)
	}
	expired = apply(expired, input("D", SmyOrderRefunded, at("2027-02-01T02:00:00Z")))
	if expired.StartsAt != expired.ExpiresAt {
		t.Fatal("expired gap cancellation wrong")
	}
	broken := cloneSmySnapshot(base)
	broken.Orders[1].StartsAt = "2026-11-16T02:00:00Z"
	broken.Orders[1].ExpiresAt = "2026-12-16T02:00:00Z"
	if err := ValidateSmyMembershipSnapshot(broken); err == nil {
		t.Fatal("broken order chain accepted")
	}
	badRequest := input("Z", SmyOrderActive, now)
	badRequest.PeriodTime = "2"
	if _, err := ApplySmyMembershipRequest(base, badRequest); err == nil {
		t.Fatal("unsupported period accepted")
	}
}

func TestSmyOrderPeriodMonthsAndRefunds(t *testing.T) {
	now := time.Date(2026, 1, 31, 2, 0, 0, 0, time.UTC)
	input := func(order, period string) SmyMembershipInput {
		return SmyMembershipInput{OrderNo: order, PeriodTime: period, Mobile: "13800000001", UniqueID: "u", ProductType: "YHK", ProductCode: "YHK01", OrderStatus: SmyOrderActive, Now: now}
	}
	apply := func(s MembershipSnapshot, req SmyMembershipInput) MembershipSnapshot {
		t.Helper()
		change, err := ApplySmyMembershipRequest(s, req)
		if err != nil {
			t.Fatal(err)
		}
		return change.After
	}
	for _, test := range []struct{ period, end string }{
		{"1", "2026-02-28T02:00:00Z"}, {"3", "2026-04-30T02:00:00Z"},
		{"6", "2026-07-31T02:00:00Z"}, {"12", "2027-01-31T02:00:00Z"},
	} {
		t.Run(test.period+" months", func(t *testing.T) {
			req := input("A", test.period)
			s := apply(MembershipSnapshot{SubscriptionName: "test"}, req)
			if s.ExpiresAt != test.end || s.Orders[0].OriginalExpiresAt != test.end {
				t.Fatalf("period %s: %+v", test.period, s)
			}
			duplicate, err := ApplySmyMembershipRequest(s, req)
			if err != nil || !duplicate.Duplicate || !reflect.DeepEqual(duplicate.After, s) {
				t.Fatal("duplicate changed period rights", err)
			}
		})
	}
	leap := time.Date(2028, 2, 29, 2, 0, 0, 0, time.UTC)
	if got := smyFormat(addSmyCalendarMonths(leap, 12)); got != "2029-02-28T02:00:00Z" {
		t.Fatal(got)
	}
	s := MembershipSnapshot{SubscriptionName: "mixed"}
	for _, req := range []SmyMembershipInput{input("A", "1"), input("B", "3"), input("C", "6")} {
		s = apply(s, req)
	}
	if s.ExpiresAt != "2026-11-28T02:00:00Z" {
		t.Fatal(s.ExpiresAt)
	}
	refund := input("B", "3")
	refund.OrderStatus = SmyOrderRefunded
	future := apply(s, refund)
	if future.ExpiresAt != "2026-08-28T02:00:00Z" || future.Orders[2].StartsAt != "2026-02-28T02:00:00Z" || future.Orders[2].PeriodTime != "6" {
		t.Fatal(future)
	}
	refund.Now = time.Date(2026, 3, 15, 2, 0, 0, 0, time.UTC)
	active := apply(s, refund)
	if active.ExpiresAt != "2026-09-15T02:00:00Z" || active.Orders[1].ExpiresAt != smyFormat(refund.Now) || active.Orders[2].StartsAt != smyFormat(refund.Now) {
		t.Fatal(active)
	}
	refund.Now = time.Date(2026, 6, 1, 2, 0, 0, 0, time.UTC)
	ended := apply(s, refund)
	if ended.ExpiresAt != s.ExpiresAt {
		t.Fatal("ended multi-month refund chased later rights")
	}
	conflict := input("B", "1")
	if _, err := ApplySmyMembershipRequest(s, conflict); err == nil {
		t.Fatal("period mismatch accepted")
	}
	for _, period := range []string{"", "0", "2", "-1", "03", "1.5", "13"} {
		if err := ValidateSmyMembershipRequest(input("invalid", period)); err == nil {
			t.Fatalf("invalid period %q accepted", period)
		}
	}
}

func TestSmyRefundUsesGrantedOrderPeriod(t *testing.T) {
	now := time.Date(2026, 1, 15, 2, 0, 0, 0, time.UTC)
	input := func(order, period string) SmyMembershipInput {
		return SmyMembershipInput{OrderNo: order, PeriodTime: period, Mobile: "13800000001", UniqueID: "u", ProductType: "YHK", ProductCode: "YHK01", OrderStatus: SmyOrderActive, Now: now}
	}
	apply := func(s MembershipSnapshot, req SmyMembershipInput) MembershipSnapshot {
		t.Helper()
		change, err := ApplySmyMembershipRequest(s, req)
		if err != nil {
			t.Fatal(err)
		}
		return change.After
	}
	s := MembershipSnapshot{SubscriptionName: "refund-period"}
	for _, req := range []SmyMembershipInput{input("A", "1"), input("B", "3"), input("C", "6")} {
		s = apply(s, req)
	}
	for _, period := range []string{"", "1", "12", "invalid", "-3"} {
		t.Run("refund period "+period, func(t *testing.T) {
			refund := input("B", period)
			refund.OrderStatus = SmyOrderRefunded
			future := apply(s, refund)
			if future.ExpiresAt != "2026-08-15T02:00:00Z" || future.Orders[1].PeriodTime != "3" || future.Orders[2].PeriodTime != "6" {
				t.Fatal("refund did not use saved periods")
			}
			duplicate, err := ApplySmyMembershipRequest(future, refund)
			if err != nil || !duplicate.Duplicate || !reflect.DeepEqual(duplicate.After, future) {
				t.Fatal("duplicate refund changed rights", err)
			}
			refund.Now = time.Date(2026, 3, 15, 2, 0, 0, 0, time.UTC)
			active := apply(s, refund)
			if active.ExpiresAt != "2026-09-15T02:00:00Z" || active.Orders[1].ExpiresAt != smyFormat(refund.Now) {
				t.Fatal("active refund calculation changed")
			}
			refund.Now = time.Date(2026, 6, 1, 2, 0, 0, 0, time.UTC)
			ended := apply(s, refund)
			if ended.ExpiresAt != s.ExpiresAt {
				t.Fatal("ended refund chased later rights")
			}
		})
	}
	refundFirst := input("unknown", "invalid")
	refundFirst.OrderStatus = SmyOrderRefunded
	tombstone := apply(MembershipSnapshot{SubscriptionName: "empty"}, refundFirst)
	if tombstone.Orders[0].PeriodTime != "" || tombstone.Orders[0].Granted {
		t.Fatal("refund first invented grant period")
	}
	lateGrant := input("unknown", "6")
	duplicate, err := ApplySmyMembershipRequest(tombstone, lateGrant)
	if err != nil || !duplicate.Duplicate || !reflect.DeepEqual(duplicate.After, tombstone) {
		t.Fatal("late grant revived tombstone", err)
	}
	corrupt := cloneSmySnapshot(s)
	corrupt.Orders[1].Status = SmyOrderRefunded
	corrupt.Orders[1].RefundAt = "2026-06-01T02:00:00Z"
	corrupt.Orders[1].PeriodTime = "invalid"
	if err := ValidateSmyMembershipSnapshot(corrupt); err == nil {
		t.Fatal("corrupt granted order period accepted")
	}
}

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
