package object

import (
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/xorm-io/core"
	"github.com/xorm-io/xorm"
)

const SmyUnionLoginLifetime = 5 * time.Minute

// One row is both the signed-request replay record and the one-time browser
// credential. The URL secret itself is never persisted. Records are retained
// after expiration/consumption so a repeated request cannot mint another link.
type SmyUnionLoginTicket struct {
	MerchantNo  string `xorm:"varchar(100) pk" json:"merchantNo"`
	RequestID   string `xorm:"'request_id' varchar(32) pk" json:"requestId"`
	TokenHash   string `xorm:"varchar(64) notnull unique" json:"-"`
	UserID      string `xorm:"'user_id' varchar(100) notnull index" json:"userId"`
	UserName    string `xorm:"varchar(255) notnull" json:"userName"`
	UniqueID    string `xorm:"'unique_id' varchar(100) notnull" json:"uniqueId"`
	Application string `xorm:"varchar(255) notnull" json:"application"`
	CreatedAt   int64  `xorm:"notnull" json:"createdAt"`
	ExpiresAt   int64  `xorm:"notnull" json:"expiresAt"`
	ConsumedAt  int64  `xorm:"notnull" json:"consumedAt"`
}

type SmyUnionLoginRequest struct {
	UniqueID    string
	MerchantNo  string
	RequestID   string
	Application string // Trusted configuration: owner/name, never from biz_content.
	RequestTime time.Time
	Now         time.Time // Zero uses the time after acquiring database locks.
}

var (
	ErrSmyUnionLoginReplay = errors.New("联登请求号已使用，请使用新的请求号重新发起")
	ErrSmyUnionLoginTicket = errors.New("联登链接无效、已过期或已使用，请从萨摩耶重新发起")
	smyUnionRequestID      = regexp.MustCompile(`^[A-Za-z0-9]{1,32}$`)
)

func validateSmyUnionLoginRequest(req SmyUnionLoginRequest, now time.Time) error {
	if req.UniqueID == "" || len(req.UniqueID) > 100 || strings.TrimSpace(req.UniqueID) != req.UniqueID {
		return errors.New("uniqueId 必须非空且不超过100字节")
	}
	if req.MerchantNo == "" || len(req.MerchantNo) > 100 || !smyUnionRequestID.MatchString(req.RequestID) {
		return errors.New("商户号或联登请求号无效")
	}
	if req.RequestTime.IsZero() || !now.Before(req.RequestTime.Add(SmyUnionLoginLifetime)) || req.RequestTime.After(now.Add(SmyUnionLoginLifetime)) {
		return errors.New("联登请求时间须在当前时间前后5分钟内")
	}
	return nil
}

func smyUnionApplication(session *xorm.Session, id string) (*Application, error) {
	parts := strings.Split(id, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, errors.New("联登目标应用配置无效，须为 owner/name")
	}
	app := new(Application)
	found, err := session.ID(core.PK{parts[0], parts[1]}).Get(app)
	if err != nil {
		return nil, err
	}
	if !found || app.Organization != SmyMembershipOwner || app.DisableSignin || !app.EnableAutoSignin {
		return nil, errors.New("联登目标应用不存在、组织不是 beibei、禁止登录或未启用自动登录")
	}
	return app, nil
}

