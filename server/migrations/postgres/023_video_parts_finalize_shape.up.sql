-- end_media_seq is only knowable at finalize time (Stage 5+). Holding a
-- placeholder during recording produces rows where end_media_seq ==
-- start_media_seq, indistinguishable from a legitimately zero-length
-- part. Making it nullable lets callers create a part at Stage 4 and
-- fill end_media_seq at FinalizeVideoPart. NULL = "not finalized."
ALTER TABLE video_parts ALTER COLUMN end_media_seq DROP NOT NULL;

-- Uniqueness of part filenames is already implied by videos.filename
-- UNIQUE + the "<base>-part<NN>" suffix convention. DB-enforcing it
-- is defense-in-depth: if the naming scheme ever changes, a collision
-- becomes a constraint violation at insert time instead of two rows
-- silently referencing the same file on storage.
ALTER TABLE video_parts ADD CONSTRAINT video_parts_filename_unique UNIQUE (filename);

-- updated_at gives operators a timestamp for when finalize landed.
-- FinalizeVideoPart rewrites duration/size/thumbnail/end_media_seq in
-- a single UPDATE; without updated_at there's no audit of when that
-- happened relative to the video row's downloaded_at.
ALTER TABLE video_parts ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
