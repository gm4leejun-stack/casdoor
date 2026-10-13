package controllers

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/casdoor/casdoor/object"
)

func TestSmyProtocolRoundTripAndTampering(t *testing.T) {
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	keys := &smyProtocolKeys{merchantNo: "test-merchant", publicKey: &private.PublicKey, privateKey: private}
	business := `{"orderNo":"A","productType":"YHK","productCode":"YHK01","uniqueId":"customer","periodTime":"3","mobile":"13800000001","orderStatus":"2"}`
	envelope := map[string]string{"version": "2.0", "merchantNo": keys.merchantNo, "req_seq_number": "seq1", "timestamp": "2026-10-15 10:00:00", "biz_content": business}
	envelope["sign"], err = smySign(envelope, private)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(envelope)
	req, version, err := smyDecodeMembership(body, keys)
	if err != nil || req.OrderNo != "A" || req.PeriodTime != "3" || version != "2.0" {
		t.Fatalf("decode: %+v %v", req, err)
	}
	envelope["biz_content"] += " "
	body, _ = json.Marshal(envelope)
	if _, _, err = smyDecodeMembership(body, keys); err == nil {
		t.Fatal("tampered raw business accepted")
	}
	if _, err = smyParseEnvelope([]byte(`{"version":"2.0","version":"3.1"}`)); err == nil {
		t.Fatal("duplicate field accepted")
	}
	for _, period := range []string{"", "1", "invalid"} {
		fields := map[string]string{"orderNo": "A", "productType": "YHK", "productCode": "YHK01", "uniqueId": "customer", "mobile": "13800000001", "orderStatus": "6"}
		if period != "" {
			fields["periodTime"] = period
		}
		refundBusiness, _ := json.Marshal(fields)
		envelope["biz_content"] = string(refundBusiness)
		envelope["sign"], err = smySign(envelope, private)
		if err != nil {
			t.Fatal(err)
		}
		refundBody, _ := json.Marshal(envelope)
		refund, _, err := smyDecodeMembership(refundBody, keys)
		if err != nil || refund.OrderStatus != "6" || refund.PeriodTime != period {
			t.Fatalf("refund period %q rejected: %+v %v", period, refund, err)
		}
	}
	for _, version := range []string{"2.0", "3.1"} {
		body, err = smyResponse(keys, version, "10000", "Success", "SUCCESS")
		if err != nil {
			t.Fatal(err)
		}
		result, err := smyParseEnvelope(body)
		if err != nil {
			t.Fatal(err)
		}
		if err = smyVerify(result, keys.publicKey); err != nil {
			t.Fatal(err)
		}
		if version == "3.1" {
			secret, _ := base64.StdEncoding.DecodeString(result["secretKey"])
			key, err := rsa.DecryptPKCS1v15(rand.Reader, private, secret)
			if err != nil {
				t.Fatal(err)
			}
			encrypted, _ := base64.StdEncoding.DecodeString(result["biz_content"])
			plain, err := smyAES(encrypted, key, false)
			if err != nil || !bytes.Contains(plain, []byte("SUCCESS")) {
				t.Fatal("response decrypt", err)
			}
		}
	}
	key3 := []byte("0123456789abcdef")
	encrypted, _ := smyAES([]byte(business), key3, true)
	secret, _ := rsa.EncryptPKCS1v15(rand.Reader, keys.publicKey, key3)
	envelope["version"] = "3.1"
	envelope["biz_content"] = base64.StdEncoding.EncodeToString(encrypted)
	envelope["secretKey"] = base64.StdEncoding.EncodeToString(secret)
	envelope["sign"], err = smySign(envelope, private)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = json.Marshal(envelope)
	req, version, err = smyDecodeMembership(body, keys)
	if err != nil || req.OrderNo != "A" || req.PeriodTime != "3" || version != "3.1" {
		t.Fatalf("3.1decode: %+v %v", req, err)
	}
	key := []byte("0123456789abcdef")
	for _, plain := range [][]byte{[]byte(business), []byte("1234567890123456")} {
		encrypted, err := smyAES(plain, key, true)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := smyAES(encrypted, key, false)
		if err != nil || !bytes.Equal(decoded, plain) {
			t.Fatal("AES zero-padding roundtrip failed", err)
		}
	}
	if _, err = smyAES([]byte("short"), key, false); err == nil {
		t.Fatal("malformed ciphertext accepted")
	}
}

func TestValidateMembershipSubject(t *testing.T) {
	const issuer = "https://id.example.com"
	for _, test := range []struct {
		name   string
		issuer string
		userID string
		valid  bool
	}{
		{name: "valid", issuer: issuer, userID: "stable-id", valid: true},
		{name: "missing issuer", userID: "stable-id"},
		{name: "missing user id", issuer: issuer},
		{name: "wrong issuer", issuer: "https://other.example.com", userID: "stable-id"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := validateMembershipSubject(test.issuer, test.userID, issuer); got != test.valid {
				t.Fatalf("validateMembershipSubject() = %v, want %v", got, test.valid)
			}
		})
	}
}

func TestMembershipEntitlementForSubjectResolvesStableUserID(t *testing.T) {
	var lookedUpUser, lookedUpPlan string
	result, err := membershipEntitlementForSubject(
		"https://id.example.com",
		"stable-id",
		func(owner, userID string) (*object.User, error) {
			if owner != "beibei" || userID != "stable-id" {
				t.Fatalf("unexpected stable lookup: %q %q", owner, userID)
			}
			return &object.User{Name: "internal-name"}, nil
		},
		func(owner, userName, plan string) (*object.MembershipEntitlement, error) {
			if owner != "beibei" {
				t.Fatalf("subscription lookup used owner %q, want beibei", owner)
			}
			lookedUpUser = userName
			lookedUpPlan = plan
			return &object.MembershipEntitlement{}, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if lookedUpUser != "internal-name" {
		t.Fatalf("subscription lookup used %q, want resolved internal username", lookedUpUser)
	}
	if lookedUpPlan != "beibei-pro" {
		t.Fatalf("subscription lookup used plan %q, want beibei-pro", lookedUpPlan)
	}
	want := (object.MembershipSubject{Issuer: "https://id.example.com", UserID: "stable-id"})
	if result.Subject != want {
		t.Fatalf("subject = %#v, want %#v", result.Subject, want)
	}
}

func TestMembershipEntitlementForSubjectReturnsMissingForUnknownUserID(t *testing.T) {
	result, err := membershipEntitlementForSubject(
		"https://id.example.com", "missing",
		func(string, string) (*object.User, error) { return nil, nil },
		func(string, string, string) (*object.MembershipEntitlement, error) {
			t.Fatal("subscription lookup must not run")
			return nil, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != object.EntitlementMissing {
		t.Fatalf("status = %q, want %q", result.Status, object.EntitlementMissing)
	}
	if result.Plan != "beibei-pro" {
		t.Fatalf("plan = %q, want beibei-pro", result.Plan)
	}
	wantSubject := (object.MembershipSubject{Issuer: "https://id.example.com", UserID: "missing"})
	if result.Subject != wantSubject {
		t.Fatalf("subject = %#v, want %#v", result.Subject, wantSubject)
	}
	if result.EvaluatedAt == "" {
		t.Fatal("evaluatedAt must be set")
	}
}
