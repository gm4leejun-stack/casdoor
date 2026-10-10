# Optional SMS bridge

The SMS bridge is an optional adapter between Casdoor's Custom HTTP SMS
provider and the existing Java `POST /NotifyService/sendMessage` endpoint. It
does not store data or retry requests. Casdoor's base Compose deployment does
not depend on it.

## Enable it

Set these values in a local `.env` file or deployment secret store:

```ini
SMS_BRIDGE_TOKEN=at-least-32-random-characters
SMS_UPSTREAM_URL=https://sms.example.com/NotifyService/sendMessage
SMS_BUSINESS_CODE=verification-business-code
SMS_MESSAGE_ENUM_CODE=sms-message-enum-code
SMS_CODE_PARAM=code
SMS_MOBILE_AES_KEY=the-literal-external.mobile.privateKey-value
SMS_UPSTREAM_TIMEOUT_MS=5000
```

`SMS_MOBILE_AES_KEY` must be the same literal UTF-8 key as the Java service's
`external.mobile.privateKey`; it must contain 16, 24, or 32 UTF-8 bytes. Do not
commit real credentials.

Start the base deployment normally when SMS is not needed:

```bash
docker compose up -d
```

Add the optional overlay only when SMS is enabled:

```bash
docker compose -f docker-compose.yml -f compose.sms.yml up -d --build
```

The overlay deliberately has no required-variable interpolation. Missing SMS
configuration can only make `sms-bridge` fail its own startup; it cannot block
Compose from starting the base Casdoor services.

## Configure Casdoor

Create an SMS provider in the Casdoor console with these values:

- Type: `Custom HTTP SMS`
- Method: `POST`
- Endpoint: `http://sms-bridge:8080/send`
- Content type: `application/json`
- Parameter: `code`
- Template code: `%s`
- Header: `Authorization: Bearer <SMS_BRIDGE_TOKEN>`
- Body mappings: `Phone -> mobile`, `Content -> code`

Test the provider before enabling phone verification for an application.

## Verify the adapter

```bash
node --test sms-bridge/server.test.mjs
```
