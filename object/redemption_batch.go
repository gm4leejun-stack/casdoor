package object

import (
	"fmt"
	"time"

	"github.com/casdoor/casdoor/util"
	"github.com/xorm-io/core"
)

type RedemptionBatchState string

const (
	RedemptionBatchActive    RedemptionBatchState = "Active"
	RedemptionBatchDisabled  RedemptionBatchState = "Disabled"
	RedemptionBatchExhausted RedemptionBatchState = "Exhausted"
	RedemptionBatchExpired   RedemptionBatchState = "Expired"
)

type RedemptionBatch struct {
	Owner          string               `xorm:"varchar(100) notnull pk" json:"owner"`
	Name           string               `xorm:"varchar(100) notnull pk" json:"name"`
	DisplayName    string               `xorm:"varchar(200)" json:"displayName"`
	Plan           string               `xorm:"varchar(100) notnull" json:"plan"`
	DurationDays   int                  `xorm:"notnull" json:"durationDays"`
	Quantity       int                  `xorm:"notnull" json:"quantity"`
	CodeExpireTime string               `xorm:"varchar(100)" json:"codeExpireTime"`
	State          RedemptionBatchState `xorm:"varchar(30) notnull" json:"state"`
	CreatedTime    string               `xorm:"varchar(100)" json:"createdTime"`
	CreatedBy      string               `xorm:"varchar(100)" json:"createdBy"`
}

func (b *RedemptionBatch) GetId() string { return fmt.Sprintf("%s/%s", b.Owner, b.Name) }

func (b *RedemptionBatch) Validate(now time.Time) error {
	if b.Owner == "" || b.Name == "" || b.Plan == "" {
		return fmt.Errorf("owner, name, and plan are required")
	}
	if b.DurationDays <= 0 || b.DurationDays > 3650 {
		return fmt.Errorf("durationDays must be between 1 and 3650")
	}
	if b.Quantity <= 0 || b.Quantity > 1000000 {
		return fmt.Errorf("quantity must be between 1 and 1000000")
	}
	if b.CodeExpireTime != "" {
		expires, err := time.Parse(time.RFC3339, b.CodeExpireTime)
		if err != nil {
			return fmt.Errorf("invalid codeExpireTime: %w", err)
		}
		if !expires.After(now) {
			return fmt.Errorf("codeExpireTime must be in the future")
		}
	}
	return nil
}

func AddRedemptionBatch(batch *RedemptionBatch) (bool, error) {
	if err := batch.Validate(time.Now().UTC()); err != nil {
		return false, err
	}
	if batch.State == "" {
		batch.State = RedemptionBatchActive
	}
	if batch.CreatedTime == "" {
		batch.CreatedTime = util.GetCurrentTime()
	}
	affected, err := ormer.Engine.Insert(batch)
	return affected != 0, err
}

func GetRedemptionBatches(owner string) ([]*RedemptionBatch, error) {
	rows := []*RedemptionBatch{}
	err := ormer.Engine.Where("owner = ?", owner).Desc("created_time").Find(&rows)
	return rows, err
}

func GetRedemptionBatch(id string) (*RedemptionBatch, error) {
	owner, name, err := util.GetOwnerAndNameFromIdWithError(id)
	if err != nil {
		return nil, err
	}
	row := &RedemptionBatch{Owner: owner, Name: name}
	found, err := ormer.Engine.Get(row)
	if err != nil || !found {
		return nil, err
	}
	return row, nil
}

func DisableRedemptionBatch(owner, name string) (bool, error) {
	affected, err := ormer.Engine.ID(core.PK{owner, name}).Cols("state").Update(&RedemptionBatch{State: RedemptionBatchDisabled})
	return affected != 0, err
}
