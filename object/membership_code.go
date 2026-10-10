package object

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/casdoor/casdoor/util"
	"github.com/xorm-io/xorm"
)

type MembershipCode struct {
	Owner        string `xorm:"varchar(100) notnull pk" json:"owner"`
	Name         string `xorm:"varchar(100) notnull pk" json:"name"`
	CodeHash     string `xorm:"varchar(128) notnull unique" json:"-"`
	Plan         string `xorm:"varchar(100) notnull" json:"plan"`
	DurationDays int    `xorm:"notnull" json:"durationDays"`
	State        string `xorm:"varchar(20) notnull" json:"state"`
	User         string `xorm:"varchar(100)" json:"user"`
	RedeemedTime string `xorm:"varchar(100)" json:"redeemedTime"`
	CreatedTime  string `xorm:"varchar(100)" json:"createdTime"`
}

type MembershipRedemption struct {
	Owner        string `xorm:"varchar(100) notnull pk" json:"owner"`
	Name         string `xorm:"varchar(100) notnull pk" json:"name"`
	CodeName     string `xorm:"varchar(100)" json:"codeName"`
	User         string `xorm:"varchar(100)" json:"user"`
	Subscription string `xorm:"varchar(100)" json:"subscription"`
	OldEndTime   string `xorm:"varchar(100)" json:"oldEndTime"`
	NewEndTime   string `xorm:"varchar(100)" json:"newEndTime"`
	CreatedTime  string `xorm:"varchar(100)" json:"createdTime"`
}

const membershipCodeActive = "Active"

func membershipPepper() string { return os.Getenv("CASDOOR_MEMBERSHIP_CODE_PEPPER") }

func membershipCodeHash(code string) string {
	h := hmac.New(sha256.New, []byte(membershipPepper()))
	h.Write([]byte(strings.TrimSpace(code)))
	return fmt.Sprintf("%x", h.Sum(nil))
}

func GenerateMembershipCodes(owner, plan string, durationDays, quantity int) ([]string, error) {
	if owner == "" || plan == "" || durationDays <= 0 || quantity <= 0 || quantity > 1000 || membershipPepper() == "" {
		return nil, fmt.Errorf("invalid membership code parameters")
	}
	result := make([]string, 0, quantity)
	for i := 0; i < quantity; i++ {
		buf := make([]byte, 18)
		if _, err := rand.Read(buf); err != nil {
			return nil, err
		}
		code := "GM-" + base64.RawURLEncoding.EncodeToString(buf)
		row := &MembershipCode{Owner: owner, Name: "code_" + util.GenerateId()[:10], CodeHash: membershipCodeHash(code), Plan: plan, DurationDays: durationDays, State: membershipCodeActive, CreatedTime: util.GetCurrentTime()}
		if _, err := ormer.Engine.Insert(row); err != nil {
			return nil, err
		}
		result = append(result, code)
	}
	return result, nil
}

func RedeemMembershipCode(owner, user, code string) (*Subscription, error) {
	if owner == "" || user == "" || code == "" || membershipPepper() == "" {
		return nil, fmt.Errorf("invalid membership code")
	}
	var result *Subscription
	_, err := ormer.Engine.Transaction(func(tx *xorm.Session) (interface{}, error) {
		row := &MembershipCode{}
		ok, err := tx.ForUpdate().Where("owner = ? AND code_hash = ?", owner, membershipCodeHash(code)).Get(row)
		if err != nil || !ok || subtle.ConstantTimeCompare([]byte(row.CodeHash), []byte(membershipCodeHash(code))) != 1 || row.State != membershipCodeActive {
			return nil, fmt.Errorf("membership code is invalid or used")
		}
		now := time.Now().UTC()
		sub := &Subscription{}
		has, err := tx.Where("owner = ? AND user = ? AND plan = ?", owner, user, row.Plan).Desc("end_time").Get(sub)
		if err != nil {
			return nil, err
		}
		oldEnd := ""
		if !has {
			sub = &Subscription{Owner: owner, Name: "sub_" + util.GenerateId()[:10], User: user, Plan: row.Plan, StartTime: now.Format(time.RFC3339), EndTime: now.AddDate(0, 0, row.DurationDays).Format(time.RFC3339), State: SubStateActive, CreatedTime: util.GetCurrentTime()}
			if _, err = tx.Insert(sub); err != nil {
				return nil, err
			}
		} else {
			oldEnd = sub.EndTime
			end, parseErr := time.Parse(time.RFC3339, sub.EndTime)
			if parseErr != nil {
				return nil, parseErr
			}
			base := now
			if end.After(base) {
				base = end
			}
			sub.EndTime = base.AddDate(0, 0, row.DurationDays).Format(time.RFC3339)
			if sub.State == SubStateExpired {
				sub.StartTime = now.Format(time.RFC3339)
				sub.State = SubStateActive
			}
			if _, err = tx.ID(util.GetId(sub.Owner, sub.Name)).AllCols().Update(sub); err != nil {
				return nil, err
			}
		}
		row.State = "Used"
		row.User = user
		row.RedeemedTime = now.Format(time.RFC3339)
		if _, err = tx.ID(util.GetId(row.Owner, row.Name)).AllCols().Update(row); err != nil {
			return nil, err
		}
		audit := &MembershipRedemption{Owner: owner, Name: "redemption_" + util.GenerateId()[:10], CodeName: row.Name, User: user, Subscription: sub.GetId(), OldEndTime: oldEnd, NewEndTime: sub.EndTime, CreatedTime: now.Format(time.RFC3339)}
		if _, err = tx.Insert(audit); err != nil {
			return nil, err
		}
		result = sub
		return nil, nil
	})
	return result, err
}

func GetMembershipEntitlement(owner, user, plan string) (map[string]interface{}, error) {
	if owner == "" || user == "" || plan == "" {
		return nil, fmt.Errorf("invalid membership query")
	}
	subs, err := GetSubscriptionsByUser(owner, user)
	if err != nil {
		return nil, err
	}
	for _, sub := range subs {
		if sub.Plan == plan {
			_ = sub.UpdateState()
			return map[string]interface{}{"subject": map[string]string{"owner": owner, "userName": user}, "plan": plan, "status": string(sub.State), "startsAt": sub.StartTime, "expiresAt": sub.EndTime, "subscriptionId": sub.GetId(), "evaluatedAt": time.Now().UTC().Format(time.RFC3339)}, nil
		}
	}
	return map[string]interface{}{"subject": map[string]string{"owner": owner, "userName": user}, "plan": plan, "status": "Missing", "startsAt": "", "expiresAt": "", "evaluatedAt": time.Now().UTC().Format(time.RFC3339)}, nil
}
