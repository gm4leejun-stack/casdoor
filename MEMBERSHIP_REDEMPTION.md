# Casdoor membership redemption extension

This branch is based on `gm4leejun-stack/casdoor` commit
`43a6aabc68d7790d1698bd9eb50ef0f682952516`. Build production images from an
immutable commit and promote them by digest.

Required runtime configuration:

- `CASDOOR_MEMBERSHIP_CODE_PEPPER`: secret with at least 16 characters. Store
  it in the deployment secret manager. Losing it makes unused codes unusable.
- `CASDOOR_MEMBERSHIP_SERVICE_CLIENT_ID`: client ID of the dedicated Gundam
  read-only application.
- `CASDOOR_MEMBERSHIP_SERVICE_OWNER`: owner that service application may query.
- `CASDOOR_MEMBERSHIP_SERVICE_PLAN`: plan that service application may query.

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
