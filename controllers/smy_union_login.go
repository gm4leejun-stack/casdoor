package controllers

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/beego/beego/v2/server/web"
	"github.com/casdoor/casdoor/form"
	"github.com/casdoor/casdoor/object"
	"github.com/casdoor/casdoor/util"
)

const smyUnionLoginExchangePath = "/smy/unionLogin/exchange"

type smyUnionLoginConfig struct {
	baseURL     string
	lobehubURL  string
	application *object.Application
}
type smyUnionLoginError struct{ code, msg string }

func (e *smyUnionLoginError) Error() string { return e.msg }
func smyUnionError(code, msg string) error  { return &smyUnionLoginError{code: code, msg: msg} }

func smyUnionURL(value string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(value))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return nil, fmt.Errorf("invalid union login URL")
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	localHTTP := host == "localhost" || (ip != nil && (ip.IsLoopback() || (web.BConfig.RunMode == "dev" && ip.IsPrivate())))
	if u.Scheme != "https" && !(u.Scheme == "http" && localHTTP) {
		return nil, fmt.Errorf("union login requires HTTPS except loopback or private IP development")
	}
	return u, nil
}

func loadSmyUnionLoginConfig() (*smyUnionLoginConfig, error) {
	base, err := smyUnionURL(os.Getenv("SMY_UNION_LOGIN_BASE_URL"))
	if err != nil || (base.Path != "" && base.Path != "/") {
		return nil, fmt.Errorf("SMY_UNION_LOGIN_BASE_URL must be the externally reachable Casdoor origin")
	}
	lobe, err := smyUnionURL(os.Getenv("SMY_UNION_LOGIN_LOBEHUB_URL"))
	if err != nil || lobe.Path != "/signin/smy" {
		return nil, fmt.Errorf("SMY_UNION_LOGIN_LOBEHUB_URL must end with /signin/smy")
	}
	id := strings.TrimSpace(os.Getenv("SMY_UNION_LOGIN_APPLICATION"))
	if id == "" {
		return nil, fmt.Errorf("SMY_UNION_LOGIN_APPLICATION is required")
	}
	app, err := object.GetApplication(id)
	if err != nil {
		return nil, err
	}
	if app == nil || app.Organization != object.SmyMembershipOwner || app.DisableSignin || !app.EnableAutoSignin || app.HasPromptPage() {
		return nil, fmt.Errorf("union login requires an enabled beibei application with auto signin and no interactive prompt page")
	}
	callback := *lobe
	callback.Path, callback.RawPath = "/api/auth/oauth2/callback/casdoor", ""
	found := false
	for _, redirect := range app.RedirectUris {
		if redirect == callback.String() {
			found = true
		}
	}
	if !found {
		return nil, fmt.Errorf("union login application must allow the exact LobeHub Better Auth callback")
	}
	return &smyUnionLoginConfig{baseURL: strings.TrimRight(base.String(), "/"), lobehubURL: lobe.String(), application: app}, nil
}

// Decode nested business fields separately; phone information is ignored.
func smyUnionUniqueID(business []byte) (string, error) {
	decoder := json.NewDecoder(bytes.NewReader(business))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return "", smyUnionError("000017", "biz_content must be an object")
	}
	seen := map[string]bool{}
	uniqueID := ""
	for decoder.More() {
		token, err = decoder.Token()
		if err != nil {
			return "", smyUnionError("000017", "invalid biz_content")
		}
		name := token.(string)
		if seen[name] {
			return "", smyUnionError("000017", "duplicate business field")
		}
		seen[name] = true
		var raw json.RawMessage
		if err = decoder.Decode(&raw); err != nil {
			return "", smyUnionError("000017", "invalid business field")
		}
		if name == "uniqueId" {
			if err = json.Unmarshal(raw, &uniqueID); err != nil {
				return "", smyUnionError("000017", "uniqueId must be a string")
			}
		}
	}
	if _, err = decoder.Token(); err != nil {
		return "", smyUnionError("000017", "invalid biz_content")
	}
	if _, err = decoder.Token(); err != io.EOF {
		return "", smyUnionError("000017", "trailing business JSON")
	}
	if strings.TrimSpace(uniqueID) == "" || len(uniqueID) > 100 || uniqueID != strings.TrimSpace(uniqueID) {
		return "", smyUnionError("000017", "uniqueId must be non-empty, at most 100 bytes, without surrounding whitespace")
	}
	return uniqueID, nil
}

