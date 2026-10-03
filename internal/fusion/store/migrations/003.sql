CREATE TABLE start_requests(key TEXT PRIMARY KEY,task_id TEXT NOT NULL REFERENCES tasks(id),payload_hash TEXT NOT NULL,run_id TEXT NOT NULL UNIQUE,FOREIGN KEY(run_id,task_id) REFERENCES stage_runs(id,task_id));
CREATE TRIGGER immutable_start_requests BEFORE UPDATE ON start_requests BEGIN SELECT RAISE(ABORT,'immutable start request'); END;
PRAGMA user_version=3;
