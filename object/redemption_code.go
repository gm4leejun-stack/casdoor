package object

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/casdoor/casdoor/util"
	"github.com/xorm-io/core"
)

type RedemptionCodeState string

const (
	RedemptionCodeActive   RedemptionCodeState = "Active"
	RedemptionCodeUsed     RedemptionCodeState = "Used"
	RedemptionCodeDisabled RedemptionCodeState = "Disabled"
	RedemptionCodeExpired  RedemptionCodeState = "Expired"
)

type RedemptionCode struct {
	Owner        string              `xorm:"varchar(100) notnull pk" json:"owner"`
	Name         string              `xorm:"varchar(100) notnull pk" json:"name"`
	Batch        string              `xorm:"varchar(100) notnull" json:"batch"`
	CodeHash     string              `xorm:"varchar(128) notnull unique" json:"-"`
	CodeHint     string              `xorm:"varchar(20)" json:"codeHint"`
	Plan         string              `xorm:"varchar(100) notnull" json:"plan"`
	DurationDays int                 `xorm:"notnull" json:"durationDays"`
	State        RedemptionCodeState `xorm:"varchar(30) notnull" json:"state"`
	ExpireTime   string              `xorm:"varchar(100)" json:"expireTime"`
	RedeemedBy   string              `xorm:"varchar(100)" json:"redeemedBy"`
	RedeemedTime string              `xorm:"varchar(100)" json:"redeemedTime"`
	CreatedTime  string              `xorm:"varchar(100)" json:"createdTime"`
}

func (c *RedemptionCode) GetId() string { return fmt.Sprintf("%s/%s", c.Owner, c.Name) }

func (c *RedemptionCode) IsRedeemable(now time.Time, batch *RedemptionBatch) bool {
	if c.State != RedemptionCodeActive || batch == nil || batch.State != RedemptionBatchActive {
		return false
	}
	if c.ExpireTime != "" {
		expires, err := time.Parse(time.RFC3339, c.ExpireTime)
		if err != nil || !now.Before(expires) {
			return false
		}
	}
	return true
}

func GetRedemptionCode(id string) (*RedemptionCode, error) {
	owner, name, err := util.GetOwnerAndNameFromIdWithError(id)
	if err != nil {
		return nil, err
	}
	row := &RedemptionCode{Owner: owner, Name: name}
	found, err := ormer.Engine.Get(row)
	if err != nil || !found {
		return nil, err
	}
	return row, nil
}

func GetRedemptionCodeByHash(owner, digest string) (*RedemptionCode, error) {
	row := &RedemptionCode{Owner: owner, CodeHash: digest}
	found, err := ormer.Engine.Get(row)
	if err != nil || !found {
		return nil, err
	}
	return row, nil
}

func DisableRedemptionCode(owner, name string) (bool, error) {
	affected, err := ormer.Engine.ID(core.PK{owner, name}).Cols("state").Update(&RedemptionCode{State: RedemptionCodeDisabled})
	return affected != 0, err
}

var safeCodeCharacters = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
var codeStripper = regexp.MustCompile(`[^A-Z0-9]`)

func NormalizeRedemptionCode(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	return codeStripper.ReplaceAllString(value, "")
}

func (c *RedemptionCode) SafeHint() string {
	if len(c.CodeHint) > 8 {
		return c.CodeHint[len(c.CodeHint)-8:]
	}
	return c.CodeHint
}
