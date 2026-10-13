package object

import (
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xorm-io/core"
	"github.com/xorm-io/xorm"
)

// The full concurrency suite is opt-in because it requires a disposable
// PostgreSQL database. CI must set CASDOOR_TEST_POSTGRES_DSN.
func TestRedemptionPostgresRequiresIntegrationDatabase(t *testing.T) {
	if os.Getenv("CASDOOR_TEST_POSTGRES_DSN") == "" {
		t.Skip("CASDOOR_TEST_POSTGRES_DSN is required for redemption concurrency tests")
	}
	// Database wiring is shared with Casdoor's adapter integration suite. This
	// sentinel prevents CI from silently claiming PostgreSQL coverage without a
	// real DSN; behavior tests live in the deployment repository's integration job.
}

// This test never resets or drops any tables. It requires a separately created
// disposable database whose name starts with smy_membership_agent_.
func TestSmyMembershipPostgresTransactions(t *testing.T) {
	dsn := os.Getenv("CASDOOR_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("CASDOOR_TEST_POSTGRES_DSN must point at a new disposable PostgreSQL database")
	}
	engine, err := xorm.NewEngine("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	var database string
	if _, err := engine.SQL("SELECT current_database()").Get(&database); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(database, "smy_membership_agent_") {
		t.Fatalf("refusing non-disposable database %q", database)
	}
	tables := []*struct{ Name string }{}
	if err := engine.SQL("SELECT tablename AS name FROM pg_tables WHERE schemaname = 'public'").Find(&tables); err != nil {
		t.Fatal(err)
	}
	if len(tables) > 0 {
		t.Fatal("integration database must be empty; retained test data can be inspected, use a new database for another run")
	}
	if err := engine.Sync2(new(User), new(Organization), new(Plan), new(Subscription), new(SmyMembershipCustomer), new(SmyMembershipOrderRecord), new(SmyMembershipOperation), new(Syncer), new(RedemptionBatch), new(RedemptionCode)); err != nil {
		t.Fatal(err)
	}
	oldOrmer := ormer
	ormer = &Ormer{Engine: engine, driverName: "postgres"}
	defer func() { ormer = oldOrmer }()
	if _, err := engine.Insert(&Organization{Owner: "admin", Name: SmyMembershipOwner}, &Plan{Owner: SmyMembershipOwner, Name: SmyMembershipPlan, IsEnabled: true, Period: PeriodMonthly}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 15, 2, 0, 0, 0, time.UTC)
	request := func(order, mobile, unique string) SmyMembershipInput {
		return SmyMembershipInput{OrderNo: order, Mobile: mobile, UniqueID: unique, ProductType: "1", ProductCode: "beibei-pro", PeriodTime: "1", OrderStatus: SmyOrderActive, Now: now}
	}
	history := func(t *testing.T, mobile string) *SmyMembershipHistory {
		t.Helper()
		result, err := GetSmyMembershipHistory("mobile", mobile)
		if err != nil {
			t.Fatal(err)
		}
		if result.ConsistencyError != "" {
			t.Fatalf("inconsistent history: %s", result.ConsistencyError)
		}
		return result
	}
	parallel := func(t *testing.T, requests []SmyMembershipInput) {
		t.Helper()
		var wg sync.WaitGroup
		failures := make(chan error, len(requests))
		start := make(chan struct{})
		for _, req := range requests {
			wg.Add(1)
			go func(req SmyMembershipInput) { defer wg.Done(); <-start; failures <- ProcessSmyMembership(req, nil) }(req)
		}
		close(start)
		wg.Wait()
		close(failures)
		for err := range failures {
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Run("duplicate grant and refund are each effective once", func(t *testing.T) {
		req := request("duplicate-A", "13800000101", "customer-duplicate")
		req.PeriodTime = "3"
		parallel(t, []SmyMembershipInput{req, req, req, req})
		h := history(t, req.Mobile)
		if len(h.Operations) != 2 || len(h.Orders) != 1 {
			t.Fatalf("duplicate grant wrote extra operations: %+v", h)
		}
		if h.Subscription.EndTime != addSmyCalendarMonths(now, 3).Format(time.RFC3339Nano) {
			t.Fatal("duplicate grant did not preserve the three-month period")
		}
		req.OrderStatus = SmyOrderRefunded
		req.PeriodTime = "invalid" // refunds must not validate the notification period
		req.Now = now.AddDate(0, 0, 10)
		parallel(t, []SmyMembershipInput{req, req, req})
		h = history(t, req.Mobile)
		if len(h.Operations) != 3 || h.Orders[0].Status != SmyOrderRefunded || h.Subscription.EndTime != req.Now.Format(time.RFC3339Nano) {
			t.Fatalf("refund not idempotent: %+v", h)
		}
		req.OrderStatus = SmyOrderActive
		req.PeriodTime = "3"
		if err := ProcessSmyMembership(req, nil); err != nil {
			t.Fatal(err)
		}
		if len(history(t, req.Mobile).Operations) != 3 {
			t.Fatal("late grant revived refunded order")
		}
	})
	t.Run("first account concurrent distinct orders", func(t *testing.T) {
		a := request("concurrent-A", "13800000102", "customer-concurrent")
		a.PeriodTime = "3"
		b := a
		b.OrderNo = "concurrent-B"
		b.PeriodTime = "6"
		parallel(t, []SmyMembershipInput{a, b})
		h := history(t, a.Mobile)
		if len(h.Operations) != 3 || len(h.Orders) != 2 {
			t.Fatalf("concurrent orders lost: %+v", h)
		}
		if h.Subscription.EndTime != addSmyCalendarMonths(now, 9).Format(time.RFC3339Nano) {
			t.Fatal("distinct orders did not add their three and six month periods")
		}
		count, err := engine.Where("owner = ? AND phone = ?", SmyMembershipOwner, a.Mobile).Count(new(User))
		if err != nil || count != 1 {
			t.Fatalf("created %d users: %v", count, err)
		}
		user := new(User)
		found, err := engine.ID(core.PK{SmyMembershipOwner, h.Customer.UserName}).Get(user)
		if err != nil || !found {
			t.Fatal(err)
		}
		if user.Password != "" || user.Type != "normal-user" || user.IsAdmin {
			t.Fatal("automatic account has unsafe credentials or privilege")
		}
	})
	t.Run("mixed periods refund replays each order months", func(t *testing.T) {
		a := request("mixed-A", "13800000110", "customer-mixed")
		b := a
		b.OrderNo = "mixed-B"
		b.PeriodTime = "3"
		c := a
		c.OrderNo = "mixed-C"
		c.PeriodTime = "6"
		for _, req := range []SmyMembershipInput{a, b, c} {
			if err := ProcessSmyMembership(req, nil); err != nil {
				t.Fatal(err)
			}
		}
		h := history(t, a.Mobile)
		if h.Subscription.EndTime != "2027-08-15T02:00:00Z" {
			t.Fatal(h.Subscription.EndTime)
		}
		conflict := b
		conflict.PeriodTime = "1"
		if err := ProcessSmyMembership(conflict, nil); err == nil {
			t.Fatal("changed period accepted")
		}
		if len(history(t, a.Mobile).Operations) != 4 {
			t.Fatal("changed period wrote operation")
		}
		b.OrderStatus = SmyOrderRefunded
		b.PeriodTime = "1" // original B granted three months
		repeat := b
		repeat.PeriodTime = ""
		parallel(t, []SmyMembershipInput{b, repeat})
		h = history(t, a.Mobile)
		if h.Subscription.EndTime != "2027-05-15T02:00:00Z" || h.Orders[2].StartsAt != "2026-11-15T02:00:00Z" || h.Orders[1].PeriodTime != "3" || len(h.Operations) != 5 {
			t.Fatal("multi-month refund reordering failed")
		}
		c.OrderStatus = SmyOrderRefunded
		c.PeriodTime = "invalid" // original C granted six months
		c.Now = time.Date(2026, 12, 1, 2, 0, 0, 0, time.UTC)
		if err := ProcessSmyMembership(c, nil); err != nil {
			t.Fatal(err)
		}
		h = history(t, a.Mobile)
		if h.Subscription.EndTime != smyFormat(c.Now) || h.Orders[2].PeriodTime != "6" || h.Operations[5].Input.PeriodTime != "invalid" || len(h.Operations) != 6 {
			t.Fatal("active multi-month refund failed")
		}
	})
	t.Run("refund first prevents delayed grant", func(t *testing.T) {
		req := request("refund-first-A", "13800000103", "customer-refund-first")
		req.OrderStatus = SmyOrderRefunded
		req.PeriodTime = ""
		if err := ProcessSmyMembership(req, nil); err != nil {
			t.Fatal(err)
		}
		req.OrderStatus = SmyOrderActive
		req.PeriodTime = "6"
		req.Now = now.Add(time.Hour)
		if err := ProcessSmyMembership(req, nil); err != nil {
			t.Fatal(err)
		}
		h := history(t, req.Mobile)
		if h.Subscription != nil || len(h.Operations) != 2 || h.Orders[0].Granted {
			t.Fatalf("refund-first granted membership: %+v", h)
		}
	})
	t.Run("callback failure rolls everything back", func(t *testing.T) {
		req := request("rollback-A", "13800000104", "customer-rollback")
		marker := errors.New("signed response preparation failed")
		if err := ProcessSmyMembership(req, func() error { return marker }); !errors.Is(err, marker) {
			t.Fatalf("expected callback failure: %v", err)
		}
		for _, bean := range []interface{}{new(User), new(SmyMembershipCustomer)} {
			field := "mobile"
			if _, ok := bean.(*User); ok {
				field = "phone"
			}
			count, err := engine.Where(field+" = ?", req.Mobile).Count(bean)
			if err != nil || count != 0 {
				t.Fatalf("rollback leaked %T row: %d %v", bean, count, err)
			}
		}
		count, err := engine.ID(req.OrderNo).Count(new(SmyMembershipOrderRecord))
		if err != nil || count != 0 {
			t.Fatal("rollback leaked order")
		}
		if err := ProcessSmyMembership(req, nil); err != nil {
			t.Fatal(err)
		}
		if len(history(t, req.Mobile).Operations) != 2 {
			t.Fatal("retry after rollback failed")
		}
	})
	t.Run("cross customer order and transaction rollback preserve existing membership", func(t *testing.T) {
		h := history(t, "13800000102")
		req := request("concurrent-A", "13800000109", "other-customer")
		if err := ProcessSmyMembership(req, nil); err == nil {
			t.Fatal("cross customer order accepted")
		}
		count, err := engine.Where("phone = ?", req.Mobile).Count(new(User))
		if err != nil || count != 0 {
			t.Fatal("cross customer rejection leaked account", err)
		}
		req = request("existing-rollback", h.Customer.Mobile, h.Customer.UniqueID)
		if err := ProcessSmyMembership(req, func() error { return errors.New("rollback") }); err == nil {
			t.Fatal("failure accepted")
		}
		after := history(t, req.Mobile)
		if after.Customer.Sequence != h.Customer.Sequence || after.Subscription.EndTime != h.Subscription.EndTime || len(after.Orders) != len(h.Orders) {
			t.Fatal("existing membership rollback failed")
		}
	})
	t.Run("managed writes fail and redemption claim rolls back", func(t *testing.T) {
		h := history(t, "13800000102")
		sub := *h.Subscription
		next := sub
		next.EndTime = AddSmyCalendarMonth(now).Format(time.RFC3339Nano)
		if _, err := UpdateSubscription(sub.GetId(), &next); !errors.Is(err, ErrManagedMembership) {
			t.Fatalf("update guard: %v", err)
		}
		next = sub
		next.Name = "extra-subscription"
		if _, err := AddSubscription(&next); !errors.Is(err, ErrManagedMembership) {
			t.Fatalf("add guard: %v", err)
		}
		if _, err := DeleteSubscription(&sub); !errors.Is(err, ErrManagedMembership) {
			t.Fatalf("delete guard: %v", err)
		}
		pepper, plain := "integration-pepper-123456789", "ABCDEFGHJKLMNPQRSTUVWXYZ23"
		batch := &RedemptionBatch{Owner: SmyMembershipOwner, Name: "guard-batch", Plan: SmyMembershipPlan, DurationDays: 30, Quantity: 1, State: RedemptionBatchActive}
		code := &RedemptionCode{Owner: SmyMembershipOwner, Name: "guard-code", Batch: batch.Name, Plan: SmyMembershipPlan, DurationDays: 30, State: RedemptionCodeActive, CodeHash: HashRedemptionCode(pepper, plain)}
		if _, err := engine.Insert(batch, code); err != nil {
			t.Fatal(err)
		}
		if _, err := RedeemMembershipCode(RedemptionRequest{Owner: SmyMembershipOwner, User: h.Customer.UserName, Code: plain, Pepper: pepper, Now: now}); !errors.Is(err, ErrManagedMembership) {
			t.Fatalf("redemption guard: %v", err)
		}
		stored := new(RedemptionCode)
		if found, err := engine.ID(core.PK{code.Owner, code.Name}).Get(stored); err != nil || !found || stored.State != RedemptionCodeActive {
			t.Fatalf("redemption claim leaked: %+v %v", stored, err)
		}
		if len(history(t, h.Customer.Mobile).Operations) != 3 {
			t.Fatal("guarded operation changed ledger")
		}
	})
	t.Run("tampered subscription fails closed but history remains available", func(t *testing.T) {
		h := history(t, "13800000102")
		sub := *h.Subscription
		sub.EndTime = AddSmyCalendarMonth(AddSmyCalendarMonth(AddSmyCalendarMonth(now))).Format(time.RFC3339Nano)
		if _, err := engine.ID(core.PK{sub.Owner, sub.Name}).Cols("end_time").Update(&sub); err != nil {
			t.Fatal(err)
		}
		req := request("tampered-C", h.Customer.Mobile, h.Customer.UniqueID)
		if err := ProcessSmyMembership(req, nil); err == nil {
			t.Fatal("tampered subscription was overwritten")
		}
		inconsistent, err := GetSmyMembershipHistory("orderNo", "concurrent-A")
		if err != nil {
			t.Fatal(err)
		}
		if inconsistent.ConsistencyError == "" || len(inconsistent.Operations) != 3 || inconsistent.Subscription.EndTime != sub.EndTime {
			t.Fatalf("tampered history unavailable: %+v", inconsistent)
		}
		count, err := engine.ID(req.OrderNo).Count(new(SmyMembershipOrderRecord))
		if err != nil || count != 0 {
			t.Fatal("failed inconsistency check leaked order")
		}
	})
	t.Logf("isolated database retained for inspection: %s", database)
}
