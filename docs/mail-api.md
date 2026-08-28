# Mail API

`POST /api/mail` accepts one authenticated service-to-service mail intent and
returns after the intent has been durably claimed and accepted by the Knative
Broker. It does not wait for SMTP delivery.

Before claiming the intent, Chaos loads the enabled `template_id` and performs
the same strict subject/body render used by the SMTP consumer. Missing template
variables, invalid template syntax, disabled templates, and unsafe rendered
subjects are rejected without publishing an event.

## Authentication

The bearer token must be an Aegis Service Access Token whose audience is
Chaos. User access tokens are rejected with `403 SERVICE_ACCESS_REQUIRED`.

## Request

The `Idempotency-Key` header is required. A caller must reuse the same key when
retrying one logical mail intent and use a different key for a different
intent. Challenge-backed messages should use the stable challenge ID.

```http
POST /api/mail HTTP/1.1
Authorization: Bearer <service-access-token>
Idempotency-Key: challenge-01K...
Content-Type: application/json

{
  "to": "user@example.com",
  "template_id": "otp_login",
  "variables": {
    "code": "123456"
  },
  "expires_at": "2026-08-28T12:05:00Z"
}
```

The accepted fields are:

| Field | Required | Constraint |
| --- | --- | --- |
| `to` | yes | Plain email address; display names are rejected |
| `template_id` | yes | Lowercase letters, digits, `_` or `-`; maximum 64 characters |
| `subject` | no | Maximum 998 characters; CR/LF is rejected |
| `variables` | no | JSON object used for template rendering; maximum encoded size 32 KiB |
| `expires_at` | no | Must be in the next 24 hours; defaults to 24 hours |

Unknown fields, trailing JSON values, and bodies larger than 64 KiB are
rejected. `data` remains a compatibility alias for `variables`; callers must
not send both.

## Response

The first accepted request returns `202 Accepted`:

```json
{
  "ok": true,
  "delivery_id": "5dd253af-178b-480d-a1dc-ab42cc5e4d6d"
}
```

Replaying an accepted request with the same idempotency key and identical body
returns `202` with the same `delivery_id` without publishing another event.
Reusing the key with a different body returns `422 IDEMPOTENCY_KEY_REUSED`. A
concurrent duplicate returns `409 MAIL_REQUEST_IN_PROGRESS` with
`Retry-After: 1`.

Template validation failures use these responses:

| Status | Code | Meaning |
| --- | --- | --- |
| `404` | `MAIL_TEMPLATE_NOT_FOUND` | `template_id` does not exist |
| `409` | `MAIL_TEMPLATE_DISABLED` | Template exists but is disabled |
| `422` | `MAIL_TEMPLATE_INVALID` | Stored subject or body template is invalid |
| `422` | `MAIL_TEMPLATE_DATA_MISMATCH` | Request variables cannot render the subject or body |

## Delivery semantics

Chaos signs the private delivery payload and publishes it as a CloudEvent.
Knative and RabbitMQ provide at-least-once delivery to the internal consumer.
The API's idempotency record prevents duplicate publication caused by normal
caller retries; it does not claim exactly-once SMTP delivery across every
possible process crash.

The idempotency table is additive and created by the existing GORM
`AutoMigrate` startup path. Rollback is performed by deploying the previous
binary; the unused table can remain in place so rollback does not discard
retry history.
