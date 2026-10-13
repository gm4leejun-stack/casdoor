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

// Uses a separate, explicitly disposable database and never clears existing
// tables. Keep identity/ticket regressions isolated from membership fixtures.
func TestSmyUnionLoginPostgresTransactions(t *testing.T) {
	dsn := os.Getenv("CASDOOR_UNION_LOGIN_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("CASDOOR_UNION_LOGIN_TEST_POSTGRES_DSN is required")
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
	if !strings.HasPrefix(database, "smy_union_login_agent_") {
		t.Fatalf("refusing non-disposable database %q", database)
	}
	var tables []struct{ Name string }
	if err := engine.SQL("SELECT tablename AS name FROM pg_tables WHERE schemaname = 'public'").Find(&tables); err != nil || len(tables) != 0 {
		t.Fatalf("integration database must be empty: %v", err)
	}
	if err := engine.Sync2(new(User), new(Organization), new(Application), new(SmyMembershipCustomer), new(SmyUnionLoginTicket), new(Syncer)); err != nil {
		t.Fatal(err)
	}
	oldOrmer := ormer
	ormer = &Ormer{Engine: engine, driverName: "postgres"}
	defer func() { ormer = oldOrmer }()
	appID := "admin/union-lobehub"
	if _, err := engine.Insert(&Organization{Owner: "admin", Name: SmyMembershipOwner}, &Application{Owner: "admin", Name: "union-lobehub", Organization: SmyMembershipOwner, EnableAutoSignin: true}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 15, 2, 0, 0, 0, time.UTC)
	req := SmyUnionLoginRequest{UniqueID: "union-customer", MerchantNo: "merchant", RequestID: "request1", Application: appID, RequestTime: now, Now: now}
	token, err := IssueSmyUnionLogin(req, nil)
	if err != nil {
		t.Fatal(err)
	}
	user := new(User)
	found, err := engine.Where("owner = ?", SmyMembershipOwner).Get(user)
	if err != nil || !found || user.Phone != "" || user.Email != "" || user.Password != "" || user.Type != "normal-user" || user.IsAdmin {
		t.Fatalf("unsafe new union login account: %+v %v", user, err)
	}
	customer := new(SmyMembershipCustomer)
	if found, err := engine.Where("unique_id = ?", req.UniqueID).Get(customer); err != nil || !found || customer.Sequence != 0 || customer.UserID != user.Id {
		t.Fatalf("union login unexpectedly initialized membership: %+v %v", customer, err)
	}
	ticket := new(SmyUnionLoginTicket)
	if found, err := engine.ID(core.PK{req.MerchantNo, req.RequestID}).Get(ticket); err != nil || !found || ticket.TokenHash == token || ticket.TokenHash != HashMagicLinkSecret(token) || ticket.ExpiresAt-ticket.CreatedAt != 300 {
		t.Fatalf("invalid stored ticket: %+v %v", ticket, err)
	}
	if _, err := IssueSmyUnionLogin(req, nil); !errors.Is(err, ErrSmyUnionLoginReplay) {
		t.Fatalf("signed request replay accepted: %v", err)
	}
	if _, _, err := ConsumeSmyUnionLogin(token, "admin/wrong-app", now, nil); !errors.Is(err, ErrSmyUnionLoginTicket) {
		t.Fatalf("wrong target app accepted: %v", err)
	}
	preflightErr := errors.New("security preflight failed")
	if _, _, err := ConsumeSmyUnionLogin(token, appID, now, func(*User, *Application) error { return preflightErr }); !errors.Is(err, preflightErr) {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			got, _, err := ConsumeSmyUnionLogin(token, appID, now.Add(time.Second), nil)
			if err == nil && got.Id != user.Id {
				err = errors.New("redemption returned wrong account")
			}
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	succeeded := 0
	for err := range results {
		if err == nil {
			succeeded++
		} else if !errors.Is(err, ErrSmyUnionLoginTicket) {
			t.Fatal(err)
		}
	}
	if succeeded != 1 {
		t.Fatalf("concurrent redemption succeeded %d times", succeeded)
	}
	req.RequestID = "request2"
	expired, err := IssueSmyUnionLogin(req, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ConsumeSmyUnionLogin(expired, appID, now.Add(SmyUnionLoginLifetime), nil); !errors.Is(err, ErrSmyUnionLoginTicket) {
		t.Fatalf("expired ticket accepted: %v", err)
	}
	// An expired/consumed request row is still a replay tombstone, even if a
	// later request uses a fresh timestamp and a different asserted identity.
	replay := req
	replay.UniqueID = "replayedDifferentCustomer"
	replay.Now, replay.RequestTime = now.Add(time.Hour), now.Add(time.Hour)
	if _, err := IssueSmyUnionLogin(replay, nil); !errors.Is(err, ErrSmyUnionLoginReplay) {
		t.Fatalf("expired request tombstone did not prevent replay: %v", err)
	}
	if count, err := engine.Where("unique_id = ?", replay.UniqueID).Count(new(SmyMembershipCustomer)); err != nil || count != 0 {
		t.Fatalf("replay created a different identity: %d %v", count, err)
	}
	req.RequestID = "request3"
	disabled, err := IssueSmyUnionLogin(req, nil)
	if err != nil {
		t.Fatal(err)
	}
	user.IsForbidden = true
	if _, err := engine.ID(core.PK{user.Owner, user.Name}).Cols("is_forbidden").Update(user); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ConsumeSmyUnionLogin(disabled, appID, now, nil); err == nil {
		t.Fatal("account disabled after issuance was allowed to sign in")
	}
	rollback := req
	rollback.RequestID, rollback.UniqueID = "rollbackReq", "rollbackCustomer"
	if _, err := IssueSmyUnionLogin(rollback, func(string) error { return preflightErr }); !errors.Is(err, preflightErr) {
		t.Fatal(err)
	}
	if count, err := engine.Where("unique_id = ?", rollback.UniqueID).Count(new(SmyMembershipCustomer)); err != nil || count != 0 {
		t.Fatalf("response failure leaked customer: %d %v", count, err)
	}
	if count, err := engine.ID(core.PK{rollback.MerchantNo, rollback.RequestID}).Count(new(SmyUnionLoginTicket)); err != nil || count != 0 {
		t.Fatalf("response failure leaked ticket: %d %v", count, err)
	}
	// Concurrent, distinct signed requests for an unseen uniqueId share one user.
	var issuance sync.WaitGroup
	issueErrors := make(chan error, 2)
	for _, id := range []string{"concurrent1", "concurrent2"} {
		issuance.Add(1)
		go func(id string) {
			defer issuance.Done()
			req := rollback
			req.RequestID, req.UniqueID = id, "concurrentCustomer"
			_, err := IssueSmyUnionLogin(req, nil)
			issueErrors <- err
		}(id)
	}
	issuance.Wait()
	close(issueErrors)
	for err := range issueErrors {
		if err != nil {
			t.Fatal(err)
		}
	}
	if count, err := engine.Where("unique_id = ?", "concurrentCustomer").Count(new(SmyMembershipCustomer)); err != nil || count != 1 {
		t.Fatalf("concurrent issuance duplicated binding: %d %v", count, err)
	}
	if count, err := engine.Where("owner = ?", SmyMembershipOwner).Count(new(User)); err != nil || count != 2 {
		t.Fatalf("concurrent issuance/rollback leaked account: %d %v", count, err)
	}
	// The app and binding are rechecked when the browser arrives, not merely
	// when the service-to-service call signed its link.
	req.RequestID, req.UniqueID = "appDisabledReq", "concurrentCustomer"
	appDisabled, err := IssueSmyUnionLogin(req, nil)
	if err != nil {
		t.Fatal(err)
	}
	app := &Application{Owner: "admin", Name: "union-lobehub", DisableSignin: true}
	if _, err := engine.ID(core.PK{app.Owner, app.Name}).Cols("disable_signin").Update(app); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ConsumeSmyUnionLogin(appDisabled, appID, now, nil); err == nil {
		t.Fatal("application disabled after issuance was allowed")
	}
	app.DisableSignin = false
	if _, err := engine.ID(core.PK{app.Owner, app.Name}).Cols("disable_signin").Update(app); err != nil {
		t.Fatal(err)
	}
	bound := new(SmyMembershipCustomer)
	if found, err := engine.Where("unique_id = ?", req.UniqueID).Get(bound); err != nil || !found {
		t.Fatal(err)
	}
	bound.UserName = "tampered-binding"
	if _, err := engine.ID(bound.UserID).Cols("user_name").Update(bound); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ConsumeSmyUnionLogin(appDisabled, appID, now, nil); err == nil {
		t.Fatal("changed binding after issuance was allowed")
	}
	t.Logf("isolated union login database retained for inspection: %s", database)
}

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
	t.Run("legacy phone unique index migration is explicit and repeatable", func(t *testing.T) {
		if _, err := engine.Exec(`CREATE UNIQUE INDEX "UQE_smy_membership_customer_mobile" ON smy_membership_customer (mobile)`); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 2; i++ {
			if err := MigrateSmyIdentityIndexes(engine); err != nil {
				t.Fatal(err)
			}
		}
	})
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
	identity := func(t *testing.T, unique string) (*User, *SmyMembershipCustomer) {
		t.Helper()
		s := engine.NewSession()
		defer s.Close()
		if err := s.Begin(); err != nil {
			t.Fatal(err)
		}
		defer s.Rollback()
		user, customer, err := ResolveSmyIdentity(s, unique, now)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Commit(); err != nil {
			t.Fatal(err)
		}
		return user, customer
	}
	t.Run("concurrent identity-only creation produces one binding", func(t *testing.T) {
		var wg sync.WaitGroup
		failures := make(chan error, 4)
		ids := make(chan string, 4)
		start := make(chan struct{})
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				s := engine.NewSession()
				defer s.Close()
				if err := s.Begin(); err != nil {
					failures <- err
					return
				}
				defer s.Rollback()
				user, _, err := ResolveSmyIdentity(s, "concurrent-identity", now)
				if err != nil {
					failures <- err
					return
				}
				if err := s.Commit(); err != nil {
					failures <- err
					return
				}
				ids <- user.Id
			}()
		}
		close(start)
		wg.Wait()
		close(failures)
		close(ids)
		for err := range failures {
			t.Fatal(err)
		}
		seen := map[string]bool{}
		for id := range ids {
			seen[id] = true
		}
		if len(seen) != 1 {
			t.Fatalf("concurrent identity created %d users", len(seen))
		}
	})
	t.Run("identity first creates no membership and later grant reuses account", func(t *testing.T) {
		user, customer := identity(t, "identity-first")
		if user.Password != "" || user.Phone != "" || user.Email != "" || customer.Mobile != "" || customer.Sequence != 0 {
			t.Fatal("identity-only account includes credentials, contact, or ledger")
		}
		h, err := GetSmyMembershipHistory("uniqueId", customer.UniqueID)
		if err != nil || h.ConsistencyError != "" || h.MembershipInitialized || len(h.Operations) != 0 || h.Subscription != nil {
			t.Fatalf("identity-only history is invalid: %+v %v", h, err)
		}
		s := engine.NewSession()
		if err := rejectManagedMembership(s, &Subscription{Owner: SmyMembershipOwner, Plan: SmyMembershipPlan, User: user.Name}); err != nil {
			s.Close()
			t.Fatalf("identity-only account was treated as managed membership: %v", err)
		}
		s.Close()
		a := request("identity-A", "13800000120", customer.UniqueID)
		if err := ProcessSmyMembership(a, nil); err != nil {
			t.Fatal(err)
		}
		b := a
		b.OrderNo, b.Mobile = "identity-B", "13900000120"
		if err := ProcessSmyMembership(b, nil); err != nil {
			t.Fatal(err)
		}
		h = history(t, b.Mobile)
		if h.Customer.UserID != user.Id || !h.MembershipInitialized || len(h.Operations) != 3 {
			t.Fatal("membership did not initialize and reuse identity")
		}
		a.Mobile = b.Mobile
		if err := ProcessSmyMembership(a, nil); err != nil {
			t.Fatal(err)
		}
		if len(history(t, b.Mobile).Operations) != 3 {
			t.Fatal("changed phone duplicate wrote operation")
		}
		other := request("identity-other", b.Mobile, "identity-other")
		if err := ProcessSmyMembership(other, nil); err != nil {
			t.Fatal(err)
		}
		candidates, err := GetSmyMembershipHistory("mobile", b.Mobile)
		if err != nil || len(candidates.Candidates) != 2 {
			t.Fatalf("phone matches must return both separate identities: %+v %v", candidates, err)
		}
	})
	t.Run("legacy binding preserves user and contact while phone changes", func(t *testing.T) {
		user, customer := identity(t, "legacy-binding")
		user.Phone, user.Email = "13800000121", "legacy@example.invalid"
		if _, err := engine.ID(core.PK{user.Owner, user.Name}).Cols("phone", "email").Update(user); err != nil {
			t.Fatal(err)
		}
		customer.Mobile = user.Phone
		if _, err := engine.ID(customer.UserID).Cols("mobile").Update(customer); err != nil {
			t.Fatal(err)
		}
		req := request("legacy-A", "13900000121", customer.UniqueID)
		if err := ProcessSmyMembership(req, nil); err != nil {
			t.Fatal(err)
		}
		reused, bound := identity(t, customer.UniqueID)
		if reused.Id != user.Id || reused.Phone != user.Phone || reused.Email != user.Email || bound.Mobile != customer.Mobile {
			t.Fatal("existing user or historical binding contact changed")
		}
	})
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
		count, err := engine.Where("unique_id = ?", a.UniqueID).Count(new(SmyMembershipCustomer))
		if err != nil || count != 1 {
			t.Fatalf("created %d users: %v", count, err)
		}
		user := new(User)
		found, err := engine.ID(core.PK{SmyMembershipOwner, h.Customer.UserName}).Get(user)
		if err != nil || !found {
			t.Fatal(err)
		}
		if user.Password != "" || user.Phone != "" || user.Email != "" || user.Type != "normal-user" || user.IsAdmin {
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
		count, err := engine.Where("unique_id = ?", req.UniqueID).Count(new(SmyMembershipCustomer))
		if err != nil || count != 0 {
			t.Fatalf("rollback leaked customer: %d %v", count, err)
		}
		count, err = engine.ID(req.OrderNo).Count(new(SmyMembershipOrderRecord))
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
		count, err := engine.Where("unique_id = ?", req.UniqueID).Count(new(SmyMembershipCustomer))
		if err != nil || count != 0 {
			t.Fatal("cross customer rejection leaked account", err)
		}
		req = request("existing-rollback", "13800000102", h.Customer.UniqueID)
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
		if len(history(t, "13800000102").Operations) != 3 {
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
		req := request("tampered-C", "13800000102", h.Customer.UniqueID)
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
