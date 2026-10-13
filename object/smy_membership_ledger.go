package object

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var smyMobilePattern = regexp.MustCompile(`^1[3-9][0-9]{9}$`)
var smyBeijing = time.FixedZone("Asia/Shanghai", 8*60*60)

func AddSmyCalendarMonth(value time.Time) time.Time {
	return addSmyCalendarMonths(value, 1)
}

// Add the full order period to the starting month, clamping only in the target
// month (January 31 + 3 months = April 30, not April 28).
func addSmyCalendarMonths(value time.Time, months int) time.Time {
	local := value.In(smyBeijing)
	lastDay := time.Date(local.Year(), local.Month()+time.Month(months)+1, 0, 0, 0, 0, 0, smyBeijing).Day()
	day := local.Day()
	if day > lastDay {
		day = lastDay
	}
	return time.Date(local.Year(), local.Month()+time.Month(months), day, local.Hour(), local.Minute(), local.Second(), local.Nanosecond(), smyBeijing).UTC()
}

func smyPeriodMonths(period string) (int, error) {
	switch period {
	case "1", "3", "6", "12":
		return strconv.Atoi(period)
	default:
		return 0, fmt.Errorf("periodTime 仅支持1、3、6、12个自然月")
	}
}

func smyTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("无效会员时间%q: %w", value, err)
	}
	return parsed, nil
}

func smyFormat(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }

const (
	SmyOrderActive   = "2"
	SmyOrderRefunded = "6"
)

type SmyMembershipInput struct {
	OrderNo     string    `json:"orderNo"`
	ProductType string    `json:"productType"`
	ProductCode string    `json:"productCode"`
	UniqueID    string    `json:"uniqueId"`
	PeriodTime  string    `json:"periodTime"`
	Mobile      string    `json:"mobile"`
	OrderStatus string    `json:"orderStatus"`
	RequestID   string    `json:"-"`
	Now         time.Time `json:"-"`
}

// Orders stay in grant order. Original dates never change.
type SmyMembershipOrderState struct {
	OrderNo           string `json:"orderNo"`
	UniqueID          string `json:"uniqueId"`
	Mobile            string `json:"mobile"`
	ProductType       string `json:"productType"`
	ProductCode       string `json:"productCode"`
	PeriodTime        string `json:"periodTime"`
	Status            string `json:"status"`
	Granted           bool   `json:"granted"`
	OpenedAt          string `json:"openedAt,omitempty"`
	RefundAt          string `json:"refundAt,omitempty"`
	OriginalStartsAt  string `json:"originalStartsAt,omitempty"`
	OriginalExpiresAt string `json:"originalExpiresAt,omitempty"`
	StartsAt          string `json:"startsAt,omitempty"`
	ExpiresAt         string `json:"expiresAt,omitempty"`
}

type MembershipSnapshot struct {
	SubscriptionName string                     `json:"subscriptionName"`
	StartsAt         string                     `json:"startsAt"`
	ExpiresAt        string                     `json:"expiresAt"`
	BaseStartsAt     string                     `json:"baseStartsAt"`
	BaseExpiresAt    string                     `json:"baseExpiresAt"`
	Orders           []*SmyMembershipOrderState `json:"orders"`
}

type SmyMembershipOrderChange struct {
	OrderNo string                   `json:"orderNo"`
	Before  *SmyMembershipOrderState `json:"before,omitempty"`
	After   *SmyMembershipOrderState `json:"after"`
}

type SmyMembershipChange struct {
	Before         MembershipSnapshot         `json:"before"`
	After          MembershipSnapshot         `json:"after"`
	OperationType  string                     `json:"operationType"`
	Description    string                     `json:"description"`
	AffectedOrders []SmyMembershipOrderChange `json:"affectedOrders"`
	Duplicate      bool                       `json:"duplicate"`
}

func ValidateSmyMembershipRequest(req SmyMembershipInput) error {
	for key, value := range map[string]string{"orderNo": req.OrderNo, "uniqueId": req.UniqueID, "productType": req.ProductType, "productCode": req.ProductCode} {
		if value == "" || len(value) > 100 || strings.TrimSpace(value) != value {
			return fmt.Errorf("%s 必须非空且不超过100字节", key)
		}
	}
	if !smyMobilePattern.MatchString(req.Mobile) {
		return fmt.Errorf("mobile 必须为中国大陆手机号")
	}
	if req.OrderStatus != SmyOrderActive && req.OrderStatus != SmyOrderRefunded {
		return fmt.Errorf("orderStatus 仅支持2开通或6退款")
	}
	// Refund duration comes from the saved grant, never the notification.
	if req.OrderStatus == SmyOrderActive {
		if _, err := smyPeriodMonths(req.PeriodTime); err != nil {
			return err
		}
	}
	return nil
}

func cloneSmySnapshot(snapshot MembershipSnapshot) MembershipSnapshot {
	data, _ := json.Marshal(snapshot)
	var result MembershipSnapshot
	_ = json.Unmarshal(data, &result)
	return result
}

