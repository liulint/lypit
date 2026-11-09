package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/gin-gonic/gin"
)

const dramaPackageLimit = 500 << 20
const dramaPackageManifest = "lypit-work.json"

type dramaPackageMedia struct {
	Blob  string     `json:"blob"`
	Media DramaMedia `json:"media"`
}
type dramaPackage struct {
	Format    string              `json:"format"`
	Version   int                 `json:"version"`
	Work      DramaWork           `json:"work"`
	Documents map[string]string   `json:"documents"`
	Media     []dramaPackageMedia `json:"media"`
}

// Construct a public content snapshot; never serialize jobs, provider snapshots or credentials.
func portableDramaWork(w DramaWork) DramaWork {
	return DramaWork{Title: w.Title, Idea: w.Idea, Genre: w.Genre, Form: w.Form,
		Aspect: w.Aspect, Duration: w.Duration, Language: w.Language, PromptLanguage: w.PromptLanguage,
		EpisodeNumber: w.EpisodeNumber, Plan: w.Plan}
}
func portableDramaMedia(m DramaMedia) DramaMedia {
	return DramaMedia{Entry: m.Entry, Kind: m.Kind, Path: m.Path, Hash: m.Hash,
		Duration: m.Duration, Mock: m.Mock, Category: m.Category, ReferenceOnly: m.ReferenceOnly}
}

func (s *Studio) exportDramaPackage(id string) ([]byte, error) {
	w, err := s.dramaWork(id)
	if err != nil {
		return nil, err
	}
	p := dramaPackage{Format: "lypit-work", Version: 1, Work: portableDramaWork(w), Documents: map[string]string{}}
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	total := 0
	add := func(name string, b []byte) error {
		total += len(b)
		if total > dramaPackageLimit {
			return errors.New("作品包超过500MB")
		}
		f, e := z.Create(name)
		if e != nil {
			return e
		}
		_, e = f.Write(b)
		return e
	}
	for kind := range dramaPaths {
		d, e := s.dramaDoc(id, kind)
		if e != nil {
			return nil, e
		}
		if d.Text == "" {
			continue
		}
		name := "documents/" + kind + ".md"
		p.Documents[kind] = name
		if e = add(name, []byte(d.Text)); e != nil {
			return nil, e
		}
	}
	media, err := s.dramaMedias(id)
	if err != nil {
		return nil, err
	}
	for i, m := range media {
		if err = s.verifyDramaMedia(m); err != nil {
			return nil, err
		}
		b, e := os.ReadFile(s.media(m.File))
		if e != nil {
			return nil, e
		}
		name := fmt.Sprintf("media/%04d%s", i, path.Ext(m.File))
		p.Media = append(p.Media, dramaPackageMedia{Blob: name, Media: portableDramaMedia(m)})
		if e = add(name, b); e != nil {
			return nil, e
		}
	}
	if len(p.Media)+len(p.Documents)+1 > 2048 || len(jsonBytes(p)) > 1<<20 {
		return nil, errors.New("作品包清单过大，请减少素材数量")
	}
	if err = add(dramaPackageManifest, jsonBytes(p)); err != nil {
		return nil, err
	}
	if err = z.Close(); err != nil {
		return nil, err
	}
	if buf.Len() > dramaPackageLimit {
		return nil, errors.New("压缩后的作品包超过500MB")
	}
	return buf.Bytes(), nil
}

func packagePath(name string) bool {
	return name != "" && name != "." && path.Clean(name) == name && !strings.HasPrefix(name, "/") &&
		!strings.ContainsAny(name, "\\:\x00") && !strings.HasPrefix(name, "../") && len(name) < 1024
}

