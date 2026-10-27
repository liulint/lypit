package main

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Store struct {
	mu    sync.Mutex
	db    *sql.DB
	lock  *os.File
	state State
}

func now() string { return time.Now().UTC().Format(time.RFC3339) }
func uid() string {
	b := make([]byte, 12)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func hash(v any) string {
	b, _ := json.Marshal(v)
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}
func clone[T any](v T) T { b, _ := json.Marshal(v); var out T; _ = json.Unmarshal(b, &out); return out }
func defaults() Settings {
	zero := 0.0
	return Settings{Providers: []Provider{{ID: "demo", Name: "模拟服务 · 不调用付费模型", Adapter: "mock", Models: []Model{{ID: "demo-v1", Capabilities: []string{"text", "vision", "image", "video", "tts"}, ReferenceImages: true, MaxReferences: 9, Structured: true, Audio: true, Sizes: []string{"1024x1536", "480p"}, MinDuration: 4, MaxDuration: 15, Price: &zero, Unit: "request", PriceSource: "本地模拟，无费用", Verified: "2026-10-21", Voice: "demo"}}}}, Bindings: map[string]Binding{"text": {"demo", "demo-v1"}, "vision": {"demo", "demo-v1"}, "image": {"demo", "demo-v1"}, "video": {"demo", "demo-v1"}, "tts": {"demo", "demo-v1"}}}
}
func openStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	lock, lockErr := os.OpenFile(filepath.Join(dir, "studio.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if lockErr != nil {
		return nil, lockErr
	}
	if lockErr = unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); lockErr != nil {
		lock.Close()
		return nil, fmt.Errorf("数据目录已由另一个后端进程使用")
	}
	keepLock := false
	defer func() {
		if !keepLock {
			lock.Close()
		}
	}()
	db, e := sql.Open("sqlite3", filepath.Join(dir, "studio.db")+"?_journal_mode=WAL&_busy_timeout=5000&_synchronous=FULL")
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	if _, e = db.Exec("CREATE TABLE IF NOT EXISTS state (id INTEGER PRIMARY KEY CHECK(id=1), body TEXT NOT NULL)"); e != nil {
		db.Close()
		return nil, e
	}
	s := &Store{db: db, lock: lock, state: State{FormatVersion: stateVersion, Settings: defaults(), Projects: map[string]*Project{}, Quotes: map[string]StoredQuote{}}}
	var body []byte
	e = db.QueryRow("SELECT body FROM state WHERE id=1").Scan(&body)
	if e == nil {
		s.state = State{}
		e = json.Unmarshal(body, &s.state)
	} else if e == sql.ErrNoRows {
		e = s.saveLocked()
	}
	if e != nil {
		db.Close()
		return nil, e
	}
	if s.state.FormatVersion > stateVersion {
		db.Close()
		return nil, fmt.Errorf("数据格式较新，不能由当前程序覆盖")
	}
	if s.state.FormatVersion < stateVersion {
		if e = backupBeforeMigration(s, dir); e != nil {
			db.Close()
			return nil, e
		}
		if e = migrateState(&s.state); e != nil {
			db.Close()
			return nil, e
		}
		migrated, err := json.Marshal(s.state)
		if err != nil {
			db.Close()
			return nil, err
		}
		tx, err := db.Begin()
		if err != nil {
			db.Close()
			return nil, err
		}
		if _, err = tx.Exec("UPDATE state SET body=? WHERE id=1", migrated); err != nil {
			tx.Rollback()
			db.Close()
			return nil, err
		}
		if err = tx.Commit(); err != nil {
			db.Close()
			return nil, err
		}
	}
	for id, probe := range s.state.Probes {
		if probe.State == "running" {
			probe.State = "interrupted"
			probe.Error = "服务中断；只允许恢复已保存响应，不重提 CLI"
			s.state.Probes[id] = probe
		}
	}
	for _, p := range s.state.Projects {
		if p.Status == "running" {
			p.Status = "interrupted"
			p.Error = "服务中断；恢复仅继续未提交工作和已知任务，不会重提未知请求"
			for i := range p.Tasks {
				if p.Tasks[i].State == "running" {
					p.Tasks[i].State = "interrupted"
				}
			}
		}
	}
	if e = s.saveLocked(); e != nil {
		db.Close()
		return nil, e
	}
	keepLock = true
	return s, nil
}
func (s *Store) saveLocked() error {
	b, e := json.Marshal(s.state)
	if e != nil {
		return e
	}
	_, e = s.db.Exec("INSERT INTO state(id,body) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body", b)
	return e
}

// Roll back the in-memory state too when durable persistence fails.
func (s *Store) change(fn func(*State) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	before := clone(s.state)
	if e := fn(&s.state); e != nil {
		s.state = before
		return e
	}
	if e := s.saveLocked(); e != nil {
		s.state = before
		return e
	}
	return nil
}
func (s *Store) project(id string) (Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.state.Projects[id]
	if p == nil {
		return Project{}, fmt.Errorf("项目不存在")
	}
	return clone(*p), nil
}
func (s *Store) settings() Settings { s.mu.Lock(); defer s.mu.Unlock(); return clone(s.state.Settings) }
func (s *Store) update(id string, fn func(*Project)) error {
	return s.change(func(st *State) error {
		p := st.Projects[id]
		if p == nil {
			return fmt.Errorf("项目不存在")
		}
		fn(p)
		return nil
	})
}
func board(p Project) *Board {
	if len(p.Boards) == 0 {
		return nil
	}
	b := p.Boards[len(p.Boards)-1]
	return &b
}

func (s *Store) close() error {
	err := s.db.Close()
	if s.lock != nil {
		_ = s.lock.Close()
	}
	return err
}
