# Anonymous demo route contract

Status: route policy approved; reviewed synthetic fixture snapshot not yet approved.

The public route matrix for Version 1 contains only:

| Method | Route | Data authority |
|---|---|---|
| GET | `/healthz` | Static process health |
| GET | `/demo` | Reviewed build-time synthetic fixtures |
| GET | `/demo/story/{known_fixture_id}` | Reviewed build-time synthetic fixture lookup |

All demo responses use `noindex,nofollow`. Unknown fixture IDs return a demo-only
not-found response and cannot fall through into protected routing.

## Forbidden import and request graph

Modules below `apps/web/src/app/demo` may import only demo-local fixtures,
presentation components, `@relantern/domain`, and `@relantern/design-tokens`.
They must not import authentication, the generated API client, server actions,
private environment access, database code, OpenAI, Discord, email, object
storage, SSE, or any `/api/v1` or `/api/auth` client. Demo interactions remain
browser-local and resettable.

The fixture snapshot, contact link, copy, and visual presentation require owner
review before the product demo is implemented. Until then `/demo` is a static
foundation placeholder with no data dependency.
