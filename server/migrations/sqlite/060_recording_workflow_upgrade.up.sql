-- Convert the old single-file representation once. Application playback and
-- deletion now use part rows exclusively, including for historical recordings.
INSERT INTO video_parts(video_id,part_index,filename,quality,codec,segment_format,duration_seconds,size_bytes,thumbnail,start_media_seq,end_media_seq)
SELECT v.id,1,v.filename||'.mp4',coalesce(v.selected_quality,v.quality),CASE WHEN v.recording_type='audio' THEN 'aac' ELSE 'h264' END,'fmp4',coalesce(v.duration_seconds,0),coalesce(v.size_bytes,0),v.thumbnail,0,NULL
FROM videos v WHERE (v.status='DONE' OR (v.status='FAILED' AND v.size_bytes>0))
AND (v.deleted_at IS NULL OR v.deletion_kind='missing')
AND NOT EXISTS(SELECT 1 FROM video_parts p WHERE p.video_id=v.id);

-- Normalize stored checkpoints, including sequence zero. SEGMENTS with a zero
-- frontier may precede its first fetch; re-reading sequence zero is safe and
-- preserves any saved segment, without guessing from the filesystem at runtime.
UPDATE jobs SET resume_state=json_set(resume_state,
 '$.stage',coalesce(json_extract(resume_state,'$.stage'),'AUTH'),
 '$.current_part_index',max(coalesce(json_extract(resume_state,'$.current_part_index'),1),1),
 '$.part_started',json(CASE WHEN coalesce(json_extract(resume_state,'$.part_started'),0)=1
 OR coalesce(json_extract(resume_state,'$.part_start_media_sequence'),0)<>0
 OR coalesce(json_extract(resume_state,'$.accounted_frontier_media_sequence'),0)<>0
 OR coalesce(json_extract(resume_state,'$.part_bytes'),0)>0
 OR coalesce(json_array_length(resume_state,'$.completed_above_frontier'),0)>0
 OR coalesce(json_array_length(resume_state,'$.gaps'),0)>0
 OR json_extract(resume_state,'$.stage') IN ('PREPARE_INPUT','REMUX','PROBE','THUMBNAIL','CORRUPTION_CHECK','STORE')
 THEN 'true' ELSE 'false' END))
WHERE json_valid(resume_state) AND json_type(resume_state)='object'
 AND coalesce(json_type(resume_state,'$.stage'),'text') IN ('text','null')
 AND coalesce(json_type(resume_state,'$.current_part_index'),'integer') IN ('integer','real','null')
 AND coalesce(json_type(resume_state,'$.part_start_media_sequence'),'integer') IN ('integer','real','null')
 AND coalesce(json_type(resume_state,'$.accounted_frontier_media_sequence'),'integer') IN ('integer','real','null')
 AND coalesce(json_type(resume_state,'$.part_bytes'),'integer') IN ('integer','real','null')
 AND coalesce(json_type(resume_state,'$.part_started'),'false') IN ('true','false','null');

-- Existing cache rows supply their actual output keys. Retention never guesses
-- a derived filename after the upgrade.
INSERT INTO media_publications(key,video_id,digest,size_bytes)
SELECT 'videos/'||filename,video_id,'',coalesce(size_bytes,0) FROM video_playback_assets WHERE filename IS NOT NULL
ON CONFLICT(key) DO NOTHING;
