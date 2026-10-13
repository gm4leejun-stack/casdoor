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

func TestMembershipEntitlementForSubjectRejectsUnknownUserID(t *testing.T) {
	_, err := membershipEntitlementForSubject(
		"https://id.example.com", "missing",
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
