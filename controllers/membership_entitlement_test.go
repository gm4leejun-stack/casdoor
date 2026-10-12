package controllers

import (
	"errors"
	"testing"

	"github.com/casdoor/casdoor/object"
)

func TestValidateMembershipSubject(t *testing.T) {
	const issuer = "https://id.example.com"
	for _, test := range []struct {
		name   string
		owner  string
		plan   string
		issuer string
		userID string
		valid  bool
	}{
		{name: "valid", owner: "acme", plan: "pro", issuer: issuer, userID: "stable-id", valid: true},
		{name: "missing owner", plan: "pro", issuer: issuer, userID: "stable-id"},
		{name: "missing plan", owner: "acme", issuer: issuer, userID: "stable-id"},
		{name: "missing issuer", owner: "acme", plan: "pro", userID: "stable-id"},
		{name: "missing user id", owner: "acme", plan: "pro", issuer: issuer},
		{name: "wrong issuer", owner: "acme", plan: "pro", issuer: "https://other.example.com", userID: "stable-id"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := validateMembershipSubject(test.owner, test.plan, test.issuer, test.userID, issuer); got != test.valid {
				t.Fatalf("validateMembershipSubject() = %v, want %v", got, test.valid)
			}
		})
	}
}

func TestMembershipEntitlementForSubjectResolvesStableUserID(t *testing.T) {
	var lookedUpUser string
	result, err := membershipEntitlementForSubject(
		"acme",
		"https://id.example.com",
		"stable-id",
		"pro",
		func(owner, userID string) (*object.User, error) {
			if owner != "acme" || userID != "stable-id" {
				t.Fatalf("unexpected stable lookup: %q %q", owner, userID)
			}
			return &object.User{Name: "internal-name"}, nil
		},
		func(owner, userName, plan string) (*object.MembershipEntitlement, error) {
			lookedUpUser = userName
			return &object.MembershipEntitlement{}, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if lookedUpUser != "internal-name" {
		t.Fatalf("subscription lookup used %q, want resolved internal username", lookedUpUser)
	}
	want := (object.MembershipSubject{Issuer: "https://id.example.com", UserID: "stable-id"})
	if result.Subject != want {
		t.Fatalf("subject = %#v, want %#v", result.Subject, want)
	}
}

func TestMembershipEntitlementForSubjectRejectsUnknownUserID(t *testing.T) {
	_, err := membershipEntitlementForSubject(
		"acme", "https://id.example.com", "missing", "pro",
		func(string, string) (*object.User, error) { return nil, nil },
		func(string, string, string) (*object.MembershipEntitlement, error) {
			t.Fatal("subscription lookup must not run")
			return nil, nil
		},
	)
	if !errors.Is(err, errMembershipUserNotFound) {
		t.Fatalf("error = %v, want errMembershipUserNotFound", err)
	}
}
