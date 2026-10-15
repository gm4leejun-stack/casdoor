package controllers

import (
	"crypto/rand"
	"encoding/json"
	"os"
	"time"

	"github.com/casdoor/casdoor/conf"
	"github.com/casdoor/casdoor/object"
)

type redemptionBatchRequest struct {
	Owner          string `json:"owner"`
	Name           string `json:"name"`
	DisplayName    string `json:"displayName"`
	Plan           string `json:"plan"`
	DurationDays   int    `json:"durationDays"`
	Quantity       int    `json:"quantity"`
	CodeExpireTime string `json:"codeExpireTime"`
}

type redeemMembershipRequest struct {
	Owner     string `json:"owner"`
	Code      string `json:"code"`
	RequestID string `json:"requestId"`
}

func membershipPepper() string { return os.Getenv("CASDOOR_MEMBERSHIP_CODE_PEPPER") }

func (c *ApiController) AddRedemptionBatch() {
	if conf.IsBaseMode() {
		c.ResponseError("customized membership endpoints are disabled in base mode")
		return
	}
	var req redemptionBatchRequest
	if err := json.Unmarshal(c.Ctx.Input.RequestBody, &req); err != nil {
		c.ResponseError("invalid request body")
		return
	}
	if !c.IsAdminOfOrganization(req.Owner) {
		c.ResponseError(c.T("auth:Unauthorized operation"))
		return
	}
	batch := &object.RedemptionBatch{Owner: req.Owner, Name: req.Name, DisplayName: req.DisplayName, Plan: req.Plan, DurationDays: req.DurationDays, Quantity: req.Quantity, CodeExpireTime: req.CodeExpireTime, CreatedBy: c.GetSessionUsername(), State: object.RedemptionBatchActive}
	// Plaintext codes are returned only once and never stored. A no-store header
	// prevents browsers and intermediary caches from retaining the export.
	c.Ctx.Output.Header("Cache-Control", "no-store")
	codes, err := object.CreateRedemptionBatch(batch, membershipPepper(), rand.Reader)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk(map[string]interface{}{"batch": batch, "codes": codes})
}

func (c *ApiController) GetRedemptionBatches() {
	if conf.IsBaseMode() {
		c.ResponseError("customized membership endpoints are disabled in base mode")
		return
	}
	owner := c.Ctx.Input.Query("owner")
	if !c.IsAdminOfOrganization(owner) {
		c.ResponseError(c.T("auth:Unauthorized operation"))
		return
	}
	batches, err := object.GetRedemptionBatches(owner)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk(batches)
}

func (c *ApiController) DisableRedemptionBatch() {
	if conf.IsBaseMode() {
		c.ResponseError("customized membership endpoints are disabled in base mode")
		return
	}
	var req struct{ Owner, Name string }
	if err := json.Unmarshal(c.Ctx.Input.RequestBody, &req); err != nil {
		c.ResponseError("invalid request body")
		return
	}
	if !c.IsAdminOfOrganization(req.Owner) {
		c.ResponseError(c.T("auth:Unauthorized operation"))
		return
	}
	affected, err := object.DisableRedemptionBatch(req.Owner, req.Name)
	c.Data["json"] = wrapActionResponse(affected, err)
	c.ServeJSON()
}

func (c *ApiController) RedeemMembershipCode() {
	if conf.IsBaseMode() {
		c.ResponseError("customized membership endpoints are disabled in base mode")
		return
	}
	var req redeemMembershipRequest
	if err := json.Unmarshal(c.Ctx.Input.RequestBody, &req); err != nil {
		c.ResponseError("invalid request body")
		return
	}
	currentUser := c.getCurrentUser()
	owner := req.Owner
	if owner == "" || currentUser == nil || currentUser.Owner != owner {
		c.ResponseError(c.T("auth:Unauthorized operation"))
		return
	}
	userName := currentUser.Name
	result, err := object.RedeemMembershipCode(object.RedemptionRequest{Owner: owner, User: userName, Code: req.Code, Pepper: membershipPepper(), RequestID: req.RequestID, Now: time.Now().UTC()})
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk(result)
}

func (c *ApiController) GetMyRedemptions() {
	if conf.IsBaseMode() {
		c.ResponseError("customized membership endpoints are disabled in base mode")
		return
	}
	owner, currentUser := c.Ctx.Input.Query("owner"), c.getCurrentUser()
	if owner == "" || currentUser == nil || currentUser.Owner != owner {
		c.ResponseError(c.T("auth:Unauthorized operation"))
		return
	}
	audits, err := object.GetRedemptionAudits(owner, currentUser.Name)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk(audits)
}

func (c *ApiController) GetRedemptions() {
	if conf.IsBaseMode() {
		c.ResponseError("customized membership endpoints are disabled in base mode")
		return
	}
	owner := c.Ctx.Input.Query("owner")
	if !c.IsAdminOfOrganization(owner) {
		c.ResponseError(c.T("auth:Unauthorized operation"))
		return
	}
	audits, err := object.GetRedemptionAudits(owner, "")
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk(audits)
}
