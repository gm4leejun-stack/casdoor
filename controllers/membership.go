package controllers

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/casdoor/casdoor/object"
)

func (c *ApiController) MembershipRedeem() {
	if c.GetSessionUsername() == "" {
		c.ResponseError("unauthorized")
		return
	}
	owner, user, ok := strings.Cut(c.GetSessionUsername(), "/")
	if !ok {
		c.ResponseError("invalid user")
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	if json.Unmarshal(c.Ctx.Input.RequestBody, &body) != nil {
		c.ResponseError("invalid request")
		return
	}
	sub, err := object.RedeemMembershipCode(owner, user, body.Code)
	if err != nil {
		c.ResponseError("membership code is invalid or used")
		return
	}
	c.ResponseOk(map[string]interface{}{"plan": sub.Plan, "startsAt": sub.StartTime, "expiresAt": sub.EndTime})
}

func (c *ApiController) MembershipGenerate() {
	if !c.IsAdmin() {
		c.ResponseError("unauthorized")
		return
	}
	var body struct {
		Owner, Plan            string
		DurationDays, Quantity int
	}
	if json.Unmarshal(c.Ctx.Input.RequestBody, &body) != nil {
		c.ResponseError("invalid request")
		return
	}
	if !c.IsAdminOfOrganization(body.Owner) {
		c.ResponseError("unauthorized")
		return
	}
	codes, err := object.GenerateMembershipCodes(body.Owner, body.Plan, body.DurationDays, body.Quantity)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk(map[string]interface{}{"codes": codes})
}

func (c *ApiController) MembershipEntitlement() {
	auth := c.Ctx.Input.Header("Authorization")
	token := strings.TrimPrefix(auth, "Bearer ")
	if expected := os.Getenv("CASDOOR_MEMBERSHIP_SERVICE_TOKEN"); expected == "" || token != expected {
		c.ResponseError("unauthorized")
		return
	}
	owner, user, plan := c.Ctx.Input.Query("owner"), c.Ctx.Input.Query("user"), c.Ctx.Input.Query("plan")
	value, err := object.GetMembershipEntitlement(owner, user, plan)
	if err != nil {
		c.ResponseError("membership unavailable")
		return
	}
	c.ResponseOk(value)
}
