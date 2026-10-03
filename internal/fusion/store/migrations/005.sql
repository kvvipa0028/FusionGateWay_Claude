CREATE TABLE default_layer_revisions(scope TEXT NOT NULL CHECK(scope IN ('global','project')),id TEXT NOT NULL CHECK(scope!='global' OR id='global'),revision INTEGER NOT NULL CHECK(revision>0),layer_json TEXT NOT NULL,hash TEXT NOT NULL,created_at INTEGER NOT NULL,PRIMARY KEY(scope,id,revision));
CREATE TRIGGER immutable_default_layers BEFORE UPDATE ON default_layer_revisions BEGIN SELECT RAISE(ABORT,'immutable defaults revision'); END;
CREATE TRIGGER preserve_default_layers BEFORE DELETE ON default_layer_revisions BEGIN SELECT RAISE(ABORT,'preserve defaults history'); END;
CREATE TABLE default_layer_heads(scope TEXT NOT NULL,id TEXT NOT NULL,revision INTEGER NOT NULL,PRIMARY KEY(scope,id),FOREIGN KEY(scope,id,revision) REFERENCES default_layer_revisions(scope,id,revision));
CREATE TRIGGER preserve_default_heads BEFORE DELETE ON default_layer_heads BEGIN SELECT RAISE(ABORT,'use explicit inherit revision'); END;
PRAGMA user_version=5;
