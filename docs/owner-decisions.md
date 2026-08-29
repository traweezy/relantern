# Owner decisions

Status: approved for PR 0 on 2026-08-29.

| Decision | Selection |
|---|---|
| Product and repository name | Relantern / `traweezy/relantern` |
| Repository visibility | Private |
| Owner GitHub login and numeric ID | `traweezy` / `5276132` |
| Owner timezone | `America/New_York` |
| Production hostname | Railway-provided public hostname for bootstrap; custom domain deferred |
| Crawler contact | `https://github.com/traweezy` |
| Daily digest | 08:00 local time, every day, maximum 10 items |
| Weekly radar | Saturday at 09:00 local time |
| Catch-up and empty behavior | Six-hour grace; dashboard-only when empty |
| Quiet hours | 22:00–07:00; confirmed critical alerts may bypass |
| Default read behavior | Mark read after two seconds |
| Delivery | Private Discord only; email and Resend disabled |
| OpenAI cadence | One bounded daily AI research/synthesis batch before the digest |
| Emergency AI lane | Allowed only for confirmed critical watched-dependency security events |
| API budget | USD 25 monthly soft alert and USD 50 hard stop |
| Deep research limits | Three stories/day and ten/week; normal web research runs once/day |
| Source and retention policy | Specification defaults approved |
| Demo contact | `https://github.com/traweezy`; no case-study URL initially |
| Demo indexing | `noindex,nofollow`; synthetic fixtures require review |
| Hosted telemetry | Railway baseline initially; external OTLP deferred |
| Railway region | Closest stable US East region available to the project |

## OpenAI billing boundary

ChatGPT Pro and Codex usage are not assumed to fund API calls. Relantern uses a
separate OpenAI API project, key, usage ledger, and hard cost stop. The API
project is created later with independent staging and production credentials.

## Daily-work interpretation

The once-daily choice applies to AI web research and normal synthesis, not to
deterministic collection. Cheap feed/API polling, source-health accounting,
deduplication, and confirmed security metadata matching remain continuous so
the product can meet its freshness and critical-alert contracts.
