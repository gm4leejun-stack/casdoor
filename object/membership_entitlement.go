package object

import "time"

type EntitlementStatus string

const (
	EntitlementMissing   EntitlementStatus = "Missing"
	EntitlementActive    EntitlementStatus = "Active"
	EntitlementUpcoming  EntitlementStatus = "Upcoming"
	EntitlementExpired   EntitlementStatus = "Expired"
	EntitlementSuspended EntitlementStatus = "Suspended"
	EntitlementInactive  EntitlementStatus = "Inactive"
	EntitlementError     EntitlementStatus = "Error"
)

type MembershipEntitlement struct {
	Subject        MembershipSubject `json:"subject"`
	Plan           string            `json:"plan"`
	Status         EntitlementStatus `json:"status"`
	StartsAt       string            `json:"startsAt,omitempty"`
	ExpiresAt      string            `json:"expiresAt,omitempty"`
	SubscriptionID string            `json:"subscriptionId,omitempty"`
	EvaluatedAt    string            `json:"evaluatedAt"`
}

type MembershipSubject struct {
	Owner    string `json:"owner"`
	UserName string `json:"userName"`
}

func EvaluateMembership(sub *Subscription, now time.Time) MembershipEntitlement {
	result := MembershipEntitlement{Status: EntitlementMissing, EvaluatedAt: now.UTC().Format(time.RFC3339)}
	if sub == nil {
		return result
	}
	result.Subject = MembershipSubject{Owner: sub.Owner, UserName: sub.User}
	result.Plan, result.SubscriptionID = sub.Plan, sub.GetId()
	result.StartsAt, result.ExpiresAt = sub.StartTime, sub.EndTime
	if sub.State == SubStateSuspended {
		result.Status = EntitlementSuspended
		return result
	}
	start, startErr := time.Parse(time.RFC3339, sub.StartTime)
	end, endErr := time.Parse(time.RFC3339, sub.EndTime)
	if startErr != nil || endErr != nil || !end.After(start) {
		result.Status = EntitlementError
		return result
	}
	if !now.Before(end) {
		result.Status = EntitlementExpired
		return result
	}
	if now.Before(start) {
		result.Status = EntitlementUpcoming
		return result
	}
	if sub.State == SubStateActive {
		result.Status = EntitlementActive
	} else if sub.State == SubStateUpcoming {
		result.Status = EntitlementUpcoming
	} else {
		result.Status = EntitlementInactive
	}
	return result
}