// IssueSmyUnionLogin atomically creates/resolves the identity and persists the
// link. beforeCommit lets the caller finish signing/encrypting the response;
// a failure rolls back both new identity and ticket and returns no URL secret.
func IssueSmyUnionLogin(req SmyUnionLoginRequest, beforeCommit func(string) error) (string, error) {
	now := req.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if err := validateSmyUnionLoginRequest(req, now); err != nil {
		return "", err
	}
	session := ormer.Engine.NewSession()
	defer session.Close()
	if err := session.Begin(); err != nil {
		return "", err
	}
	defer session.Rollback()
	// Keep the same lock order as membership maintenance and identity creation.
	if _, err := lockSmyOrganization(session); err != nil {
		return "", err
	}
	app, err := smyUnionApplication(session, req.Application)
	if err != nil {
		return "", err
	}
	existing := new(SmyUnionLoginTicket)
	found, err := session.ID(core.PK{req.MerchantNo, req.RequestID}).Get(existing)
	if err != nil {
		return "", err
	}
	if found {
		return "", ErrSmyUnionLoginReplay
	}
	if req.Now.IsZero() {
		now = time.Now().UTC()
	}
	if err := validateSmyUnionLoginRequest(req, now); err != nil {
		return "", err
	}
	user, _, err := ResolveSmyIdentity(session, req.UniqueID, now)
	if err != nil {
		return "", err
	}
	token, err := generateMagicLinkToken()
	if err != nil {
		return "", err
	}
	ticket := &SmyUnionLoginTicket{MerchantNo: req.MerchantNo, RequestID: req.RequestID, TokenHash: HashMagicLinkSecret(token), UserID: user.Id, UserName: user.Name, UniqueID: req.UniqueID, Application: app.Owner + "/" + app.Name, CreatedAt: now.Unix(), ExpiresAt: now.Add(SmyUnionLoginLifetime).Unix()}
	if _, err := session.Insert(ticket); err != nil {
		return "", err
	}
	if beforeCommit != nil {
		if err := beforeCommit(token); err != nil {
			return "", err
		}
	}
	if err := session.Commit(); err != nil {
		return "", err
	}
	return token, nil
}

// ConsumeSmyUnionLogin serializes redemption across instances and rechecks only
// identity/account/application, never orders or membership. beforeCommit is for
// account security preflight, not for emitting cookies or an HTTP redirect.
func ConsumeSmyUnionLogin(token, expectedApplication string, now time.Time, beforeCommit func(*User, *Application) error) (*User, *Application, error) {
	if !smyUnionTokenValid(token) {
		return nil, nil, ErrSmyUnionLoginTicket
	}
	session := ormer.Engine.NewSession()
	defer session.Close()
	if err := session.Begin(); err != nil {
		return nil, nil, err
	}
	defer session.Rollback()
	if _, err := lockSmyOrganization(session); err != nil {
		return nil, nil, err
	}
	ticket := new(SmyUnionLoginTicket)
	found, err := session.Where("token_hash = ?", HashMagicLinkSecret(token)).ForUpdate().Get(ticket)
	if err != nil {
		return nil, nil, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if !found || ticket.ConsumedAt != 0 || now.Unix() >= ticket.ExpiresAt || ticket.Application != expectedApplication {
		return nil, nil, ErrSmyUnionLoginTicket
	}
	customers := []SmyMembershipCustomer{}
	if err := session.Where("unique_id = ?", ticket.UniqueID).ForUpdate().Find(&customers); err != nil {
		return nil, nil, err
	}
	if len(customers) != 1 || customers[0].UserID != ticket.UserID || customers[0].UserName != ticket.UserName {
		return nil, nil, errors.New("联登身份绑定已失效或不一致")
	}
	user, err := smyLockBoundUser(session, &customers[0])
	if err != nil {
		return nil, nil, err
	}
	app, err := smyUnionApplication(session, expectedApplication)
	if err != nil {
		return nil, nil, err
	}
	if beforeCommit != nil {
		if err := beforeCommit(user, app); err != nil {
			return nil, nil, err
		}
	}
	ticket.ConsumedAt = now.Unix()
	affected, err := session.ID(core.PK{ticket.MerchantNo, ticket.RequestID}).Where("consumed_at = 0 AND expires_at > ?", now.Unix()).Cols("consumed_at").Update(ticket)
	if err != nil {
		return nil, nil, err
	}
	if affected != 1 {
		return nil, nil, ErrSmyUnionLoginTicket
	}
	if err := session.Commit(); err != nil {
		return nil, nil, err
	}
	return user, app, nil
}

func smyUnionTokenValid(token string) bool {
	// A 32-byte random value encoded without padding is always 43 URL-safe bytes.
	if len(token) != 43 {
		return false
	}
	for _, c := range token {
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}
