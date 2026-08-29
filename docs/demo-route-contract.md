# Anonymous demo route contract

Status: route policy and synthetic fixture presentation approved for PR 12.

The public route matrix for Version 1 contains only:

| Method | Route | Data authority |
|---|---|---|
| GET | `/healthz` | Static process health |
| GET | `/demo` | Reviewed immutable synthetic fixture module |
| GET | `/demo/story/{known_fixture_id}` | Reviewed immutable synthetic fixture lookup |

All demo responses use `noindex,nofollow`. Unknown fixture IDs return a demo-only
not-found response and cannot fall through into protected routing.

## Forbidden import and request graph

Modules below `apps/web/src/app/demo` may import only demo-local fixtures,
presentation components, `@relantern/domain`, and `@relantern/design-tokens`.
They must not import authentication, the generated API client, server actions,
private environment access, database code, OpenAI, Discord, email, object
storage, SSE, or any `/api/v1` or `/api/auth` client. Demo interactions remain
browser-local and resettable.

Strict nonce-based CSP requires request-time document rendering. In this
contract, "static" describes the immutable fixture data and isolated import
graph, not static HTML. Owner-session state cannot influence demo content; the
only expected response-byte difference is the per-request CSP nonce.
