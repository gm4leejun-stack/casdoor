package object

import (
	"errors"
	"time"

	"github.com/xorm-io/core"
	"github.com/xorm-io/xorm"
)

func rejectManagedMembership(s *xorm.Session, sub *Subscription) error {
	if sub == nil {
		return nil
	}
	if sub.Owner != SmyMembershipOwner || sub.Plan != SmyMembershipPlan {
		return nil
	}
	customer := new(SmyMembershipCustomer)
	customer.UserName = sub.User
	found, err := s.Get(customer)
	if err != nil {
		return err
	}
	if found {
		operations, err := s.Where("user_id = ?", customer.UserID).Count(new(SmyMembershipOperation))
		if err != nil {
			return err
		}
		if customer.Sequence > 0 || operations > 0 {
			return ErrManagedMembership
		}
		orders, err := s.Where("user_id = ?", customer.UserID).Count(new(SmyMembershipOrderRecord))
		if err != nil {
			return err
		}
		if customer.Sequence != 0 || orders != 0 {
			return errors.New("客户会员操作档状态异常，禁止直接修改订阅")
		}
	}
	return nil
}

func lockMembershipWrite(s *xorm.Session, owner string) error {
	if owner != SmyMembershipOwner {
		return nil
	}
	if ormer.Engine.DriverName() != "postgres" && ormer.Engine.DriverName() != "mysql" {
		// Smy writes fail closed on these drivers, so no concurrent enrollment exists.
		return nil
	}
	_, err := lockSmyOrganization(s)
	return err
}

// Refresh only state, never overwrite a concurrent grant with a stale object.
func refreshSubscriptionState(sub *Subscription) error {
	if sub.Owner != SmyMembershipOwner || (ormer.Engine.DriverName() != "postgres" && ormer.Engine.DriverName() != "mysql") {
		_, err := UpdateSubscription(sub.GetId(), sub)
		return err
	}
	s := ormer.Engine.NewSession()
	defer s.Close()
	if err := s.Begin(); err != nil {
		return err
	}
	defer s.Rollback()
	if err := lockMembershipWrite(s, sub.Owner); err != nil {
		return err
	}
	current := new(Subscription)
	found, err := s.ID(core.PK{sub.Owner, sub.Name}).ForUpdate().Get(current)
	if err != nil || !found {
		return err
	}
	if current.State == SubStateActive || current.State == SubStateUpcoming || current.State == SubStateExpired {
		current.State = smyState(current.StartTime, current.EndTime, time.Now().UTC())
	} else {
		if err = rejectManagedMembership(s, current); err != nil {
			return err
		}
		current.State, current.Description = sub.State, sub.Description
	}
	if _, err = s.ID(core.PK{current.Owner, current.Name}).Cols("state", "description").Update(current); err != nil {
		return err
	}
	if err = s.Commit(); err != nil {
		return err
	}
	*sub = *current
	return nil
}