func ValidateSmyMembershipSnapshot(s MembershipSnapshot) error {
	if s.SubscriptionName == "" {
		return fmt.Errorf("会员订阅标识缺失")
	}
	for _, pair := range [][2]string{{s.StartsAt, s.ExpiresAt}, {s.BaseStartsAt, s.BaseExpiresAt}} {
		if pair[0] == "" && pair[1] == "" {
			continue
		}
		start, err := smyTime(pair[0])
		if err != nil {
			return err
		}
		end, err := smyTime(pair[1])
		if err != nil {
			return err
		}
		if end.Before(start) {
			return fmt.Errorf("会员结束时间早于开始时间")
		}
	}
	seen := map[string]bool{}
	var identityMobile, identityUnique string
	var priorEnd time.Time
	if s.BaseExpiresAt != "" {
		priorEnd, _ = smyTime(s.BaseExpiresAt)
	}
	for _, order := range s.Orders {
		if order == nil || seen[order.OrderNo] {
			return fmt.Errorf("订单为空或重复")
		}
		seen[order.OrderNo] = true
		if identityMobile == "" {
			identityMobile, identityUnique = order.Mobile, order.UniqueID
		} else if identityMobile != order.Mobile || identityUnique != order.UniqueID {
			return fmt.Errorf("操作档订单客户身份不一致")
		}
		req := SmyMembershipInput{OrderNo: order.OrderNo, UniqueID: order.UniqueID, Mobile: order.Mobile, ProductType: order.ProductType, ProductCode: order.ProductCode, PeriodTime: order.PeriodTime, OrderStatus: order.Status}
		if err := ValidateSmyMembershipRequest(req); err != nil {
			return err
		}
		if order.Status == SmyOrderRefunded {
			if _, err := smyTime(order.RefundAt); err != nil {
				return err
			}
		} else if order.RefundAt != "" || !order.Granted {
			return fmt.Errorf("订单 %s 状态与发放信息不符", order.OrderNo)
		}
		if !order.Granted {
			if order.Status != SmyOrderRefunded || order.StartsAt != "" || order.ExpiresAt != "" || order.OpenedAt != "" || order.OriginalStartsAt != "" || order.OriginalExpiresAt != "" {
				return fmt.Errorf("未发放订单 %s 不能有排期", order.OrderNo)
			}
			continue
		}
		opened, err := smyTime(order.OpenedAt)
		if err != nil {
			return err
		}
		originalStart, err := smyTime(order.OriginalStartsAt)
		if err != nil {
			return err
		}
		originalEnd, err := smyTime(order.OriginalExpiresAt)
		if err != nil {
			return err
		}
		months, err := smyPeriodMonths(order.PeriodTime)
		if err != nil {
			return fmt.Errorf("订单 %s 原发放周期无效: %w", order.OrderNo, err)
		}
		if originalStart.Before(opened) || !addSmyCalendarMonths(originalStart, months).Equal(originalEnd) {
			return fmt.Errorf("订单 %s 原始排期无效", order.OrderNo)
		}
		start, err := smyTime(order.StartsAt)
		if err != nil {
			return err
		}
		end, err := smyTime(order.ExpiresAt)
		if err != nil {
			return err
		}
		if end.Before(start) {
			return fmt.Errorf("订单 %s 排期倒退", order.OrderNo)
		}
		if order.Status == SmyOrderRefunded && end.After(start) && end.After(priorEnd) {
			priorEnd = end
		}
		if order.Status == SmyOrderRefunded {
			refunded, _ := smyTime(order.RefundAt)
			if refunded.Before(opened) || end.After(originalEnd) || (end.After(start) && end.After(refunded)) {
				return fmt.Errorf("订单 %s 退款状态与保留时段不一致", order.OrderNo)
			}
		}
		if order.Status == SmyOrderActive {
			if !addSmyCalendarMonths(start, months).Equal(end) || (!priorEnd.IsZero() && start.Before(priorEnd)) {
				return fmt.Errorf("订单 %s 月排期或衔接无效", order.OrderNo)
			}
			if start.After(priorEnd) && !start.Equal(opened) {
				return fmt.Errorf("订单 %s 与前订单间存在非开通形成的空档", order.OrderNo)
			}
			priorEnd = end
		}
	}
	if s.StartsAt != "" {
		start, _ := smyTime(s.StartsAt)
		if priorEnd.Before(start) {
			priorEnd = start
		}
	}
	if !priorEnd.IsZero() {
		totalEnd, err := smyTime(s.ExpiresAt)
		if err != nil {
			return err
		}
		if !totalEnd.Equal(priorEnd) {
			return fmt.Errorf("未退款订单及原有会员与总到期时间不一致")
		}
	}
	return nil
}

