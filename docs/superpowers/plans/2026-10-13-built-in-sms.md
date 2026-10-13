# Built-in Fixed-Protocol SMS Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let Casdoor call the fixed SMS API directly with four global settings and no bridge service.

**Architecture:** Add one implementation of the existing `SmsClient` interface, selected by a `Samoyed SMS` Provider. Keep the upstream URL, two business codes, and AES key in Casdoor process configuration.

**Tech Stack:** Go standard library, existing Casdoor Provider flow, React/TypeScript.

---

### Task 1: Direct SMS client

**Files:**
- Create: `object/sms_samoyed.go`
- Modify: `object/sms.go`

- [ ] Validate the four configuration values and AES key length.
- [ ] Encrypt the mainland mobile with AES-ECB/PKCS#7 and POST the fixed JSON body.
- [ ] Accept only HTTP 2xx with response code `200`.

### Task 2: Minimal Provider UI and deployment configuration

**Files:**
- Modify: `web/src/lib/setting.tsx`
- Modify: `web/src/pages/ProviderEditPage.tsx`
- Modify: `docker-compose.yml`

- [ ] Add `Samoyed SMS` and show only the SMS test input.
- [ ] Pass the four existing process environment variables into the Compose container.

### Task 3: Remove bridge and verify

**Files:**
- Delete: `SMS_BRIDGE.md`
- Delete: `compose.sms.yml`
- Delete: `sms-bridge/`

- [ ] Remove the obsolete service artifacts.
- [ ] Run `gofmt`, affected Go tests, full Go tests, frontend typecheck/lint/build, and a local fixed-protocol smoke check.
