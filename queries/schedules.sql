-- name: ListDueScheduleIDs :many
select id
from app.schedule_definitions
where enabled
  and paused_at is null
  and next_due_at <= sqlc.arg(due_at)
order by next_due_at, id
limit sqlc.arg(result_limit);
