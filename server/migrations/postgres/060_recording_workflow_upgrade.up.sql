-- Historical single-file recordings become ordinary one-part recordings.
INSERT INTO video_parts(video_id,part_index,filename,quality,codec,segment_format,duration_seconds,size_bytes,thumbnail,start_media_seq,end_media_seq)
SELECT v.id,1,v.filename||'.mp4',coalesce(v.selected_quality,v.quality),CASE WHEN v.recording_type='audio' THEN 'aac' ELSE 'h264' END,'fmp4',coalesce(v.duration_seconds,0),coalesce(v.size_bytes,0),v.thumbnail,0,NULL
FROM videos v WHERE (v.status='DONE' OR (v.status='FAILED' AND v.size_bytes>0))
AND (v.deleted_at IS NULL OR v.deletion_kind='missing')
AND NOT EXISTS(SELECT 1 FROM video_parts p WHERE p.video_id=v.id);

UPDATE jobs SET resume_state=resume_state || jsonb_build_object(
 'stage',coalesce(resume_state->>'stage','AUTH'),
 'current_part_index',greatest(coalesce((resume_state->>'current_part_index')::numeric,1),1),
 'part_started',coalesce((resume_state->>'part_started')::boolean,false)
 OR coalesce((resume_state->>'part_start_media_sequence')::numeric,0)<>0
 OR coalesce((resume_state->>'accounted_frontier_media_sequence')::numeric,0)<>0
 OR coalesce((resume_state->>'part_bytes')::numeric,0)>0
 OR CASE WHEN jsonb_typeof(resume_state->'completed_above_frontier')='array' THEN jsonb_array_length(resume_state->'completed_above_frontier') ELSE 0 END>0
 OR CASE WHEN jsonb_typeof(resume_state->'gaps')='array' THEN jsonb_array_length(resume_state->'gaps') ELSE 0 END>0
 OR coalesce(resume_state->>'stage' IN ('PREPARE_INPUT','REMUX','PROBE','THUMBNAIL','CORRUPTION_CHECK','STORE'),false))
WHERE jsonb_typeof(resume_state)='object'
 AND coalesce(jsonb_typeof(resume_state->'stage'),'string') IN ('string','null')
 AND coalesce(jsonb_typeof(resume_state->'current_part_index'),'number') IN ('number','null')
 AND coalesce(jsonb_typeof(resume_state->'part_start_media_sequence'),'number') IN ('number','null')
 AND coalesce(jsonb_typeof(resume_state->'accounted_frontier_media_sequence'),'number') IN ('number','null')
 AND coalesce(jsonb_typeof(resume_state->'part_bytes'),'number') IN ('number','null')
 AND coalesce(jsonb_typeof(resume_state->'part_started'),'boolean') IN ('boolean','null');

-- Existing cache rows supply their actual output keys. Retention never guesses
-- a derived filename after the upgrade.
INSERT INTO media_publications(key,video_id,digest,size_bytes)
SELECT 'videos/'||filename,video_id,'',coalesce(size_bytes,0) FROM video_playback_assets WHERE filename IS NOT NULL
ON CONFLICT(key) DO NOTHING;
