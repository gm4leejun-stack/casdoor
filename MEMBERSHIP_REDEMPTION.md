# Casdoor membership redemption extension

This branch is based on `gm4leejun-stack/casdoor` commit
`43a6aabc68d7790d1698bd9eb50ef0f682952516`. Build production images from an
immutable commit and promote them by digest.

Required runtime configuration:

- `CASDOOR_MEMBERSHIP_CODE_PEPPER`: secret with at least 16 characters. Store
  it in the deployment secret manager. Losing it makes unused codes unusable.
- `CASDOOR_MEMBERSHIP_SERVICE_TOKEN`: shared secret used only by Gundam's
  internal read-only entitlement request. Gundam sends it in the
  `X-Membership-Service-Token` header.

The database contains only HMAC-SHA-256 code digests and safe hints. Plaintext
codes are returned exactly once by `POST /api/add-redemption-batch`; the
response is marked `Cache-Control: no-store`.

Database rollout must first check that `(owner,user,plan)` has no duplicate
Subscription rows. The redemption service refuses ambiguous duplicates. The
PostgreSQL integration job must exercise concurrent use with
`CASDOOR_TEST_POSTGRES_DSN`; a production rollout should not rely on SQLite for
transaction semantics.

Rollback procedure: disable issuance, stop Casdoor, restore the pre-deploy
database snapshot, and deploy the previously recorded image digest. Never
rotate the code pepper without retaining the previous lookup key until all
outstanding codes have expired.
