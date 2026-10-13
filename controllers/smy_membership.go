package controllers

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"time"

	"github.com/casdoor/casdoor/object"
)

var smyRequestIDPattern = regexp.MustCompile(`^[A-Za-z0-9]{1,32}$`)

func smyDecodeMembership(body []byte, keys *smyProtocolKeys) (object.SmyMembershipInput, string, error) {
	var req object.SmyMembershipInput
	envelope, err := smyParseEnvelope(body)
	if err != nil {
		return req, "2.0", err
	}
	version := envelope["version"]
	if version != "2.0" && version != "3.1" {
		return req, "2.0", fmt.Errorf("version must be 2.0 or 3.1")
	}
	if envelope["merchantNo"] != keys.merchantNo || !smyRequestIDPattern.MatchString(envelope["req_seq_number"]) || envelope["biz_content"] == "" {
		return req, version, fmt.Errorf("invalid merchantNo, req_seq_number or biz_content")
	}
	if _, err = time.ParseInLocation("2006-01-02 15:04:05", envelope["timestamp"], time.FixedZone("Asia/Shanghai", 8*3600)); err != nil {
		return req, version, fmt.Errorf("invalid timestamp")
	}
	if err = smyVerify(envelope, keys.publicKey); err != nil {
		return req, version, err
	}
	business := []byte(envelope["biz_content"])
	if version == "3.1" {
		secret, err := base64.StdEncoding.DecodeString(envelope["secretKey"])
		if err != nil {
			return req, version, fmt.Errorf("invalid secretKey")
		}
		key, err := rsa.DecryptPKCS1v15(rand.Reader, keys.privateKey, secret)
		if err != nil {
			return req, version, fmt.Errorf("secretKey decryption failed")
		}
		ciphertext, err := base64.StdEncoding.DecodeString(envelope["biz_content"])
		if err != nil {
			return req, version, fmt.Errorf("invalid encrypted biz_content")
		}
		business, err = smyAES(ciphertext, key, false)
		if err != nil {
			return req, version, err
		}
	} else if envelope["secretKey"] != "" {
		return req, version, fmt.Errorf("2.0 does not accept secretKey")
	}
	// Reject duplicate business fields as well; their interpretation must be unambiguous.
	fields, err := smyParseEnvelope(business)
	if err != nil {
		return req, version, fmt.Errorf("invalid biz_content: %w", err)
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		return req, version, err
	}
	if err = json.Unmarshal(encoded, &req); err != nil {
		return req, version, fmt.Errorf("invalid membership business fields")
	}
	req.RequestID = envelope["req_seq_number"]
	return req, version, object.ValidateSmyMembershipRequest(req)
}

func (c *ApiController) SmyMaintainMembership() {
	c.Ctx.Output.Header("Cache-Control", "no-store")
	c.Ctx.Output.Header("Content-Type", "application/json; charset=utf-8")
	keys, err := loadSmyProtocolKeys()
	if err != nil {
		c.Ctx.Output.SetStatus(503)
		c.Data["json"] = map[string]string{"error": "membership protocol configuration unavailable"}
		_ = c.ServeJSON()
		return
	}
	body := c.Ctx.Input.RequestBody
	if len(body) == 0 {
		body, err = io.ReadAll(io.LimitReader(c.Ctx.Request.Body, 65537))
	}
	version := "2.0"
	var req object.SmyMembershipInput
	if err == nil && len(body) > 65536 {
		err = fmt.Errorf("request exceeds 64KiB")
	}
	if err == nil {
		req, version, err = smyDecodeMembership(body, keys)
	}
	var response []byte
	if err == nil {
		err = object.ProcessSmyMembership(req, func() error {
			var prepareErr error
			response, prepareErr = smyResponse(keys, version, "10000", "Success", "SUCCESS")
			return prepareErr
		})
	}
	if err != nil {
		response, err = smyResponse(keys, version, "40000", err.Error(), "FAILED")
		if err != nil {
			c.Ctx.Output.SetStatus(503)
			c.Data["json"] = map[string]string{"error": "membership response preparation failed"}
			_ = c.ServeJSON()
			return
		}
	}
	_ = c.Ctx.Output.Body(response)
}

func (c *ApiController) GetMembershipCustomerHistory() {
	c.Ctx.Output.Header("Cache-Control", "no-store")
	if !c.IsAdminOfOrganization(object.SmyMembershipOwner) {
		c.ResponseError(c.T("auth:Unauthorized operation"))
		return
	}
	result, err := object.GetSmyMembershipHistory(c.Ctx.Input.Query("field"), c.Ctx.Input.Query("value"))
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk(result)
}
