package object

// Membership persistence uses one transaction per notification.

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/casdoor/casdoor/util"
	"github.com/xorm-io/core"
	"github.com/xorm-io/xorm"
)

type SmyMembershipCustomer struct {
	UserID   string `xorm:"'user_id' varchar(100) pk" json:"userId"`
	UserName string `xorm:"varchar(255) notnull unique" json:"userName"`
	Mobile   string `xorm:"varchar(100) notnull" json:"mobile"`
	UniqueID string `xorm:"'unique_id' varchar(100) notnull unique" json:"uniqueId"`
	Sequence int64  `json:"sequence"`
}

type SmyMembershipOrderRecord struct {
	OrderNo string                  `xorm:"varchar(100) pk" json:"orderNo"`
	UserID  string                  `xorm:"'user_id' varchar(100) notnull index" json:"userId"`
	State   SmyMembershipOrderState `xorm:"json" json:"state"`
}

// Append-only business operations retain full schedules, not technical logs.
type SmyMembershipOperation struct {
	UserID      string             `xorm:"'user_id' varchar(100) pk" json:"userId"`
	Sequence    int64              `xorm:"pk" json:"sequence"`
	CreatedTime string             `xorm:"varchar(100)" json:"createdTime"`
	RequestID   string             `xorm:"'request_id' varchar(100)" json:"requestId"`
	OrderNo     string             `xorm:"varchar(100) index" json:"orderNo"`
	Action      string             `xorm:"varchar(30)" json:"action"`
	Reason      string             `xorm:"text" json:"reason"`
	Input       SmyMembershipInput `xorm:"json" json:"input"`
	Before      MembershipSnapshot `xorm:"json" json:"before"`
	After       MembershipSnapshot `xorm:"json" json:"after"`
}

type SmyMembershipHistory struct {
	Candidates            []SmyMembershipCustomer    `json:"candidates,omitempty"`
	MembershipInitialized bool                       `json:"membershipInitialized"`
	Customer              SmyMembershipCustomer      `json:"customer"`
	Subscription          *Subscription              `json:"subscription"`
	Orders                []*SmyMembershipOrderState `json:"orders"`
	Operations            []SmyMembershipOperation   `json:"operations"`
	ConsistencyError      string                     `json:"consistencyError,omitempty"`
}

var ErrManagedMembership = errors.New("会员已由操作档托管，禁止绕过操作档修改、删除或新增同套餐订阅")

const SmyMembershipOwner = "beibei"
const SmyMembershipPlan = "beibei-pro"

// Organization locking also serializes first-time user/binding creation. No
// process-local mutex is involved, so separate Casdoor instances share the lock.
func lockSmyOrganization(session *xorm.Session) (*Organization, error) {
	switch ormer.Engine.DriverName() {
	case "postgres", "mysql":
	default:
		return nil, fmt.Errorf("会员事务只支持 PostgreSQL/MySQL，当前数据库 %s", ormer.Engine.DriverName())
	}
	organizations := []*Organization{}
	if err := session.Where("name = ?", SmyMembershipOwner).ForUpdate().Find(&organizations); err != nil {
		return nil, err
	}
	if len(organizations) != 1 {
		return nil, fmt.Errorf("组织 beibei 必须存在且唯一，实际 %d 条", len(organizations))
	}
	return organizations[0], nil
}

// LockSmyOrganization serializes identity and membership writes across instances.
func LockSmyOrganization(session *xorm.Session) (*Organization, error) {
	return lockSmyOrganization(session)
}

func smyState(start, end string, now time.Time) SubscriptionState {
	startsAt, startErr := smyTime(start)
	expiresAt, endErr := smyTime(end)
	if startErr != nil || endErr != nil || expiresAt.Before(startsAt) {
		return SubStateError
	}
	if !now.Before(expiresAt) {
		return SubStateExpired
	}
	if now.Before(startsAt) {
		return SubStateUpcoming
	}
	return SubStateActive
}