func smyDecodeUnionLogin(body []byte, keys *smyProtocolKeys, now time.Time) (object.SmyUnionLoginRequest, string, error) {
	var req object.SmyUnionLoginRequest
	envelope, err := smyParseEnvelope(body)
	if err != nil {
		return req, "2.0", smyUnionError("000017", "invalid envelope")
	}
	version := envelope["version"]
	if version != "2.0" && version != "3.1" {
		return req, "2.0", smyUnionError("000017", "version must be 2.0 or 3.1")
	}
	if envelope["merchantNo"] != keys.merchantNo || !smyRequestIDPattern.MatchString(envelope["req_seq_number"]) || envelope["biz_content"] == "" {
		return req, version, smyUnionError("000017", "invalid merchantNo, req_seq_number or biz_content")
	}
	if err = smyVerify(envelope, keys.publicKey); err != nil {
		return req, version, smyUnionError("000001", "signature verification failed")
	}
	at, err := time.ParseInLocation("2006-01-02 15:04:05", envelope["timestamp"], time.FixedZone("Asia/Shanghai", 8*3600))
	if err != nil || !now.Before(at.Add(object.SmyUnionLoginLifetime)) || at.After(now.Add(object.SmyUnionLoginLifetime)) {
		return req, version, smyUnionError("000017", "timestamp must be within 5 minutes of server time")
	}
	business := []byte(envelope["biz_content"])
	if version == "3.1" {
		secret, decodeErr := base64.StdEncoding.DecodeString(envelope["secretKey"])
		if decodeErr != nil {
			return req, version, smyUnionError("000018", "invalid secretKey")
		}
		key, decryptErr := rsa.DecryptPKCS1v15(rand.Reader, keys.privateKey, secret)
		if decryptErr != nil {
			return req, version, smyUnionError("000018", "secretKey decryption failed")
		}
		ciphertext, decodeErr := base64.StdEncoding.DecodeString(envelope["biz_content"])
		if decodeErr != nil {
			return req, version, smyUnionError("000018", "invalid encrypted biz_content")
		}
		business, err = smyAES(ciphertext, key, false)
		if err != nil {
			return req, version, smyUnionError("000018", "biz_content decryption failed")
		}
	} else if envelope["secretKey"] != "" {
		return req, version, smyUnionError("000017", "2.0 does not accept secretKey")
	}
	req.UniqueID, err = smyUnionUniqueID(business)
	req.RequestID, req.MerchantNo, req.RequestTime = envelope["req_seq_number"], envelope["merchantNo"], at
	return req, version, err
}

func (c *ApiController) SmyUnionLogin() {
	c.Ctx.Output.Header("Cache-Control", "no-store")
	c.Ctx.Output.Header("Content-Type", "application/json; charset=utf-8")
	keys, err := loadSmyProtocolKeys()
	if err != nil {
		c.Ctx.Output.SetStatus(http.StatusServiceUnavailable)
		c.Data["json"] = map[string]string{"error": "union login protocol configuration unavailable"}
		_ = c.ServeJSON()
		return
	}
	body := c.Ctx.Input.RequestBody
	if len(body) == 0 {
		body, err = io.ReadAll(io.LimitReader(c.Ctx.Request.Body, 65537))
	}
	version := "2.0"
	var req object.SmyUnionLoginRequest
	var response []byte
	if err == nil && len(body) > 65536 {
		err = smyUnionError("000017", "request exceeds 64KiB")
	}
	if err == nil {
		req, version, err = smyDecodeUnionLogin(body, keys, time.Now().UTC())
	}
	if err == nil {
		var config *smyUnionLoginConfig
		config, err = loadSmyUnionLoginConfig()
		if err == nil {
			req.Application = config.application.GetId()
			_, err = object.IssueSmyUnionLogin(req, func(ticket string) error {
				h5URL := config.baseURL + smyUnionLoginExchangePath + "?ticket=" + url.QueryEscape(ticket)
				var responseErr error
				response, responseErr = smyBusinessResponse(keys, version, "000000", "Success", map[string]string{"h5Url": h5URL})
				return responseErr
			})
		}
	}
	if err != nil {
		code := "999999"
		var businessErr *smyUnionLoginError
		if errors.As(err, &businessErr) {
			code = businessErr.code
		}
		if errors.Is(err, object.ErrSmyUnionLoginReplay) {
			code = "000015"
		}
		response, err = smyBusinessResponse(keys, version, code, err.Error(), map[string]string{})
	}
	if err != nil {
		c.Ctx.Output.SetStatus(http.StatusServiceUnavailable)
		c.Data["json"] = map[string]string{"error": "union login response preparation failed"}
		_ = c.ServeJSON()
		return
	}
	_ = c.Ctx.Output.Body(response)
}

