package controllers

import (
	"errors"
	"os"
	"strings"
	"time"

	"github.com/casdoor/casdoor/conf"
	"github.com/casdoor/casdoor/object"
)

var errMembershipUserNotFound = errors.New("membership user not found")

type membershipUserLookup func(owner, userID string) (*object.User, error)
type membershipLookup func(owner, userName, plan string) (*object.MembershipEntitlement, error)

func canonicalIssuer(value string) string {
	return strings.TrimRight(strings.TrimSpace(value), "/")
}

func validateMembershipSubject(owner, plan, issuer, userID, configuredIssuer string) bool {
	return owner != "" && plan != "" && userID != "" && canonicalIssuer(issuer) != "" &&
		canonicalIssuer(issuer) == canonicalIssuer(configuredIssuer)
}

func membershipEntitlementForSubject(owner, issuer, userID, plan string, getUser membershipUserLookup, getEntitlement membershipLookup) (*object.MembershipEntitlement, error) {
	user, err := getUser(owner, userID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errMembershipUserNotFound
	}
	result, err := getEntitlement(owner, user.Name, plan)
	if err != nil {
		return nil, err
	}
	result.Subject = object.MembershipSubject{Issuer: canonicalIssuer(issuer), UserID: userID}
	return result, nil
}

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
	result.Subject = object.MembershipSubject{Issuer: canonicalIssuer(conf.GetConfigString("origin")), UserID: currentUser.Id}
	c.ResponseOk(result)
}

func (c *ApiController) GetMembershipEntitlement() {
	owner, plan := c.Ctx.Input.Query("owner"), c.Ctx.Input.Query("plan")
	issuer, userID := c.Ctx.Input.Query("issuer"), c.Ctx.Input.Query("userId")
	if !validateMembershipSubject(owner, plan, issuer, userID, conf.GetConfigString("origin")) {
		c.ResponseError("owner, plan, issuer, and userId are required and issuer must match this BeiBei AI instance")
		return
	}
	// Gundam uses a dedicated static token for this internal read-only API.
	// ApiFilter validates the same header before allowing the request through.
	expectedToken := os.Getenv("CASDOOR_MEMBERSHIP_SERVICE_TOKEN")
	if expectedToken == "" || c.Ctx.Input.Header("X-Membership-Service-Token") != expectedToken {
		c.ResponseError(c.T("auth:Unauthorized operation"))
		return
	}
	result, err := membershipEntitlementForSubject(owner, issuer, userID, plan, object.GetUserByUserId, membershipEntitlement)
	if err != nil {
		c.ResponseError("membership entitlement unavailable")
		return
	}
	c.ResponseOk(result)
}