// Validate the complete archive before writing anything. Only listed payloads are read;
// archive paths are never used as filesystem extraction destinations.
func (s *Studio) importDramaPackage(id, hash string, z *zip.Reader) (DramaWork, error) {
	var empty DramaWork
	if !dramaID.MatchString(id) {
		return empty, errors.New("导入操作标识无效")
	}
	var prior string
	err := s.store.db.QueryRow("SELECT hash FROM drama_imports WHERE id=?", id).Scan(&prior)
	if err == nil {
		if prior != hash {
			return empty, errConflict
		}
		return s.dramaWork(id)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return empty, err
	}
	files := map[string]*zip.File{}
	var total uint64
	if len(z.File) > 2048 {
		return empty, errors.New("作品包文件过多")
	}
	for _, f := range z.File {
		if !packagePath(f.Name) || !f.Mode().IsRegular() || files[f.Name] != nil {
			return empty, errors.New("作品包含无效、重复路径或非普通文件")
		}
		if f.UncompressedSize64 > 100<<20 {
			return empty, errors.New("单个素材超过100MB")
		}
		total += f.UncompressedSize64
		if total > dramaPackageLimit {
			return empty, errors.New("作品包解压后超过500MB")
		}
		files[f.Name] = f
	}
	used := map[string]bool{}
	read := func(name string, limit int64) ([]byte, error) {
		f := files[name]
		if f == nil || used[name] || f.UncompressedSize64 > uint64(limit) {
			return nil, errors.New("作品包文件缺失、重复引用或超限")
		}
		used[name] = true
		r, e := f.Open()
		if e != nil {
			return nil, e
		}
		defer r.Close()
		b, e := io.ReadAll(io.LimitReader(r, limit+1))
		if e != nil {
			return nil, e
		}
		if int64(len(b)) > limit {
			return nil, errors.New("作品包文件过大")
		}
		return b, nil
	}
	b, err := read(dramaPackageManifest, 1<<20)
	if err != nil {
		return empty, errors.New("请选择新版映序导出的作品包（缺少有效lypit-work.json）")
	}
	var p dramaPackage
	if err = json.Unmarshal(b, &p); err != nil || p.Format != "lypit-work" || p.Version != 1 {
		return empty, errors.New("作品包格式或版本不受支持")
	}
	w := portableDramaWork(p.Work)
	if strings.TrimSpace(w.Title) == "" || len(w.Title) > 320 || strings.TrimSpace(w.Idea) == "" || len(w.Idea) > 20000 ||
		(w.Aspect != "9:16" && w.Aspect != "16:9") || w.Duration < 1 || w.Duration > 600 || w.EpisodeNumber < 0 || w.EpisodeNumber > 999 || !validDramaPromptLanguage(w.PromptLanguage) {
		return empty, errors.New("作品包的基本信息无效")
	}
	w.ID = id
	w.Created = now()
	cfg, err := s.store.settings()
	if err != nil {
		return empty, err
	}
	w.Profile = dramaProfile(cfg, "first_frame", w.PromptLanguage)
	w.ProfileHash = profileHash(w.Profile)
	w.PlanHash = dramaEpisodeHash(w)
	docs := map[string][]byte{}
	for kind, name := range p.Documents {
		if dramaPaths[kind] == "" {
			return empty, errors.New("未知作品文档")
		}
		b, e := read(name, 1<<20)
		if e != nil {
			return empty, e
		}
		docs[kind] = b
	}
	// Validate hashes now, then stream each payload again during installation to bound memory.
	for _, item := range p.Media {
		m := item.Media
		if !packagePath(m.Path) || !safeMedia(item.Blob) || !contains([]string{"image", "video", "film", "voice", "music", "audio"}, m.Kind) || len(m.Entry) > 1024 {
			return empty, errors.New("作品素材信息无效")
		}
		b, e := read(item.Blob, 100<<20)
		if e != nil {
			return empty, e
		}
		if digest(b) != m.Hash {
			return empty, errors.New("作品素材校验失败")
		}
	}
	if len(used) != len(files) {
		return empty, errors.New("作品包包含未声明文件")
	}
	written := []string{}
	committed := false
	defer func() {
		if !committed {
			for _, name := range written {
				_ = os.Remove(name)
			}
		}
	}()
	write := func(name string, b []byte) error {
		written = append(written, name)
		if e := atomicDramaFile(name, b); e != nil {
			return e
		}
		return nil
	}
	err = s.store.transaction(func(tx *sql.Tx) error {
		shell := Project{ID: w.ID, Title: w.Title, AspectRatio: w.Aspect, Duration: w.Duration, Created: w.Created}
		if _, e := tx.Exec("INSERT INTO projects(id,body) VALUES(?,?)", id, jsonBytes(shell)); e != nil {
			return e
		}
		if _, e := tx.Exec("INSERT INTO drama_works VALUES(?,?)", id, jsonBytes(w)); e != nil {
			return e
		}
		for kind, b := range docs {
			name, e := s.dramaFile(id, dramaWorkPath(w, dramaPaths[kind]))
			if e != nil {
				return e
			}
			if e = write(name, b); e != nil {
				return e
			}
			meta := dramaMeta{Version: 1, Hash: digest(b), Inputs: map[string]string{}}
			if _, e = tx.Exec("INSERT INTO drama_documents VALUES(?,?,?)", id, kind, jsonBytes(meta)); e != nil {
				return e
			}
		}
		// Preserve export order (newest first) in the destination rowid ordering.
		for i := len(p.Media) - 1; i >= 0; i-- {
			item := p.Media[i]
			m := portableDramaMedia(item.Media)
			m.ID = uid()
			m.WorkID = id
			m.File = id + "/" + m.ID + path.Ext(item.Blob)
			r, e := files[item.Blob].Open()
			if e != nil {
				return e
			}
			b, e := io.ReadAll(io.LimitReader(r, 100<<20+1))
			r.Close()
			if e != nil {
				return e
			}
			if e = write(s.media(m.File), b); e != nil {
				return e
			}
			if _, e = tx.Exec("INSERT INTO drama_media VALUES(?,?,?)", m.ID, id, jsonBytes(m)); e != nil {
				return e
			}
		}
		_, e := tx.Exec("INSERT INTO drama_imports VALUES(?,?)", id, hash)
		return e
	})
	committed = err == nil
	return w, err
}

