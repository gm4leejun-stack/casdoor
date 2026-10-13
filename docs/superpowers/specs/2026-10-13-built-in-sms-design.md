# Built-in fixed-protocol SMS design

## Goal

Casdoor directly calls the single, fixed `/NotifyService/sendMessage` SMS API. The deployment runs no bridge process and each application does not repeat upstream configuration.

## Configuration

The existing Casdoor configuration mechanism supplies one global set of values:

- `smsUrl`
- `smsBusinessCode`
- `smsMessageEnumCode`
- `smsMobileAesKey`

Secrets stay in environment-specific Casdoor configuration and are never stored in Provider records or returned by APIs. Startup remains possible without these optional values; selecting or testing this Provider returns a clear configuration error if any value is missing or the AES key is not 16, 24, or 32 UTF-8 bytes.

## Backend

Add one SMS client implementing the existing `sender.SmsClient` interface and select it from `getSmsClient` using a dedicated Provider type, `Samoyed SMS`. It will:

1. Accept Casdoor's generated verification code and one `+86` mainland China mobile number.
2. Remove `+86`, encrypt the mobile using AES-ECB with PKCS#7 padding, and Base64-encode it.
3. POST JSON containing `messageEnumCode`, `businessCode`, encrypted `mobile`, current millisecond `timestamp`, and `param.code`.
4. Use the existing tenant-aware HTTP client, require HTTP 2xx, parse JSON, and accept only an upstream `code` whose string form is `200`.
5. Return concise errors to Casdoor without logging secrets, retrying, storing messages, or introducing queues.

## Provider UI

Add `Samoyed SMS` to the SMS Provider type list. Creating it requires only the normal Provider identity fields and a test phone number. Hide credentials, sign name, template code, endpoint, proxy, and protocol-specific fields because they are global and fixed. Existing application-provider binding and verification flows remain unchanged.

## Cleanup

Remove the obsolete `sms-bridge/` directory, `compose.sms.yml`, and `SMS_BRIDGE.md`. Do not add dependencies or services.

## Validation

- Run an uncommitted in-process HTTP smoke test for AES output, request shape, and upstream business failures.
- Run Go compilation/tests and frontend typecheck/lint/build checks for the affected code.

## Acceptance criteria

- Runtime topology is only `Casdoor -> fixed SMS API`.
- The four upstream values are configured once per Casdoor deployment.
- Applications select the Provider without entering upstream details.
- Verification SMS succeeds only when the upstream returns HTTP 2xx and business code `200`.
- No bridge artifacts or extra runtime dependencies remain.
