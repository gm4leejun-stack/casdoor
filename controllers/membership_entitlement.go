package controllers

import (
	"os"
	"strings"
	"time"

	"github.com/casdoor/casdoor/object"
	"github.com/casdoor/casdoor/util"
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
	userID := c.GetSessionUsername()
	if owner == "" || userID == "" {
		c.ResponseError(c.T("auth:Unauthorized operation"))
		return
	}
	userOwner, userName := owner, userID
	if strings.Contains(userID, "/") {
		userOwner, userName = util.GetOwnerAndNameFromIdNoCheck(userID)
	}
	if userOwner != owner {
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

func (c *ApiController) GetMembershipEntitlement() {
	owner, userName, plan := c.Ctx.Input.Query("owner"), c.Ctx.Input.Query("user"), c.Ctx.Input.Query("plan")
	if owner == "" || userName == "" || plan == "" {
		c.ResponseError("owner, user, and plan are required")
		return
	}
	// The service client is deliberately scoped to one configured owner/plan.
	allowedOwner := os.Getenv("CASDOOR_MEMBERSHIP_SERVICE_OWNER")
	allowedPlan := os.Getenv("CASDOOR_MEMBERSHIP_SERVICE_PLAN")
	serviceClientID := os.Getenv("CASDOOR_MEMBERSHIP_SERVICE_CLIENT_ID")
	app := c.GetSessionApplication()
	if serviceClientID == "" || app == nil || app.ClientId != serviceClientID || owner != allowedOwner || plan != allowedPlan {
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
