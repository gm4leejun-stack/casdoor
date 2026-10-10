package object

type RedemptionAudit struct {
	Id             int    `xorm:"pk autoincr" json:"id"`
	Owner          string `xorm:"varchar(100) notnull index" json:"owner"`
	CodeID         string `xorm:"varchar(200) notnull index" json:"codeId"`
	BatchID        string `xorm:"varchar(200) notnull" json:"batchId"`
	User           string `xorm:"varchar(100) notnull index" json:"user"`
	SubscriptionID string `xorm:"varchar(200)" json:"subscriptionId"`
	Plan           string `xorm:"varchar(100) notnull" json:"plan"`
	OldEndTime     string `xorm:"varchar(100)" json:"oldEndTime"`
	NewEndTime     string `xorm:"varchar(100) notnull" json:"newEndTime"`
	DurationDays   int    `xorm:"notnull" json:"durationDays"`
	RequestID      string `xorm:"varchar(200)" json:"requestId"`
	CreatedTime    string `xorm:"varchar(100)" json:"createdTime"`
	Result         string `xorm:"varchar(30) notnull" json:"result"`
}

func GetRedemptionAudits(owner, user string) ([]*RedemptionAudit, error) {
	rows := []*RedemptionAudit{}
	session := ormer.Engine.Where("owner = ?", owner).Desc("created_time")
	if user != "" {
		session = session.And("user = ?", user)
	}
	err := session.Find(&rows)
	return rows, err
}
