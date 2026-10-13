package object

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/casdoor/casdoor/util"
	"github.com/xorm-io/core"
	"github.com/xorm-io/xorm"
)

var ErrInvalidRedemptionCode = errors.New("invalid or unavailable redemption code")
var ErrPlanMismatch = errors.New("redemption code plan does not match membership plan")
var ErrPepperMissing = errors.New("redemption code pepper is not configured")

const generatedCodeBytes = 26

func GenerateRedemptionCode(random io.Reader) (string, error) {
	if random == nil {
		random = rand.Reader
	}
	buf := make([]byte, generatedCodeBytes)
	if _, err := io.ReadFull(random, buf); err != nil {
		return "", err
	}
	var result strings.Builder
	result.Grow(generatedCodeBytes)
	for _, b := range buf {
		result.WriteByte(safeCodeCharacters[int(b)&31])
	}
	return result.String(), nil
}

func HashRedemptionCode(pepper, code string) string {
	if pepper == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(pepper))
	mac.Write([]byte(NormalizeRedemptionCode(code)))
	return hex.EncodeToString(mac.Sum(nil))
}

func ValidateRedemptionPepper(pepper string) error {
	if len(pepper) < 16 {
		return ErrPepperMissing
	}
	return nil
}

func ExtendSubscription(sub *Subscription, now time.Time, duration time.Duration) error {
	if sub == nil || duration <= 0 {
		return errors.New("subscription and positive duration are required")
	}
	start, err := time.Parse(time.RFC3339, sub.StartTime)
	if err != nil {
		return fmt.Errorf("invalid subscription start time: %w", err)
	}
	end, err := time.Parse(time.RFC3339, sub.EndTime)
	if err != nil {
		return fmt.Errorf("invalid subscription end time: %w", err)
	}
	base := now
	if end.After(base) {
		base = end
	}
	sub.EndTime = base.Add(duration).UTC().Format(time.RFC3339)
	if end.Before(now) {
		sub.StartTime = now.UTC().Format(time.RFC3339)
	}
	if sub.State != SubStateSuspended {
		sub.State = SubStateActive
	}
	if start.After(now) {
		sub.State = SubStateUpcoming
	}
	return nil
}

type RedemptionRequest struct {
	Owner     string
	User      string
	Code      string
	Pepper    string
	RequestID string
	Now       time.Time
}

type RedemptionResult struct {
	CodeHint       string `json:"codeHint"`
	SubscriptionID string `json:"subscriptionId"`
	Plan           string `json:"plan"`
	StartsAt       string `json:"startsAt"`
	ExpiresAt      string `json:"expiresAt"`
}

// GeneratedRedemptionCode is returned only at generation time. The plaintext
// value is intentionally not part of RedemptionCode and cannot be recovered.
type GeneratedRedemptionCode struct {
	ID   string `json:"id"`
	Code string `json:"code"`
	Hint string `json:"hint"`
}

