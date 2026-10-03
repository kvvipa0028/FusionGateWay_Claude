CREATE TABLE controller_policy(id INTEGER PRIMARY KEY CHECK(id=1),max_active INTEGER NOT NULL CHECK(max_active BETWEEN 1 AND 16));
INSERT INTO controller_policy VALUES(1,2);
CREATE TABLE task_budgets(task_id TEXT PRIMARY KEY REFERENCES tasks(id),max_calls INTEGER NOT NULL CHECK(max_calls BETWEEN 1 AND 1000),max_reworks INTEGER NOT NULL CHECK(max_reworks BETWEEN 0 AND 1),used_calls INTEGER NOT NULL DEFAULT 0 CHECK(used_calls BETWEEN 0 AND max_calls),used_reworks INTEGER NOT NULL DEFAULT 0 CHECK(used_reworks BETWEEN 0 AND max_reworks));
CREATE TABLE reservations(run_id TEXT PRIMARY KEY REFERENCES stage_runs(id),pool_key TEXT NOT NULL,write_key TEXT NOT NULL DEFAULT '',state TEXT NOT NULL CHECK(state IN ('held','released')),admission_hash TEXT NOT NULL,stop_proof_hash TEXT NOT NULL DEFAULT '');
CREATE UNIQUE INDEX one_active_pool ON reservations(pool_key) WHERE state='held';
CREATE UNIQUE INDEX one_active_writer ON reservations(write_key) WHERE state='held' AND write_key<>'';
PRAGMA user_version=2;