func (s *Studio) dramaPackageRoutes(g *gin.RouterGroup) {
	g.GET("/works/:id/package", func(c *gin.Context) {
		s.dramaMu.Lock()
		defer s.dramaMu.Unlock()
		b, e := s.exportDramaPackage(c.Param("id"))
		if e != nil {
			result(c, nil, e)
			return
		}
		c.Header("Content-Disposition", "attachment; filename=lypit-work-"+c.Param("id")+".zip")
		c.Data(200, "application/zip", b)
	})
	g.POST("/works/import", func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, dramaPackageLimit)
		f, e := os.CreateTemp("", "lypit-import-*.zip")
		if e != nil {
			result(c, nil, e)
			return
		}
		defer os.Remove(f.Name())
		defer f.Close()
		n, e := io.Copy(f, c.Request.Body)
		if e != nil {
			result(c, nil, errors.New("读取作品包失败，压缩包须小于500MB"))
			return
		}
		z, e := zip.NewReader(f, n)
		if e != nil {
			result(c, nil, errors.New("作品包不是有效ZIP文件"))
			return
		}
		if _, e = f.Seek(0, 0); e != nil {
			result(c, nil, e)
			return
		}
		// Hash the upload without keeping the complete ZIP in memory.
		h := sha256.New()
		_, e = io.Copy(h, f)
		if e != nil {
			result(c, nil, e)
			return
		}
		hash := hex.EncodeToString(h.Sum(nil))
		s.dramaMu.Lock()
		defer s.dramaMu.Unlock()
		w, e := s.importDramaPackage(c.Query("operation_id"), hash, z)
		result(c, w, e)
	})
}