func GenerateBatchCodes(batch *RedemptionBatch, pepper string, random io.Reader) ([]*GeneratedRedemptionCode, error) {
	if batch == nil {
		return nil, errors.New("batch is required")
	}
	if err := ValidateRedemptionPepper(pepper); err != nil {
		return nil, err
	}
	if err := batch.Validate(time.Now().UTC()); err != nil {
		return nil, err
	}
	if random == nil {
		random = rand.Reader
	}
	session := ormer.Engine.NewSession()
	defer session.Close()
	if err := session.Begin(); err != nil {
		return nil, err
	}
	rollback := func(err error) ([]*GeneratedRedemptionCode, error) { _ = session.Rollback(); return nil, err }
	result := make([]*GeneratedRedemptionCode, 0, batch.Quantity)
	for i := 0; i < batch.Quantity; i++ {
		plain, err := GenerateRedemptionCode(random)
		if err != nil {
			return rollback(err)
		}
		id := "code_" + util.GenerateId()[:12]
		row := &RedemptionCode{Owner: batch.Owner, Name: id, Batch: batch.Name, CodeHash: HashRedemptionCode(pepper, plain), CodeHint: plain[len(plain)-8:], Plan: batch.Plan, DurationDays: batch.DurationDays, State: RedemptionCodeActive, ExpireTime: batch.CodeExpireTime, CreatedTime: util.GetCurrentTime()}
		if _, err = session.Insert(row); err != nil {
			return rollback(err)
		}
		result = append(result, &GeneratedRedemptionCode{ID: row.GetId(), Code: plain, Hint: row.CodeHint})
	}
	if _, err := session.ID(core.PK{batch.Owner, batch.Name}).Cols("state").Update(&RedemptionBatch{State: batch.State}); err != nil {
		return rollback(err)
	}
	if err := session.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

// CreateRedemptionBatch atomically stores the batch and all hashed codes.
// Callers receive plaintext codes only after the transaction commits.
func CreateRedemptionBatch(batch *RedemptionBatch, pepper string, random io.Reader) ([]*GeneratedRedemptionCode, error) {
	if batch == nil {
		return nil, errors.New("batch is required")
	}
	if err := ValidateRedemptionPepper(pepper); err != nil {
		return nil, err
	}
	if err := batch.Validate(time.Now().UTC()); err != nil {
		return nil, err
	}
	if batch.State == "" {
		batch.State = RedemptionBatchActive
	}
	if batch.CreatedTime == "" {
		batch.CreatedTime = util.GetCurrentTime()
	}
	if random == nil {
		random = rand.Reader
	}
	session := ormer.Engine.NewSession()
	defer session.Close()
	if err := session.Begin(); err != nil {
		return nil, err
	}
	rollback := func(err error) ([]*GeneratedRedemptionCode, error) { _ = session.Rollback(); return nil, err }
	if _, err := session.Insert(batch); err != nil {
		return rollback(err)
	}
	result := make([]*GeneratedRedemptionCode, 0, batch.Quantity)
	for i := 0; i < batch.Quantity; i++ {
		plain, err := GenerateRedemptionCode(random)
		if err != nil {
			return rollback(err)
		}
		id := "code_" + util.GenerateId()[:12]
		row := &RedemptionCode{Owner: batch.Owner, Name: id, Batch: batch.Name, CodeHash: HashRedemptionCode(pepper, plain), CodeHint: plain[len(plain)-8:], Plan: batch.Plan, DurationDays: batch.DurationDays, State: RedemptionCodeActive, ExpireTime: batch.CodeExpireTime, CreatedTime: util.GetCurrentTime()}
		if _, err = session.Insert(row); err != nil {
			return rollback(err)
		}
		result = append(result, &GeneratedRedemptionCode{ID: row.GetId(), Code: plain, Hint: row.CodeHint})
	}
	if err := session.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

func RedeemMembershipCode(req RedemptionRequest) (*RedemptionResult, error) {
	if req.Owner == "" || req.User == "" || req.Code == "" {
		return nil, ErrInvalidRedemptionCode
	}
	if err := ValidateRedemptionPepper(req.Pepper); err != nil {
		return nil, err
	}
	if req.Now.IsZero() {
		req.Now = time.Now().UTC()
	} else {
		req.Now = req.Now.UTC()
	}
	digest := HashRedemptionCode(req.Pepper, req.Code)
	session := ormer.Engine.NewSession()
	defer session.Close()
	if err := session.Begin(); err != nil {
		return nil, err
	}
	rollback := func(err error) (*RedemptionResult, error) { _ = session.Rollback(); return nil, err }
	if err := lockMembershipWrite(session, req.Owner); err != nil {
		return rollback(err)
	}
	code := &RedemptionCode{Owner: req.Owner, CodeHash: digest}
	found, err := session.ForUpdate().Get(code)
	if err != nil || !found {
		return rollback(ErrInvalidRedemptionCode)
	}
	batch := &RedemptionBatch{Owner: req.Owner, Name: code.Batch}
	found, err = session.ForUpdate().Get(batch)
	if err != nil || !found || !code.IsRedeemable(req.Now, batch) {
		return rollback(ErrInvalidRedemptionCode)
	}
	if batch.Plan != code.Plan || batch.DurationDays != code.DurationDays {
		return rollback(ErrInvalidRedemptionCode)
	}
	if err := rejectManagedMembership(session, &Subscription{Owner: req.Owner, User: req.User, Plan: code.Plan}); err != nil {
		return rollback(err)
	}
	// Conditional claim is the concurrency boundary. Only one transaction can
	// transition this code from Active to Used. A concurrent redemption gets a
	// stable unavailable response and cannot extend the subscription twice.
	claimed := &RedemptionCode{State: RedemptionCodeUsed, RedeemedBy: req.User, RedeemedTime: req.Now.Format(time.RFC3339)}
	affected, err := session.ID(core.PK{code.Owner, code.Name}).Where("state = ?", RedemptionCodeActive).Cols("state", "redeemed_by", "redeemed_time").Update(claimed)
	if err != nil || affected != 1 {
		return rollback(ErrInvalidRedemptionCode)
	}
	allUserSubscriptions := []*Subscription{}
	if err = session.Where("owner = ? AND "+quoteColumn("user")+" = ?", req.Owner, req.User).ForUpdate().Find(&allUserSubscriptions); err != nil {
		return rollback(err)
	}
	for _, existing := range allUserSubscriptions {
		if existing.Plan != code.Plan {
			return rollback(ErrPlanMismatch)
		}
	}
	sub, err := findCurrentSubscription(session, req.Owner, req.User, code.Plan)
	if err != nil {
		return rollback(err)
	}
	oldEnd := ""
	if sub == nil {
		now := req.Now
		sub = &Subscription{Owner: req.Owner, Name: "sub_" + util.GenerateId()[:12], DisplayName: "Membership - " + req.User, CreatedTime: now.Format(time.RFC3339), User: req.User, Plan: code.Plan, StartTime: now.Format(time.RFC3339), EndTime: now.AddDate(0, 0, code.DurationDays).Format(time.RFC3339), State: SubStateActive}
		if _, err = session.Insert(sub); err != nil {
			return rollback(err)
		}
	} else {
		oldEnd = sub.EndTime
		if err = ExtendSubscription(sub, req.Now, time.Duration(code.DurationDays)*24*time.Hour); err != nil {
			return rollback(err)
		}
		if _, err = session.ID(core.PK{sub.Owner, sub.Name}).AllCols().Update(sub); err != nil {
			return rollback(err)
		}
	}
	code.State, code.RedeemedBy, code.RedeemedTime = RedemptionCodeUsed, req.User, req.Now.Format(time.RFC3339)
	audit := &RedemptionAudit{Owner: req.Owner, CodeID: code.GetId(), BatchID: batch.GetId(), User: req.User, SubscriptionID: sub.GetId(), Plan: code.Plan, OldEndTime: oldEnd, NewEndTime: sub.EndTime, DurationDays: code.DurationDays, RequestID: req.RequestID, CreatedTime: req.Now.Format(time.RFC3339), Result: "Success"}
	if _, err = session.Insert(audit); err != nil {
		return rollback(err)
	}
	if err = session.Commit(); err != nil {
		return nil, err
	}
	return &RedemptionResult{CodeHint: code.SafeHint(), SubscriptionID: sub.GetId(), Plan: sub.Plan, StartsAt: sub.StartTime, ExpiresAt: sub.EndTime}, nil
}

func findCurrentSubscription(session *xorm.Session, owner, user, plan string) (*Subscription, error) {
	subs := []*Subscription{}
	if err := session.Where("owner = ? AND "+quoteColumn("user")+" = ? AND plan = ?", owner, user, plan).ForUpdate().Desc("created_time").Find(&subs); err != nil {
		return nil, err
	}
	if len(subs) == 0 {
		return nil, nil
	}
	if len(subs) > 1 {
		return nil, errors.New("multiple current subscriptions found")
	}
	return subs[0], nil
}