func smyLockBoundUser(session *xorm.Session, customer *SmyMembershipCustomer) (*User, error) {
	users := []*User{}
	if err := session.Where("owner = ? AND id = ?", SmyMembershipOwner, customer.UserID).ForUpdate().Find(&users); err != nil {
		return nil, err
	}
	if len(users) != 1 {
		return nil, fmt.Errorf("萨摩耶绑定的 Casdoor 用户不存在或 ID 不唯一，实际 %d 条", len(users))
	}
	user := users[0]
	if user.Id == "" || user.Name != customer.UserName || user.IsDeleted || user.IsForbidden {
		return nil, errors.New("萨摩耶绑定与账号不一致，或账号已删除/禁用")
	}
	return user, nil
}

func smyCreateUser(session *xorm.Session, org *Organization, now time.Time) (*User, error) {
	id := util.GenerateId()
	user := &User{Owner: SmyMembershipOwner, Name: "smy_" + id, Id: id, Type: "normal-user", DisplayName: "萨摩耶用户", CreatedTime: smyFormat(now), UpdatedTime: smyFormat(now), Avatar: org.DefaultAvatar, BalanceCurrency: org.BalanceCurrency, SignupApplication: org.DefaultApplication, RegisterSource: "Samoyed", PasswordType: org.PasswordType}
	if user.BalanceCurrency == "" {
		user.BalanceCurrency = "USD"
	}
	// Deliberately no default password, elevated user type, or verified phone.
	if err := user.UpdateUserHash(); err != nil {
		return nil, err
	}
	user.PreHash = user.Hash
	if _, err := session.Insert(user); err != nil {
		return nil, err
	}
	return user, nil
}

