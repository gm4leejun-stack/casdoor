import * as Setting from "@/lib/setting";

const headers = () => ({"Content-Type": "application/json", "Accept-Language": Setting.getAcceptLanguage()});

export function createBatch(batch: any) {
  return fetch(`${Setting.ServerUrl}/api/add-redemption-batch`, {method: "POST", credentials: "include", headers: headers(), body: JSON.stringify(batch)}).then(res => res.json());
}

export function getBatches(owner: string) {
  return fetch(`${Setting.ServerUrl}/api/get-redemption-batches?owner=${encodeURIComponent(owner)}`, {credentials: "include", headers: {"Accept-Language": Setting.getAcceptLanguage()}}).then(res => res.json());
}

export function disableBatch(owner: string, name: string) {
  return fetch(`${Setting.ServerUrl}/api/disable-redemption-batch`, {method: "POST", credentials: "include", headers: headers(), body: JSON.stringify({owner, name})}).then(res => res.json());
}

export function redeem(owner: string, code: string) {
  return fetch(`${Setting.ServerUrl}/api/redeem-membership-code`, {method: "POST", credentials: "include", headers: headers(), body: JSON.stringify({owner, code})}).then(res => res.json());
}

export function getMyEntitlement(owner: string, plan: string) {
  return fetch(`${Setting.ServerUrl}/api/get-my-membership-entitlement?owner=${encodeURIComponent(owner)}&plan=${encodeURIComponent(plan)}`, {credentials: "include"}).then(res => res.json());
}
