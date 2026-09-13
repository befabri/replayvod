-- Released single-file rows may own bytes before they have part references.
INSERT INTO videos(id,job_id,filename,display_name,broadcaster_id,status,size_bytes,duration_seconds,truncated) VALUES
 (8008,'old-single-failure','old-single-failure','Saved single file','2001','FAILED',2048,12.5,TRUE),
 (8009,'old-pending-orphan','old-pending-orphan','Orphan single file','2001','PENDING',4096,25,FALSE),
 (8010,'old-running-orphan','old-running-orphan','Running orphan single file','2001','RUNNING',8192,50,FALSE);
