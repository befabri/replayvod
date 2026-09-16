-- name: CreateRecordingIntent :exec
INSERT INTO recording_intents(id,broadcaster_id,params,wait_seconds,status,current_job_id,last_stream_id)
VALUES (sqlc.arg(id),sqlc.arg(broadcaster_id),sqlc.arg(params),sqlc.arg(wait_seconds),'active',sqlc.arg(current_job_id),sqlc.arg(last_stream_id)) ON CONFLICT(id) DO NOTHING;

-- name: GetRecordingIntent :one
SELECT * FROM recording_intents WHERE id=sqlc.arg(id);

-- name: LockRecordingIntent :one
UPDATE recording_intents SET id=id WHERE id=sqlc.arg(id) RETURNING *;

-- name: GetRecordingIntentByJob :one
SELECT recording_intents.* FROM recording_intents JOIN recording_intent_videos ON recording_intent_videos.intent_id=recording_intents.id JOIN jobs ON jobs.video_id=recording_intent_videos.video_id WHERE jobs.id=sqlc.arg(job_id);

-- name: ListRecoverableRecordingIntents :many
SELECT recording_intents.* FROM recording_intents WHERE recording_intents.id>sqlc.arg(after_id) AND (recording_intents.status IN ('active','waiting') OR EXISTS (SELECT 1 FROM recording_intent_videos r JOIN videos v ON v.id=r.video_id JOIN jobs j ON j.id=v.job_id WHERE r.intent_id=recording_intents.id AND j.status IN ('PENDING','RUNNING'))) ORDER BY recording_intents.id LIMIT sqlc.arg(limit);

-- name: SetRecordingIntentWaiting :execrows
UPDATE recording_intents SET status='waiting',wait_until=sqlc.arg(until) WHERE id=sqlc.arg(id) AND current_job_id=sqlc.arg(job_id) AND status='active' AND stop_requested=0;

-- name: ActivateRecordingIntent :execrows
UPDATE recording_intents SET status='active',current_job_id=sqlc.arg(next_job_id),last_stream_id=sqlc.arg(stream_id),wait_until=NULL WHERE id=sqlc.arg(id) AND current_job_id=sqlc.arg(previous_job_id) AND status='waiting' AND stop_requested=0 AND wait_until >= sqlc.arg(observed_at);

-- name: CloseRecordingIntent :exec
UPDATE recording_intents SET status=sqlc.arg(status),wait_until=NULL WHERE id=sqlc.arg(id);

-- name: RequestRecordingIntentStop :exec
UPDATE recording_intents SET stop_requested=1 WHERE id=sqlc.arg(id);

-- name: LinkRecordingIntentVideo :exec
INSERT INTO recording_intent_videos(intent_id,video_id,position,stream_id)
SELECT sqlc.arg(intent_id),sqlc.arg(video_id),coalesce(max(position),0)+1,sqlc.narg(stream_id) FROM recording_intent_videos WHERE intent_id=sqlc.arg(intent_id);

-- name: ListRecordingIntentJobs :many
SELECT jobs.* FROM jobs JOIN videos ON videos.id=jobs.video_id AND videos.job_id=jobs.id JOIN recording_intent_videos r ON r.video_id=videos.id
WHERE r.intent_id=sqlc.arg(intent_id) AND jobs.id>sqlc.arg(after_id) AND jobs.status IN ('RUNNING','PENDING') AND videos.deleted_at IS NULL ORDER BY jobs.id LIMIT sqlc.arg(limit);

-- name: ListRelatedRecordings :many
SELECT v.id,v.job_id,v.title,v.status,v.completion_kind,v.deleted_at,v.start_download_at,r.position FROM recording_intent_videos anchor
JOIN recording_intent_videos r ON r.intent_id=anchor.intent_id AND r.position BETWEEN anchor.position-5 AND anchor.position+5
JOIN videos v ON v.id=r.video_id WHERE anchor.video_id=sqlc.arg(video_id) ORDER BY r.position;