// ResolveSmyIdentity must run inside the caller's transaction. It does not inspect
// phone numbers or membership, and new bindings have no membership operations.
func ResolveSmyIdentity(session *xorm.Session, uniqueID string, now time.Time) (*User, *SmyMembershipCustomer, error) {
	if uniqueID == "" || len(uniqueID) > 100 || strings.TrimSpace(uniqueID) != uniqueID {
		return nil, nil, errors.New("uniqueId 必须非空且不超过100字节")
	}
	org, err := lockSmyOrganization(session)
	if err != nil {
		return nil, nil, err
	}
	customers := []SmyMembershipCustomer{}
	if err := session.Where("unique_id = ?", uniqueID).ForUpdate().Find(&customers); err != nil {
		return nil, nil, err
	}
	if len(customers) > 1 {
		return nil, nil, errors.New("uniqueId 对应多个客户，停止处理")
	}
	if len(customers) == 1 {
		customer := &customers[0]
		user, err := smyLockBoundUser(session, customer)
		return user, customer, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	user, err := smyCreateUser(session, org, now)
	if err != nil {
		return nil, nil, err
	}
	customer := &SmyMembershipCustomer{UserID: user.Id, UserName: user.Name, UniqueID: uniqueID}
	if _, err := session.Insert(customer); err != nil {
		return nil, nil, err
	}
	return user, customer, nil
}

func smyLoadSubscriptions(session *xorm.Session, userName string) ([]*Subscription, error) {
	subscriptions := []*Subscription{}
	err := session.Where("owner = ? AND "+ormer.Engine.Quote("user")+" = ? AND plan = ?", SmyMembershipOwner, userName, SmyMembershipPlan).ForUpdate().Find(&subscriptions)
	return subscriptions, err
}

func smyLoadOperations(session *xorm.Session, userID string) ([]SmyMembershipOperation, error) {
	operations := []SmyMembershipOperation{}
	err := session.Where("user_id = ?", userID).Asc("sequence").Find(&operations)
	return operations, err
}

func smySnapshotsEqual(left, right MembershipSnapshot) bool {
	if len(left.Orders) == 0 && len(right.Orders) == 0 {
		left.Orders = nil
		right.Orders = nil
	}
	return reflect.DeepEqual(left, right)
}

// Recompute each operation from its immutable input/time, rather than trusting
// an arbitrary stored After snapshot. Every transition must exactly match.
func smyReplay(customer *SmyMembershipCustomer, operations []SmyMembershipOperation) (MembershipSnapshot, error) {
	snapshot := MembershipSnapshot{}
	if len(operations) == 0 || customer.Sequence != int64(len(operations)) {
		return snapshot, errors.New("操作档缺失或客户操作序号不一致")
	}
	var previousTime time.Time
	for i, operation := range operations {
		if operation.UserID != customer.UserID || operation.Sequence != int64(i+1) {
			return snapshot, fmt.Errorf("操作档序号 %d 不连续或客户不匹配", i+1)
		}
		at, err := smyTime(operation.CreatedTime)
		if err != nil {
			return snapshot, err
		}
		if !previousTime.IsZero() && at.Before(previousTime) {
			return snapshot, fmt.Errorf("操作档序号 %d 处理时间倒退", operation.Sequence)
		}
		previousTime = at
		if !smySnapshotsEqual(snapshot, operation.Before) {
			return snapshot, fmt.Errorf("操作档序号 %d 前态不衔接", operation.Sequence)
		}
		if i == 0 {
			if operation.Action != "initialize" || operation.OrderNo != "" || len(operation.After.Orders) != 0 || operation.After.SubscriptionName == "" || operation.After.StartsAt != operation.After.BaseStartsAt || operation.After.ExpiresAt != operation.After.BaseExpiresAt {
				return snapshot, errors.New("原有会员初始化记录无效")
			}
			if err := ValidateSmyMembershipSnapshot(operation.After); err != nil {
				return snapshot, err
			}
			snapshot = operation.After
			continue
		}
		req := operation.Input
		req.Now = at
		if req.UniqueID != customer.UniqueID || req.OrderNo != operation.OrderNo {
			return snapshot, fmt.Errorf("操作档序号 %d 订单身份不匹配", operation.Sequence)
		}
		change, err := ApplySmyMembershipRequest(snapshot, req)
		if err != nil {
			return snapshot, fmt.Errorf("操作档序号 %d 无法重放: %w", operation.Sequence, err)
		}
		if change.Duplicate || change.OperationType != operation.Action || change.Description != operation.Reason || !smySnapshotsEqual(change.Before, operation.Before) || !smySnapshotsEqual(change.After, operation.After) {
			return snapshot, fmt.Errorf("操作档序号 %d 重放结果不一致", operation.Sequence)
		}
		snapshot = change.After
	}
	return snapshot, nil
}

func smySameTime(left, right string) bool {
	if left == "" || right == "" {
		return left == right
	}
	a, aerr := smyTime(left)
	b, berr := smyTime(right)
	return aerr == nil && berr == nil && a.Equal(b)
}

func smyCheckStoredState(session *xorm.Session, customer *SmyMembershipCustomer, snapshot MembershipSnapshot, subscriptions []*Subscription) error {
	if len(subscriptions) > 1 {
		return errors.New("存在多条 beibei-pro 订阅，停止维护")
	}
	if snapshot.StartsAt == "" && snapshot.ExpiresAt == "" {
		if len(subscriptions) != 0 {
			return errors.New("操作档无有效期但数据库存在订阅")
		}
	} else {
		if len(subscriptions) != 1 {
			return errors.New("操作档已有有效期但订阅缺失")
		}
		sub := subscriptions[0]
		if sub.Name != snapshot.SubscriptionName || sub.Group != "" || !smySameTime(sub.StartTime, snapshot.StartsAt) || !smySameTime(sub.EndTime, snapshot.ExpiresAt) {
			return errors.New("操作档总有效期或身份与 Subscription 不一致")
		}
		if sub.State != SubStateActive && sub.State != SubStateUpcoming && sub.State != SubStateExpired {
			return errors.New("订阅状态不是正常时间状态，停止维护")
		}
	}
	rows := []SmyMembershipOrderRecord{}
	if err := session.Where("user_id = ?", customer.UserID).Find(&rows); err != nil {
		return err
	}
	if len(rows) != len(snapshot.Orders) {
		return errors.New("订单表与操作档订单数量不一致")
	}
	expected := map[string]*SmyMembershipOrderState{}
	for _, order := range snapshot.Orders {
		expected[order.OrderNo] = order
	}
	for _, row := range rows {
		order := expected[row.OrderNo]
		if order == nil || !reflect.DeepEqual(*order, row.State) {
			return fmt.Errorf("订单 %s 当前排期与操作档不一致", row.OrderNo)
		}
	}
	return nil
}

func smyInitialize(session *xorm.Session, customer *SmyMembershipCustomer, req SmyMembershipInput, subscriptions []*Subscription) error {
	if len(subscriptions) > 1 {
		return errors.New("存在多条 beibei-pro 订阅，不能初始化")
	}
	snapshot := MembershipSnapshot{SubscriptionName: "smy_" + util.GenerateId(), Orders: []*SmyMembershipOrderState{}}
	if len(subscriptions) == 1 {
		sub := subscriptions[0]
		if sub.Group != "" || (sub.State != SubStateActive && sub.State != SubStateUpcoming && sub.State != SubStateExpired) {
			return errors.New("原有订阅含组权益或非正常状态，不能初始化")
		}
		start, err := smyTime(sub.StartTime)
		if err != nil {
			return err
		}
		end, err := smyTime(sub.EndTime)
		if err != nil {
			return err
		}
		snapshot.SubscriptionName = sub.Name
		snapshot.StartsAt, snapshot.BaseStartsAt = smyFormat(start), smyFormat(start)
		snapshot.ExpiresAt, snapshot.BaseExpiresAt = smyFormat(end), smyFormat(end)
	}
	if err := ValidateSmyMembershipSnapshot(snapshot); err != nil {
		return err
	}
	customer.Sequence = 1
	operation := &SmyMembershipOperation{UserID: customer.UserID, Sequence: 1, CreatedTime: smyFormat(req.Now), RequestID: req.RequestID, Action: "initialize", Reason: "原有会员初始化，保存受保护的原有会员有效期", Before: MembershipSnapshot{}, After: snapshot}
	if _, err := session.Insert(operation); err != nil {
		return err
	}
	_, err := session.ID(customer.UserID).Cols("sequence").Update(customer)
	return err
}

func ProcessSmyMembership(req SmyMembershipInput, beforeCommit func() error) error {
	if err := ValidateSmyMembershipRequest(req); err != nil {
		return err
	}
	session := ormer.Engine.NewSession()
	defer session.Close()
	if err := session.Begin(); err != nil {
		return err
	}
	defer session.Rollback()
	_, err := lockSmyOrganization(session)
	if err != nil {
		return err
	}
	// Use processing time after obtaining locks, not HTTP arrival time.
	if req.Now.IsZero() {
		req.Now = time.Now().UTC()
	} else {
		req.Now = req.Now.UTC()
	}
	plan := &Plan{}
	found, err := session.ID(core.PK{SmyMembershipOwner, SmyMembershipPlan}).Get(plan)
	if err != nil {
		return err
	}
	if !found || !plan.IsEnabled {
		return errors.New("套餐 beibei/beibei-pro 不存在或未启用")
	}
	user, customer, err := ResolveSmyIdentity(session, req.UniqueID, req.Now)
	if err != nil {
		return err
	}
	orderRecord := &SmyMembershipOrderRecord{}
	exists, err := session.ID(req.OrderNo).Get(orderRecord)
	if err != nil {
		return err
	}
	if exists && orderRecord.UserID != customer.UserID {
		return errors.New("订单号已属于其他客户，禁止重新绑定")
	}
	subscriptions, err := smyLoadSubscriptions(session, user.Name)
	if err != nil {
		return err
	}
	operations, err := smyLoadOperations(session, customer.UserID)
	if err != nil {
		return err
	}
	if customer.Sequence == 0 && len(operations) == 0 {
		orders, err := session.Where("user_id = ?", customer.UserID).Count(new(SmyMembershipOrderRecord))
		if err != nil {
			return err
		}
		if orders != 0 {
			return errors.New("客户尚无会员操作档但存在订单，停止维护")
		}
		if err := smyInitialize(session, customer, req, subscriptions); err != nil {
			return err
		}
	}
	operations, err = smyLoadOperations(session, customer.UserID)
	if err != nil {
		return err
	}
	snapshot, err := smyReplay(customer, operations)
	if err != nil {
		return err
	}
	if err := smyCheckStoredState(session, customer, snapshot, subscriptions); err != nil {
		return err
	}
	lastAt, err := smyTime(operations[len(operations)-1].CreatedTime)
	if err != nil {
		return err
	}
	if req.Now.Before(lastAt) {
		return errors.New("处理时间早于最近会员操作，停止维护")
	}
	change, err := ApplySmyMembershipRequest(snapshot, req)
	if err != nil {
		return err
	}
	if !change.Duplicate {
		if err := ValidateSmyMembershipSnapshot(change.After); err != nil {
			return err
		}
		if err := smyPersistChange(session, customer, req, change, subscriptions); err != nil {
			return err
		}
		operations, err = smyLoadOperations(session, customer.UserID)
		if err != nil {
			return err
		}
		replayed, err := smyReplay(customer, operations)
		if err != nil {
			return err
		}
		if !smySnapshotsEqual(replayed, change.After) {
			return errors.New("提交前重放结果不一致")
		}
		subscriptions, err = smyLoadSubscriptions(session, user.Name)
		if err != nil {
			return err
		}
		if err := smyCheckStoredState(session, customer, replayed, subscriptions); err != nil {
			return err
		}
	}
	if beforeCommit != nil {
		if err := beforeCommit(); err != nil {
			return err
		}
	}
	return session.Commit()
}

func smyPersistChange(session *xorm.Session, customer *SmyMembershipCustomer, req SmyMembershipInput, change *SmyMembershipChange, subscriptions []*Subscription) error {
	before := map[string]*SmyMembershipOrderState{}
	for _, order := range change.Before.Orders {
		before[order.OrderNo] = order
	}
	for _, order := range change.After.Orders {
		row := &SmyMembershipOrderRecord{OrderNo: order.OrderNo, UserID: customer.UserID, State: *order}
		if before[order.OrderNo] == nil {
			if _, err := session.Insert(row); err != nil {
				return err
			}
		} else if !reflect.DeepEqual(before[order.OrderNo], order) {
			count, err := session.ID(order.OrderNo).Where("user_id = ?", customer.UserID).Cols("state").Update(row)
			if err != nil {
				return err
			}
			if count != 1 {
				return fmt.Errorf("订单 %s 更新行数不一致", order.OrderNo)
			}
		}
	}
	if change.After.StartsAt != "" {
		var sub *Subscription
		if len(subscriptions) == 0 {
			sub = &Subscription{Owner: SmyMembershipOwner, Name: change.After.SubscriptionName, DisplayName: "会员 - " + customer.UserName, User: customer.UserName, Plan: SmyMembershipPlan, Period: PeriodMonthly, CreatedTime: smyFormat(req.Now)}
		} else {
			sub = subscriptions[0]
		}
		oldStart, oldEnd, oldState := sub.StartTime, sub.EndTime, sub.State
		sub.StartTime, sub.EndTime = change.After.StartsAt, change.After.ExpiresAt
		sub.State = smyState(sub.StartTime, sub.EndTime, req.Now)
		if len(subscriptions) == 0 {
			if _, err := session.Insert(sub); err != nil {
				return err
			}
		} else if oldStart != sub.StartTime || oldEnd != sub.EndTime || oldState != sub.State {
			count, err := session.ID(core.PK{sub.Owner, sub.Name}).Cols("start_time", "end_time", "state").Update(sub)
			if err != nil {
				return err
			}
			if count != 1 {
				return errors.New("订阅更新行数不一致")
			}
		}
	}
	customer.Sequence++
	operation := &SmyMembershipOperation{UserID: customer.UserID, Sequence: customer.Sequence, CreatedTime: smyFormat(req.Now), RequestID: req.RequestID, OrderNo: req.OrderNo, Action: change.OperationType, Reason: change.Description, Input: req, Before: change.Before, After: change.After}
	if _, err := session.Insert(operation); err != nil {
		return err
	}
	count, err := session.ID(customer.UserID).Cols("sequence").Update(customer)
	if err != nil {
		return err
	}
	if count != 1 {
		return errors.New("客户操作序号更新失败")
	}
	return nil
}

func GetSmyMembershipHistory(field, value string) (*SmyMembershipHistory, error) {
	value = strings.TrimSpace(value)
	columns := map[string]string{"mobile": "mobile", "uniqueId": "unique_id", "userId": "user_id", "orderNo": "order_no"}
	column, valid := columns[field]
	if !valid || value == "" || len(value) > 100 {
		return nil, errors.New("请使用完整 uniqueId、历史手机号、用户 ID 或订单号查询")
	}
	session := ormer.Engine.NewSession()
	defer session.Close()
	if err := session.Begin(); err != nil {
		return nil, err
	}
	defer session.Rollback()
	if _, err := lockSmyOrganization(session); err != nil {
		return nil, err
	}
	customer := &SmyMembershipCustomer{}
	if field == "orderNo" {
		order := &SmyMembershipOrderRecord{}
		found, err := session.ID(value).Get(order)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, errors.New("未找到该订单的会员操作档")
		}
		column, value = "user_id", order.UserID
	}
	customers := []SmyMembershipCustomer{}
	var err error
	if field == "mobile" {
		// Contact numbers are historical search clues only. Include every order
		// and operation input so a changed number can still locate the customer.
		err = session.Find(&customers)
		if err == nil {
			inputs := []SmyMembershipOperation{}
			if err := session.Cols("user_id", "input").Find(&inputs); err != nil {
				return nil, err
			}
			matchedIDs := map[string]bool{}
			for _, operation := range inputs {
				if operation.Input.Mobile == value {
					matchedIDs[operation.UserID] = true
				}
			}
			matched := []SmyMembershipCustomer{}
			for _, candidate := range customers {
				if candidate.Mobile == value || matchedIDs[candidate.UserID] {
					matched = append(matched, candidate)
				}
			}
			customers = matched
		}
	} else {
		err = session.Where(column+" = ?", value).Find(&customers)
	}
	if err != nil {
		return nil, err
	}
	if len(customers) == 0 {
		return nil, errors.New("未找到客户会员操作档")
	}
	if len(customers) > 1 {
		if field != "mobile" {
			return nil, errors.New("查询标识对应多个客户，客户绑定异常")
		}
		if err := session.Commit(); err != nil {
			return nil, err
		}
		return &SmyMembershipHistory{Candidates: customers}, nil
	}
	*customer = customers[0]
	user := &User{}
	userFound, err := session.ID(core.PK{SmyMembershipOwner, customer.UserName}).ForUpdate().Get(user)
	if err != nil {
		return nil, err
	}
	operations, err := smyLoadOperations(session, customer.UserID)
	if err != nil {
		return nil, err
	}
	subscriptions, err := smyLoadSubscriptions(session, customer.UserName)
	if err != nil {
		return nil, err
	}
	history := &SmyMembershipHistory{Customer: *customer, MembershipInitialized: customer.Sequence > 0 && len(operations) > 0, Operations: operations, Orders: []*SmyMembershipOrderState{}}
	if len(subscriptions) > 0 {
		history.Subscription = subscriptions[0]
	}
	if len(operations) > 0 {
		history.Orders = operations[len(operations)-1].After.Orders
	}
	// Preserve the actual materialized schedule even when replay is inconsistent.
	rows := []SmyMembershipOrderRecord{}
	if err := session.Where("user_id = ?", customer.UserID).Find(&rows); err != nil {
		return nil, err
	}
	if len(rows) > 0 {
		positions := map[string]int{}
		for index, order := range history.Orders {
			if order != nil {
				positions[order.OrderNo] = index + 1
			}
		}
		sort.Slice(rows, func(i, j int) bool {
			left, right := positions[rows[i].OrderNo], positions[rows[j].OrderNo]
			if left == right {
				return rows[i].OrderNo < rows[j].OrderNo
			}
			return left < right
		})
		history.Orders = []*SmyMembershipOrderState{}
		for index := range rows {
			history.Orders = append(history.Orders, &rows[index].State)
		}
	}
	if !userFound || user.Id != customer.UserID {
		history.ConsistencyError = "客户绑定与 Casdoor 用户不一致"
	}
	var replayErr error
	if customer.Sequence == 0 && len(operations) == 0 && len(rows) == 0 {
		// An identity-only binding is valid and not a managed membership.
	} else {
		var snapshot MembershipSnapshot
		snapshot, replayErr = smyReplay(customer, operations)
		if replayErr == nil {
			replayErr = smyCheckStoredState(session, customer, snapshot, subscriptions)
		}
	}
	if replayErr != nil {
		if history.ConsistencyError != "" {
			history.ConsistencyError += "；"
		}
		history.ConsistencyError += replayErr.Error()
	}
	if err := session.Commit(); err != nil {
		return nil, err
	}
	return history, nil
}