func ApplySmyMembershipRequest(snapshot MembershipSnapshot, req SmyMembershipInput) (*SmyMembershipChange, error) {
	if err := ValidateSmyMembershipRequest(req); err != nil {
		return nil, err
	}
	if err := ValidateSmyMembershipSnapshot(snapshot); err != nil {
		return nil, err
	}
	if req.Now.IsZero() {
		return nil, fmt.Errorf("会员处理时间缺失")
	}
	next := cloneSmySnapshot(snapshot)
	change := &SmyMembershipChange{Before: cloneSmySnapshot(snapshot), After: next}
	found := -1
	for i, order := range next.Orders {
		if order.OrderNo == req.OrderNo {
			found = i
			if order.Mobile != req.Mobile || order.UniqueID != req.UniqueID || order.ProductType != req.ProductType || order.ProductCode != req.ProductCode {
				return nil, fmt.Errorf("订单 %s 通知参数与原订单不一致", req.OrderNo)
			}
			if req.OrderStatus == SmyOrderActive && order.Granted && order.PeriodTime != req.PeriodTime {
				return nil, fmt.Errorf("订单 %s 开通周期与原发放订单不一致", req.OrderNo)
			}
			if order.Status == SmyOrderRefunded || req.OrderStatus == SmyOrderActive {
				change.Duplicate = true
				return change, nil
			}
			break
		}
	}
	now := req.Now.UTC()
	if req.OrderStatus == SmyOrderActive {
		start := now
		if next.ExpiresAt != "" {
			end, _ := smyTime(next.ExpiresAt)
			if end.After(now) {
				start = end
			}
		}
		if next.StartsAt == "" || start.Equal(now) {
			next.StartsAt = smyFormat(now)
		}
		months, _ := smyPeriodMonths(req.PeriodTime) // validated above
		end := addSmyCalendarMonths(start, months)
		next.ExpiresAt = smyFormat(end)
		next.Orders = append(next.Orders, &SmyMembershipOrderState{OrderNo: req.OrderNo, UniqueID: req.UniqueID, Mobile: req.Mobile, ProductType: req.ProductType, ProductCode: req.ProductCode, PeriodTime: req.PeriodTime, Status: SmyOrderActive, Granted: true, OpenedAt: smyFormat(now), OriginalStartsAt: smyFormat(start), OriginalExpiresAt: smyFormat(end), StartsAt: smyFormat(start), ExpiresAt: smyFormat(end)})
		change.OperationType, change.Description = "grant", "订单开通，顺延一个自然月"
		if months != 1 {
			change.Description = fmt.Sprintf("订单开通，顺延%d个自然月", months)
		}
	} else if found == -1 {
		// A refund-first tombstone has no grant duration. The raw periodTime is
		// retained only in the operation input for customer service.
		next.Orders = append(next.Orders, &SmyMembershipOrderState{OrderNo: req.OrderNo, UniqueID: req.UniqueID, Mobile: req.Mobile, ProductType: req.ProductType, ProductCode: req.ProductCode, Status: SmyOrderRefunded, RefundAt: smyFormat(now)})
		change.OperationType, change.Description = "refund", "退款先到，记录终态，不发放会员"
	} else {
		target := next.Orders[found]
		start, _ := smyTime(target.StartsAt)
		end, _ := smyTime(target.ExpiresAt)
		target.Status, target.RefundAt = SmyOrderRefunded, smyFormat(now)
		change.OperationType = "refund"
		if !now.Before(end) {
			change.Description = "订单已结束，记录退款，不追扣其他会员"
		} else {
			cursor := start
			if now.Before(start) || now.Equal(start) {
				target.ExpiresAt = target.StartsAt
				change.Description = "取消未开始订单，后续订单按自然月前移"
			} else {
				cursor = now
				target.ExpiresAt = smyFormat(now)
				change.Description = "终止生效中订单，保留已用时间，后续订单按自然月重排"
			}
			for _, later := range next.Orders[found+1:] {
				if later.Status != SmyOrderActive {
					continue
				}
				// Only future orders may move; consumed history cannot be reclaimed.
				laterStart, _ := smyTime(later.StartsAt)
				if laterStart.Before(now) {
					return nil, fmt.Errorf("后续订单 %s 已开始，排期不一致", later.OrderNo)
				}
				later.StartsAt = smyFormat(cursor)
				months, _ := smyPeriodMonths(later.PeriodTime) // snapshot validated above
				cursor = addSmyCalendarMonths(cursor, months)
				later.ExpiresAt = smyFormat(cursor)
			}
			next.ExpiresAt = smyFormat(cursor)
		}
	}
	change.After = next
	if err := ValidateSmyMembershipSnapshot(next); err != nil {
		return nil, err
	}
	before := map[string]*SmyMembershipOrderState{}
	for _, order := range snapshot.Orders {
		before[order.OrderNo] = order
	}
	for _, order := range next.Orders {
		if !reflect.DeepEqual(before[order.OrderNo], order) {
			change.AffectedOrders = append(change.AffectedOrders, SmyMembershipOrderChange{OrderNo: order.OrderNo, Before: before[order.OrderNo], After: order})
		}
	}
	return change, nil
}