func smyUnionAccountSafety(user *object.User, organization *object.Organization) error {
	if user.Type != "normal-user" || user.IsAdmin || user.NeedUpdatePassword || user.IsMfaEnabled() || object.IsNeedPromptMfa(organization, user) {
		return fmt.Errorf("账号需要其他安全验证，无法自动联登")
	}
	return nil
}

func (c *ApiController) SmyUnionLoginExchange() {
	c.Ctx.Output.Header("Cache-Control", "no-store")
	c.Ctx.Output.Header("Referrer-Policy", "no-referrer")
	c.Ctx.Output.Header("X-Content-Type-Options", "nosniff")
	fail := func(err error) {
		c.Ctx.Output.Header("Content-Type", "text/plain; charset=utf-8")
		c.Ctx.Output.SetStatus(http.StatusBadRequest)
		_ = c.Ctx.Output.Body([]byte("联登失败：" + err.Error() + "。请返回萨摩耶重新发起联登。"))
	}
	config, err := loadSmyUnionLoginConfig()
	if err != nil {
		fail(err)
		return
	}
	user, application, err := object.ConsumeSmyUnionLogin(c.Ctx.Input.Query("ticket"), config.application.GetId(), time.Time{}, func(user *object.User, application *object.Application) error {
		allowed, err := object.IsUserOfApplication(user, application)
		if err != nil || !allowed {
			return fmt.Errorf("账号不属于目标应用")
		}
		organization, err := object.GetOrganizationByUser(user)
		if err != nil || organization == nil {
			return fmt.Errorf("账号组织不可用")
		}
		if err := smyUnionAccountSafety(user, organization); err != nil {
			return err
		}
		if application.HasPromptPage() {
			return fmt.Errorf("目标应用需要交互确认，无法自动联登")
		}
		application.OrganizationObj = organization
		if err := object.CheckApplicationSignin(application, user, util.GetClientIpFromRequest(c.Ctx.Request), c.GetAcceptLanguage()); err != nil {
			return err
		}
		consent, err := object.CheckConsentRequired(user, application, "openid email profile")
		if err != nil || consent {
			return fmt.Errorf("目标应用需要授权确认，无法自动联登")
		}
		return nil
	})
	if err != nil {
		fail(err)
		return
	}
	// Rotate the browser session and remove the previous authentication context.
	clearAuth := func() {
		for _, key := range []string{"username", "paidUsername", "accessToken", "authTime", "SessionData", object.MfaSessionUserId, "verificationCodeType", mfaSetupVerifiedDestSession, SamlRequestIdSessionKey, "impersonateUser"} {
			c.DelSession(key)
		}
		c.clearSessionOidc()
	}
	clearAuth()
	c.Ctx.SetCookie("impersonateUser", "", -1, "/")
	if err = c.SessionRegenerateID(); err != nil {
		fail(fmt.Errorf("无法建立新登录会话"))
		return
	}
	c.Ctx.Input.SetParam("sessionSignin", "false")
	resp := c.HandleLoggedIn(application, user, &form.AuthForm{Type: ResponseTypeLogin, SigninMethod: "Samoyed", Application: application.Name, Organization: user.Owner, Username: user.Name, AutoSignin: true})
	if resp == nil || c.Ctx.ResponseWriter.Started {
		clearAuth()
		return
	}
	if resp.Status != "ok" {
		clearAuth()
		fail(fmt.Errorf("%s", resp.Msg))
		return
	}
	c.Redirect(config.lobehubURL+"?subject="+url.QueryEscape(user.Id), http.StatusFound)
}
