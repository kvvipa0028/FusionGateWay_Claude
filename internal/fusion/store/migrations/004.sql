CREATE TABLE preset_revisions(project_id TEXT NOT NULL,id TEXT NOT NULL,revision INTEGER NOT NULL CHECK(revision>0),name TEXT NOT NULL,layer_json TEXT NOT NULL,hash TEXT NOT NULL,created_at INTEGER NOT NULL,PRIMARY KEY(project_id,id,revision),UNIQUE(project_id,id,revision,hash));
CREATE TRIGGER immutable_preset_revisions BEFORE UPDATE ON preset_revisions BEGIN SELECT RAISE(ABORT,'immutable preset revision'); END;
CREATE TRIGGER preserve_preset_revisions BEFORE DELETE ON preset_revisions BEGIN SELECT RAISE(ABORT,'preserve preset history'); END;
CREATE TABLE preset_heads(project_id TEXT NOT NULL,id TEXT NOT NULL,revision INTEGER NOT NULL,PRIMARY KEY(project_id,id),FOREIGN KEY(project_id,id,revision) REFERENCES preset_revisions(project_id,id,revision));
CREATE UNIQUE INDEX tasks_project_identity ON tasks(id,project_id);
CREATE TABLE task_preset_refs(task_id TEXT PRIMARY KEY,project_id TEXT NOT NULL,id TEXT NOT NULL,revision INTEGER NOT NULL,hash TEXT NOT NULL,FOREIGN KEY(task_id,project_id) REFERENCES tasks(id,project_id),FOREIGN KEY(project_id,id,revision,hash) REFERENCES preset_revisions(project_id,id,revision,hash));
CREATE TRIGGER immutable_task_preset_refs BEFORE UPDATE ON task_preset_refs BEGIN SELECT RAISE(ABORT,'immutable task preset'); END;
PRAGMA user_version=4;
