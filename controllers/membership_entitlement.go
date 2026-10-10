package controllers

import (
	"os"
	"time"

	"github.com/casdoor/casdoor/object"
)

func membershipEntitlement(owner, userName, plan string) (*object.MembershipEntitlement, error) {
	subs, err := object.GetSubscriptionsByUser(owner, userName)
	if err != nil {
		return nil, err
	}
	var match *object.Subscription
	for _, sub := range subs {
		if sub.User == userName && (plan == "" || sub.Plan == plan) {
			if match != nil {
				return nil, os.ErrExist
			}
			match = sub
		}
	}
	result := object.EvaluateMembership(match, time.Now().UTC())
	result.Subject = object.MembershipSubject{Owner: owner, UserName: userName}
	result.Plan = plan
	if match != nil {
		result.Plan = match.Plan
	}
	return &result, nil
}

func (c *ApiController) GetMyMembershipEntitlement() {
	owner := c.Ctx.Input.Query("owner")
	plan := c.Ctx.Input.Query("plan")
	currentUser := c.getCurrentUser()
	if owner == "" || currentUser == nil || currentUser.Owner != owner {
		c.ResponseError(c.T("auth:Unauthorized operation"))
		return
	}
	result, err := membershipEntitlement(owner, currentUser.Name, plan)
	if err != nil {
		c.ResponseError("membership entitlement unavailable")
		return
	}
	c.ResponseOk(result)
}

func (c *ApiController) GetMembershipEntitlement() {
	owner, userName, plan := c.Ctx.Input.Query("owner"), c.Ctx.Input.Query("user"), c.Ctx.Input.Query("plan")
	if owner == "" || userName == "" || plan == "" {
		c.ResponseError("owner, user, and plan are required")
		return
	}
	// Gundam uses a dedicated static token for this internal read-only API.
	// ApiFilter validates the same header before allowing the request through.
	expectedToken := os.Getenv("CASDOOR_MEMBERSHIP_SERVICE_TOKEN")
	if expectedToken == "" || c.Ctx.Input.Header("X-Membership-Service-Token") != expectedToken {
		c.ResponseError(c.T("auth:Unauthorized operation"))
		return
	}
	result, err := membershipEntitlement(owner, userName, plan)
	if err != nil {
		c.ResponseError("membership entitlement unavailable")
		return
	}
	c.ResponseOk(result)
}
