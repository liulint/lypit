package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	_ "modernc.org/sqlite"
)

var errConflict = errors.New("内容已变化，请刷新后重试")

type Store struct {
	db   *sql.DB
	mu   sync.Mutex
	lock *os.File
}

func openStore(dir string) (*Store, error) {
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	l, e := os.OpenFile(filepath.Join(dir, "studio.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	if e = lockStoreFile(l); e != nil {
		l.Close()
		return nil, errors.New("此数据目录正在被另一个服务使用")
	}
	path := filepath.ToSlash(filepath.Join(dir, "studio.db"))
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	uri := url.URL{Scheme: "file", Path: path}
	params := url.Values{"_pragma": {"journal_mode(WAL)", "busy_timeout(5000)", "foreign_keys(1)", "synchronous(FULL)"}}
	uri.RawQuery = params.Encode()
	db, e := sql.Open("sqlite", uri.String())
	if e != nil {
		l.Close()
		return nil, e
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, lock: l}
	var version int
	if e = db.QueryRow("PRAGMA user_version").Scan(&version); e != nil {
		s.close()
		return nil, e
	}
	if version > dataSchemaVersion {
		s.close()
		return nil, fmt.Errorf("数据版本 %d 高于本程序支持的版本 %d，请使用新版程序；不会降级或重建数据", version, dataSchemaVersion)
	}
	if version < dataSchemaVersion {
		var tables int
		if e = db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&tables); e != nil {
			s.close()
			return nil, e
		}
		if tables > 0 {
			if _, e = backupUserData(db, dir); e != nil {
				s.close()
				return nil, e
			}
		}
	}
	e = s.transaction(func(tx *sql.Tx) error {
		_, err := tx.Exec(`CREATE TABLE IF NOT EXISTS projects(id TEXT PRIMARY KEY, body TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS settings(id INTEGER PRIMARY KEY CHECK(id=1), body TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS quotes(id TEXT PRIMARY KEY, project_id TEXT NOT NULL REFERENCES projects(id), body TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS tasks(id TEXT PRIMARY KEY, project_id TEXT NOT NULL REFERENCES projects(id), status TEXT NOT NULL, body TEXT NOT NULL, input TEXT NOT NULL, config TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS tasks_project ON tasks(project_id,status);
CREATE TABLE IF NOT EXISTS attempts(id TEXT PRIMARY KEY, task_id TEXT NOT NULL REFERENCES tasks(id), status TEXT NOT NULL, response BLOB, remote_id TEXT NOT NULL DEFAULT '');
CREATE TABLE IF NOT EXISTS history(id INTEGER PRIMARY KEY AUTOINCREMENT, project_id TEXT NOT NULL REFERENCES projects(id), revision INTEGER NOT NULL, body TEXT NOT NULL, UNIQUE(project_id,revision));
PRAGMA user_version=1;`)
		return err
	})
	if e != nil {
		s.close()
		return nil, e
	}
	_, e = db.Exec("INSERT OR IGNORE INTO settings VALUES(1,?)", jsonBytes(defaultSettings()))
	if e != nil {
		s.close()
		return nil, e
	}
	// A process interruption never silently resubmits an accepted request.
	jobs, e := s.tasks("")
	if e != nil {
		s.close()
		return nil, e
	}
	for _, t := range jobs {
		if t.Status == "running" || t.Status == "queued" {
			t.Status = "interrupted"
			t.Message = "服务已重启，请恢复原任务；已提交的请求不会自动重发"
			if e = s.saveTask(t); e != nil {
				s.close()
				return nil, e
			}
		}
	}
	return s, nil
}
func (s *Store) close() error {
	e := s.db.Close()
	_ = unlockStoreFile(s.lock)
	_ = s.lock.Close()
	return e
}
func (s *Store) project(id string) (Project, error) {
	var p Project
	var b []byte
	e := s.db.QueryRow("SELECT body FROM projects WHERE id=?", id).Scan(&b)
	if e != nil {
		return p, e
	}
	e = json.Unmarshal(b, &p)
	return p, e
}
func (s *Store) projects() ([]Project, error) {
	rows, e := s.db.Query("SELECT body FROM projects ORDER BY rowid DESC")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Project{}
	for rows.Next() {
		var b []byte
		var p Project
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		if e = json.Unmarshal(b, &p); e != nil {
			return nil, e
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (s *Store) settings() (Settings, error) {
	v := defaultSettings()
	var b []byte
	e := s.db.QueryRow("SELECT body FROM settings WHERE id=1").Scan(&b)
	if e == nil {
		e = json.Unmarshal(b, &v)
	}
	return upgradeCurrentRegistry(withVendorRegistry(v)), e
}
func (s *Store) saveProject(tx *sql.Tx, p Project) error {
	if _, e := tx.Exec("INSERT INTO projects(id,body) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body", p.ID, jsonBytes(p)); e != nil {
		return e
	}
	_, e := tx.Exec("INSERT INTO history(project_id,revision,body) VALUES(?,?,?)", p.ID, p.Revision, jsonBytes(p))
	return e
}
func (s *Store) task(id string) (Task, error) {
	var t Task
	var body, input, config []byte
	e := s.db.QueryRow("SELECT body,input,config FROM tasks WHERE id=?", id).Scan(&body, &input, &config)
	if e != nil {
		return t, e
	}
	if e = json.Unmarshal(body, &t); e != nil {
		return t, e
	}
	if e = json.Unmarshal(input, &t.Input); e != nil {
		return t, e
	}
	e = json.Unmarshal(config, &t.Config)
	return t, e
}
func (s *Store) tasks(pid string) ([]Task, error) {
	rows, e := s.db.Query("SELECT body FROM tasks WHERE (?='' OR project_id=?) ORDER BY rowid DESC", pid, pid)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Task{}
	for rows.Next() {
		var b []byte
		var t Task
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		if e = json.Unmarshal(b, &t); e != nil {
			return nil, e
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
func (s *Store) saveTask(t Task) error {
	t.Updated = now()
	_, e := s.db.Exec("UPDATE tasks SET status=?,body=? WHERE id=?", t.Status, jsonBytes(t), t.ID)
	return e
}
func (s *Store) busy(pid string) bool {
	var count int
	e := s.db.QueryRow("SELECT count(*) FROM tasks WHERE project_id=? AND status IN ('running','queued','interrupted','failed','cancelled')", pid).Scan(&count)
	return e != nil || count > 0
}
func (s *Store) transaction(fn func(*sql.Tx) error) error {
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = fn(tx); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) update(id string, revision int, fn func(*Project) error) (Project, error) {
	return s.updateScoped(id, revision, false, fn)
}
func (s *Store) updateScoped(id string, revision int, review bool, fn func(*Project) error) (Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, e := s.project(id)
	if e != nil {
		return p, e
	}
	if p.Revision != revision {
		return p, errConflict
	}
	if s.operationBusy(id, review) {
		return p, errors.New("请先完成、恢复或放弃当前任务")
	}
	if e = fn(&p); e != nil {
		return p, e
	}
	p.Revision++
	p.Updated = now()
	e = s.transaction(func(tx *sql.Tx) error { return s.saveProject(tx, p) })
	return p, e
}
func decodeBody[T any](b []byte) (T, error) { var v T; e := json.Unmarshal(b, &v); return v, e }
func (s *Store) quote(id string) (Quote, error) {
	var b []byte
	e := s.db.QueryRow("SELECT body FROM quotes WHERE id=?", id).Scan(&b)
	if e != nil {
		return Quote{}, e
	}
	return decodeBody[Quote](b)
}
func (s *Store) history(id string) ([]Project, error) {
	rows, e := s.db.Query("SELECT body FROM history WHERE project_id=? ORDER BY revision DESC LIMIT 50", id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Project{}
	for rows.Next() {
		var b []byte
		var p Project
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		if e = json.Unmarshal(b, &p); e != nil {
			return nil, e
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (s *Store) create(title, idea, style, aspect string, duration int, modes ...string) (Project, error) {
	if e := validateSpec(aspect, duration); e != nil {
		return Project{}, e
	}
	mode := "dialogue"
	if len(modes) > 0 {
		mode = modes[0]
	}
	if e := validateAudioMode(mode); e != nil {
		return Project{}, e
	}
	p := Project{AudioMode: mode, ID: uid(), Title: title, Idea: idea, Style: style, AspectRatio: aspect, Duration: duration, Revision: 1, Created: now(), Updated: now(), Assets: []Asset{}, Shots: []Shot{}, Mode: "mock"}
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.transaction(func(tx *sql.Tx) error { return s.saveProject(tx, p) })
	return p, e
}
func dbError(e error) error {
	if errors.Is(e, sql.ErrNoRows) {
		return fmt.Errorf("项目或记录不存在")
	}
	return e
}
