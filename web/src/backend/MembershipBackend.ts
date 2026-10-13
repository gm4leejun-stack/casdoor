import * as Setting from "@/lib/setting";

export type MembershipHistoryField = "mobile" | "userId" | "orderNo";

export function getMembershipCustomerHistory(field: MembershipHistoryField, value: string) {
  const params = new URLSearchParams({field, value});
  return fetch(Setting.ServerUrl + "/api/get-membership-customer-history?" + params, {
    method: "GET",
    credentials: "include",
    cache: "no-store",
    headers: {
      "Accept-Language": Setting.getAcceptLanguage(),
    },
  }).then(res => res.json());
}
