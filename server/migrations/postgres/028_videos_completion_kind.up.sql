-- completion_kind separates two axes that the status column conflates:
--   status          — did the pipeline succeed? (PENDING/RUNNING/DONE/FAILED)
--   completion_kind — is the recorded content whole? (complete/partial/cancelled)
--
-- Without this split a DONE video can mean "clean-end recording" or
-- "shutdown mid-record + resume missed the CDN window" — both show as
-- a green badge. Operators can't tell at a glance.
--
-- Values:
--   complete  — clean end, full recording. Default for new rows.
--   partial   — Stage 11 completed but resume_state.gaps contained at
--               least one restart_window_rolled entry (we lost data
--               the CDN rolled past before we restarted).
--   cancelled — operator called Cancel(); video row is marked FAILED
--               with ErrCancelled. The UI uses completion_kind to
--               render a grey "CANCELLED" instead of red "FAILED".
--
-- Existing rows back-fill with 'complete' — a wrong assumption for
-- resumed-partial recordings but acceptable since this feature ships
-- with migrating production data we can't classify after the fact.
ALTER TABLE videos ADD COLUMN completion_kind TEXT NOT NULL DEFAULT 'complete';

-- CHECK as a separate statement for dialect symmetry — sqlite's
-- migration rebuilds can test the same shape.
ALTER TABLE videos ADD CONSTRAINT videos_completion_kind_chk
    CHECK (completion_kind IN ('complete', 'partial', 'cancelled'));
