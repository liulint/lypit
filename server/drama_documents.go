package main

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

//go:embed drama_bundle
var dramaBundle embed.FS

const dramaVersion = "0.11.1"

var dramaPaths = map[string]string{
	"develop": "项目开发/creative-brief.md", "script": "剧集/EP001/剧本.md", "assets": "剧集/EP001/视觉设定.md", "images": "剧集/EP001/图片提示词.md", "storyboard": "剧集/EP001/分镜.md", "motion": "剧集/EP001/视频提示词.md", "edit": "剧集/EP001/剪辑单.md", "review": "审查/EP001-审查.md",
}
var dramaSkills = map[string]string{"develop": "short-drama-develop", "script": "short-drama-write", "assets": "short-drama-assets", "images": "short-drama-image-prompts", "storyboard": "short-drama-storyboard", "motion": "short-drama-video-prompts", "edit": "short-drama-edit", "review": "short-drama-review"}
var dramaDeps = map[string][]string{"develop": {}, "script": {"develop"}, "assets": {"script"}, "images": {"assets"}, "storyboard": {"script", "assets"}, "motion": {"script", "assets", "storyboard"}, "edit": {"script", "storyboard", "motion"}, "review": {}}
var dramaID = regexp.MustCompile(`^[a-f0-9]{24}$`)
var dramaEntryID = regexp.MustCompile(`^(EP[0-9]{3}-SC[0-9]+|(?:IMG|SHOT|MOTION|MUSIC|CUT|GROUP)-[A-Za-z0-9][A-Za-z0-9_-]*)$`)

type DramaWork struct {
	SeriesID       string               `json:"series_id,omitempty"`
	EpisodeNumber  int                  `json:"episode_number,omitempty"`
	Library        bool                 `json:"library,omitempty"`
	Plan           DramaEpisodePlan     `json:"plan"`
	PlanHash       string               `json:"plan_hash"`
	Context        *DramaEpisodeContext `json:"context,omitempty"`
	Profile        DramaProfile         `json:"profile"`
	ProfileHash    string               `json:"profile_hash"`
	ID             string               `json:"id"`
	Title          string               `json:"title"`
	Idea           string               `json:"idea"`
	Genre          string               `json:"genre"`
	Form           string               `json:"form"`
	Aspect         string               `json:"aspect"`
	Duration       int                  `json:"duration"`
	Language       string               `json:"language"`
	PromptLanguage string               `json:"prompt_language"`
	Created        string               `json:"created"`
}
type DramaEntry struct {
	ID     string            `json:"id"`
	Title  string            `json:"title"`
	Start  int               `json:"start"`
	End    int               `json:"end"`
	Body   string            `json:"body"`
	Fields map[string]string `json:"fields"`
}
type DramaIssue struct {
	Line    int    `json:"line"`
	Rule    string `json:"rule"`
	Message string `json:"message"`
}
type DramaDoc struct {
	Kind         string            `json:"kind"`
	Path         string            `json:"path"`
	Text         string            `json:"text"`
	Hash         string            `json:"hash"`
	Version      int               `json:"version"`
	State        string            `json:"state"`
	AcceptedHash string            `json:"accepted_hash"`
	Inputs       map[string]string `json:"inputs"`
	Entries      []DramaEntry      `json:"entries"`
	Issues       []DramaIssue      `json:"issues"`
}
type dramaMeta struct {
	Version  int               `json:"version"`
	Hash     string            `json:"hash"`
	Accepted string            `json:"accepted"`
	Inputs   map[string]string `json:"inputs"`
}

func (s *Studio) initDrama() error {
	var manifest struct {
		Version string
		Files   map[string]string
	}
	b, e := dramaBundle.ReadFile("drama_bundle/manifest.json")
	if e != nil {
		return e
	}
	if e = json.Unmarshal(b, &manifest); e != nil {
		return e
	}
	if manifest.Version != dramaVersion {
		return errors.New("内置技能版本不匹配")
	}
	for path, hash := range manifest.Files {
		b, e := dramaBundle.ReadFile("drama_bundle/" + path)
		if e != nil || digest(b) != hash {
			return fmt.Errorf("技能包校验失败：%s", path)
		}
	}
	_, e = s.store.db.Exec(`CREATE TABLE IF NOT EXISTS drama_series_versions(series_id TEXT NOT NULL,version INTEGER NOT NULL,body TEXT NOT NULL,PRIMARY KEY(series_id,version));
CREATE TABLE IF NOT EXISTS drama_episodes(series_id TEXT NOT NULL,number INTEGER NOT NULL,work_id TEXT NOT NULL UNIQUE REFERENCES drama_works(id),input_hash TEXT NOT NULL,PRIMARY KEY(series_id,number));
CREATE TABLE IF NOT EXISTS drama_continuity(work_id TEXT NOT NULL REFERENCES drama_works(id),version INTEGER NOT NULL,body TEXT NOT NULL,PRIMARY KEY(work_id,version));
CREATE TABLE IF NOT EXISTS drama_works(id TEXT PRIMARY KEY REFERENCES projects(id), body TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS drama_documents(work_id TEXT NOT NULL REFERENCES drama_works(id),kind TEXT NOT NULL,body TEXT NOT NULL,PRIMARY KEY(work_id,kind));
CREATE TABLE IF NOT EXISTS drama_jobs(id TEXT PRIMARY KEY REFERENCES tasks(id),body TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS drama_media_reuse(id TEXT PRIMARY KEY,body TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS drama_quotes(id TEXT PRIMARY KEY,body TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS drama_video_starts(work_id TEXT NOT NULL REFERENCES drama_works(id),entry TEXT NOT NULL,motion_hash TEXT NOT NULL,board_hash TEXT NOT NULL,mode TEXT NOT NULL,PRIMARY KEY(work_id,entry));
CREATE TABLE IF NOT EXISTS drama_video_choices(work_id TEXT NOT NULL REFERENCES drama_works(id),entry TEXT NOT NULL,media_id TEXT NOT NULL,PRIMARY KEY(work_id,entry));
CREATE TABLE IF NOT EXISTS drama_imports(id TEXT PRIMARY KEY,hash TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS drama_media(id TEXT PRIMARY KEY,work_id TEXT NOT NULL REFERENCES drama_works(id),body TEXT NOT NULL);`)
	return e
}
func dramaParse(kind, text string) ([]DramaEntry, []DramaIssue) {
	return dramaParseEpisode(kind, text, "EP001")
}
func dramaParseEpisode(kind, text, episode string) ([]DramaEntry, []DramaIssue) {
	entries := []DramaEntry{}
	issues := []DramaIssue{}
	lines := strings.Split(text, "\n")
	seen := map[string]bool{}
	for i, line := range lines {
		if !strings.HasPrefix(line, "## ") {
			continue
		}
		title := strings.TrimSpace(strings.TrimPrefix(line, "## "))
		id := strings.Fields(title)
		if len(id) == 0 {
			continue
		}
		key := id[0]
		if kind == "assets" || strings.HasPrefix(title, "场景站位") {
			key = title
		}
		if seen[key] {
			issues = append(issues, DramaIssue{i + 1, "ID", "条目标识重复：" + key})
		}
		seen[key] = true
		if len(entries) > 0 {
			entries[len(entries)-1].End = i
		}
		entries = append(entries, DramaEntry{ID: key, Title: title, Start: i, End: len(lines), Fields: map[string]string{}})
	}
	for i := range entries {
		v := &entries[i]
		v.Body = strings.Join(lines[v.Start:v.End], "\n")
		for _, line := range lines[v.Start:v.End] {
			if !strings.HasPrefix(line, "- ") {
				continue
			}
			pair := strings.SplitN(strings.TrimPrefix(line, "- "), "：", 2)
			if len(pair) == 2 {
				v.Fields[pair[0]] = pair[1]
			}
		}
		required := []string{}
		prefix := ""
		switch kind {
		case "script":
			prefix = episode + "-SC"
		case "images":
			prefix = "IMG-"
		case "storyboard":
			if strings.HasPrefix(v.Title, "场景站位") {
				continue
			}
			prefix = "SHOT-"
			required = []string{"来源", "时长", "目的", "景别/机位", "运镜", "起点", "唯一动作", "终点", "视觉依据", "输入参考图"}
		case "motion":
			if strings.HasPrefix(v.ID, "GROUP-") || strings.HasPrefix(v.ID, "MUSIC-") {
				continue
			}
			prefix = "MOTION-"
			required = []string{"分镜", "时长", "输入参考图", "终点"}
		case "edit":
			prefix = "CUT-"
			required = []string{"来源", "入点", "出点", "时长", "取舍", "声音"}
		}
		if prefix != "" && !strings.HasPrefix(v.ID, prefix) {
			issues = append(issues, DramaIssue{v.Start + 1, "ID", "条目应使用" + prefix + "标识"})
		}
		if prefix != "" && !dramaEntryID.MatchString(v.ID) {
			issues = append(issues, DramaIssue{v.Start + 1, "ID", "条目标识只能包含对应前缀、英文字母、数字、下划线和短横线"})
		}

		for _, field := range required {
			if strings.TrimSpace(v.Fields[field]) == "" {
				issues = append(issues, DramaIssue{v.Start + 1, "FIELD", "缺少字段：" + field})
			}
		}
		if kind == "storyboard" || kind == "motion" {
			if dramaSeconds(v.Fields["时长"]) <= 0 {
				issues = append(issues, DramaIssue{v.Start + 1, "TIME", "时长必须为正秒数"})
			}
		}
		if kind == "edit" {
			a, ea := strconv.ParseFloat(v.Fields["入点"], 64)
			b, eb := strconv.ParseFloat(v.Fields["出点"], 64)
			d := dramaSeconds(v.Fields["时长"])
			if ea != nil || eb != nil || a < 0 || b <= a || absFloat(b-a-d) > 0.011 {
				issues = append(issues, DramaIssue{v.Start + 1, "TIME", "入点、出点和时长不自洽"})
			}
		}
	}
	if strings.TrimSpace(text) != "" && len(entries) == 0 && kind != "develop" && kind != "review" {
		issues = append(issues, DramaIssue{1, "STRUCTURE", "缺少二级条目标题"})
	}
	return entries, issues
}
func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
func dramaSeconds(v string) float64 {
	v = strings.TrimSpace(v)
	v = strings.TrimSuffix(v, "秒")
	v = strings.TrimSuffix(v, "s")
	n, e := strconv.ParseFloat(v, 64)
	if e != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		return 0
	}
	return n
}
func (s *Studio) dramaWork(id string) (DramaWork, error) {
	var w DramaWork
	if !dramaID.MatchString(id) {
		return w, errors.New("作品标识无效")
	}
	var b []byte
	e := s.store.db.QueryRow("SELECT body FROM drama_works WHERE id=?", id).Scan(&b)
	if e == nil {
		e = json.Unmarshal(b, &w)
	}
	w.ProfileHash = profileHash(w.Profile)
	w.PlanHash = dramaEpisodeHash(w)
	return w, e
}
func (s *Studio) dramaFile(id, rel string) (string, error) {
	if !dramaID.MatchString(id) || strings.Contains(rel, "..") || strings.ContainsAny(rel, "\\:\x00") || filepath.IsAbs(rel) {
		return "", errors.New("项目路径无效")
	}
	base := filepath.Join(s.data, "works", id)
	p := filepath.Join(base, filepath.FromSlash(rel))
	cur := s.data
	parts, _ := filepath.Rel(s.data, p)
	for _, part := range strings.Split(parts, string(filepath.Separator)) {
		cur = filepath.Join(cur, part)
		info, e := os.Lstat(cur)
		if e == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("不允许符号链接")
		}
		if e != nil && !os.IsNotExist(e) {
			return "", e
		}
	}
	return p, nil
}
func atomicDramaFile(path string, b []byte) error {
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	f, e := os.OpenFile(path+"."+uid()+".part", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, e = f.Write(b); e != nil {
		f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	if e = os.Rename(tmp, path); e != nil {
		return e
	}
	return syncDirectory(filepath.Dir(path))
}
func (s *Studio) dramaDoc(id, kind string) (DramaDoc, error) {
	d := DramaDoc{Kind: kind, Path: dramaPaths[kind], State: "draft", Inputs: map[string]string{}, Entries: []DramaEntry{}, Issues: []DramaIssue{}}
	if d.Path == "" {
		return d, errors.New("未知文档类型")
	}
	w, e := s.dramaWork(id)
	if e != nil {
		return d, e
	}
	d.Path = dramaWorkPath(w, d.Path)
	path, e := s.dramaFile(id, d.Path)
	if e != nil {
		return d, e
	}
	b, e := os.ReadFile(path)
	if os.IsNotExist(e) {
		return d, nil
	}
	if e != nil {
		return d, e
	}
	if len(b) > 1<<20 {
		return d, errors.New("文档超过1MB")
	}
	d.Text = string(b)
	d.Hash = digest(b)
	d.Entries, d.Issues = dramaParseEpisode(kind, d.Text, dramaEpisodeCode(w))
	var raw []byte
	var m dramaMeta
	e = s.store.db.QueryRow("SELECT body FROM drama_documents WHERE work_id=? AND kind=?", id, kind).Scan(&raw)
	if e == nil {
		if e = json.Unmarshal(raw, &m); e != nil {
			return d, e
		}
		d.Version = m.Version
		d.AcceptedHash = m.Accepted
		d.Inputs = m.Inputs
		if m.Hash != d.Hash {
			d.State = "stale"
			d.Issues = append(d.Issues, DramaIssue{1, "EXTERNAL_CHANGE", "磁盘文档发生外部修改，须保存并重新采用"})
		} else if m.Accepted == d.Hash {
			d.State = "accepted"
			for dep, hash := range m.Inputs {
				if !s.dramaDependencyCurrent(id, dep, hash, kind != "review") {
					d.State = "stale"
					break
				}
			}
		}
	} else if !errors.Is(e, sql.ErrNoRows) {
		return d, e
	}
	return d, nil
}
func (s *Studio) dramaInputs(id, kind string) (map[string]string, error) {
	out := map[string]string{}
	w, e := s.dramaWork(id)
	if e != nil {
		return nil, e
	}
	if w.SeriesID != "" {
		out["@episode"] = dramaEpisodeHash(w)
	}
	if kind == "storyboard" || kind == "motion" {
		w, e := s.dramaWork(id)
		if e != nil {
			return nil, e
		}
		out["@profile"] = profileHash(w.Profile)
	}
	if kind == "assets" || kind == "images" || kind == "motion" {
		out["@prompt-language"] = w.PromptLanguage
	}
	deps := dramaDeps[kind]
	if kind == "review" {
		for k := range dramaPaths {
			if k != "review" {
				deps = append(deps, k)
			}
		}
	}
	for _, k := range deps {
		d, e := s.dramaDoc(id, k)
		if e != nil {
			return nil, e
		}
		if d.Text == "" && ((kind == "script" && k == "develop") || kind == "review") {
			continue
		}
		if kind != "review" && d.State != "accepted" {
			return nil, fmt.Errorf("请先采用当前%s文档", k)
		}
		out[k] = d.Hash
	}
	return out, nil
}
func (s *Studio) saveDramaDoc(id, kind, base, text string, accept bool, inputs map[string]string) (DramaDoc, error) {
	d, e := s.dramaDoc(id, kind)
	if e != nil {
		return d, e
	}
	if d.Hash != base {
		return d, errConflict
	}
	if len(text) > 1<<20 {
		return d, errors.New("文档最多1MB")
	}
	if strings.TrimSpace(text) == "" {
		return d, errors.New("文档不能为空")
	}
	w, e := s.dramaWork(id)
	if e != nil {
		return d, e
	}
	entries, issues := dramaParseEpisode(kind, text, dramaEpisodeCode(w))
	_ = entries
	if accept && len(issues) > 0 {
		return d, fmt.Errorf("结构检查未通过：第%d行 %s", issues[0].Line, issues[0].Message)
	}
	if inputs == nil {
		inputs, e = s.dramaInputs(id, kind)
		if e != nil && accept {
			return d, e
		}
		if inputs == nil {
			inputs = map[string]string{}
		}
	}
	if accept {
		if e = s.validateDramaLinks(id, kind, text); e != nil {
			return d, e
		}
	}
	path, e := s.dramaFile(id, d.Path)
	if e != nil {
		return d, e
	}
	if d.Text != "" {
		history, err := s.dramaFile(id, fmt.Sprintf(".history/%s/%s.md", kind, d.Hash))
		if err != nil {
			return d, err
		}
		if err = atomicDramaFile(history, []byte(d.Text)); err != nil {
			return d, err
		}
	}
	if e = atomicDramaFile(path, []byte(text)); e != nil {
		return d, e
	}
	m := dramaMeta{Version: d.Version + 1, Hash: digest([]byte(text)), Inputs: inputs}
	if accept {
		m.Accepted = m.Hash
	}
	_, e = s.store.db.Exec("INSERT INTO drama_documents VALUES(?,?,?) ON CONFLICT(work_id,kind) DO UPDATE SET body=excluded.body", id, kind, jsonBytes(m))
	if e != nil {
		return d, e
	}
	return s.dramaDoc(id, kind)
}

// The source field may include verbatim evidence after its single scene ID.
// Reject multiple external IDs and arbitrary suffixes rather than guessing a source.
var dramaSceneSource = regexp.MustCompile(`^(EP[0-9]{3}-SC[0-9]+)(?:\s*(?:「[^「」]*」\s*)+[。.．]?)?$`)

func dramaSourceScene(value string) string {
	match := dramaSceneSource.FindStringSubmatch(strings.TrimSpace(value))
	if len(match) == 0 {
		return ""
	}
	return match[1]
}

func (s *Studio) validateDramaLinks(id, kind, text string) error {
	entries, _ := dramaParse(kind, text)
	get := func(k string) map[string]DramaEntry {
		d, _ := s.dramaDoc(id, k)
		out := map[string]DramaEntry{}
		for _, v := range d.Entries {
			out[v.ID] = v
		}
		return out
	}
	if kind == "storyboard" {
		scenes := get("script")
		for _, v := range entries {
			if !strings.HasPrefix(v.ID, "SHOT-") {
				continue
			}
			source := strings.TrimSpace(v.Fields["来源"])
			// Preserve exact identifier matching before interpreting quoted evidence.
			if _, ok := scenes[source]; ok {
				continue
			}
			if _, ok := scenes[dramaSourceScene(source)]; !ok {
				return fmt.Errorf("%s 来源场景不存在", v.ID)
			}
		}
	}
	if kind == "motion" {
		shots := get("storyboard")
		for _, v := range entries {
			if !strings.HasPrefix(v.ID, "MOTION-") {
				continue
			}
			q, ok := shots[strings.TrimSpace(v.Fields["分镜"])]
			if !ok {
				return fmt.Errorf("%s 分镜不存在", v.ID)
			}
			if strings.TrimPrefix(v.ID, "MOTION-") != strings.TrimPrefix(q.ID, "SHOT-") {
				return fmt.Errorf("%s 与分镜编号不一致", v.ID)
			}
			if dramaSeconds(v.Fields["时长"]) != dramaSeconds(q.Fields["时长"]) {
				return fmt.Errorf("%s 与分镜时长不一致", v.ID)
			}
			if v.Fields["输入参考图"] != q.Fields["输入参考图"] {
				return fmt.Errorf("%s 参考图必须与分镜一致", v.ID)
			}
			if v.Fields["终点"] != q.Fields["终点"] {
				return fmt.Errorf("%s 终点必须与分镜一致", v.ID)
			}
		}
	}
	return nil
}
func (s *Studio) dramaRoutes(a *gin.RouterGroup) {
	g := a.Group("/drama")
	g.GET("/skills", func(c *gin.Context) {
		paths := []string{}
		fs.WalkDir(dramaBundle, "drama_bundle/skills", func(path string, d fs.DirEntry, e error) error {
			if e == nil && !d.IsDir() && strings.HasSuffix(path, ".md") {
				paths = append(paths, strings.TrimPrefix(path, "drama_bundle/"))
			}
			return e
		})
		sort.Strings(paths)
		c.JSON(200, gin.H{"version": dramaVersion, "files": paths, "stages": dramaSkills})
	})
	g.GET("/skill", func(c *gin.Context) {
		path := c.Query("path")
		if !strings.HasPrefix(path, "skills/") || strings.Contains(path, "..") || !strings.HasSuffix(path, ".md") {
			result(c, nil, errors.New("资料路径无效"))
			return
		}
		b, e := dramaBundle.ReadFile("drama_bundle/" + path)
		result(c, gin.H{"text": string(b)}, e)
	})
	g.GET("/works", func(c *gin.Context) {
		rows, e := s.store.db.Query("SELECT body FROM drama_works ORDER BY rowid DESC")
		if e != nil {
			result(c, nil, e)
			return
		}
		defer rows.Close()
		out := []DramaWork{}
		for rows.Next() {
			var b []byte
			var w DramaWork
			if e = rows.Scan(&b); e != nil {
				break
			}
			if e = json.Unmarshal(b, &w); e != nil {
				break
			}
			if !w.Library {
				w.PlanHash = dramaEpisodeHash(w)
				out = append(out, w)
			}
		}
		if e == nil {
			e = rows.Err()
		}
		result(c, out, e)
	})
	g.POST("/works", func(c *gin.Context) {
		var w DramaWork
		if !bind(c, &w) {
			return
		}
		if w.SeriesID != "" || w.Library || w.Context != nil || w.EpisodeNumber != 0 {
			result(c, nil, errors.New("系列分集请从系列入口创建"))
			return
		}
		if strings.TrimSpace(w.Title) == "" || len(w.Title) > 320 || strings.TrimSpace(w.Idea) == "" || len(w.Idea) > 20000 {
			result(c, nil, errors.New("请填写标题和创作意图"))
			return
		}
		if w.Aspect == "" {
			w.Aspect = "9:16"
		}
		if w.Duration == 0 {
			w.Duration = 33
		}
		if (w.Aspect != "9:16" && w.Aspect != "16:9") || w.Duration < 1 || w.Duration > 600 {
			result(c, nil, errors.New("画幅须为9:16或16:9，时长须为1～600秒"))
			return
		}
		cfg, err := s.store.settings()
		if err != nil {
			result(c, nil, err)
			return
		}
		if w.PromptLanguage == "" {
			w.PromptLanguage = "zh-CN"
		}
		if !validDramaPromptLanguage(w.PromptLanguage) {
			result(c, nil, errors.New("提示词语言请选择中文或英文"))
			return
		}
		if w.Profile.Language == "" {
			w.Profile.Language = w.PromptLanguage
		}
		w.Profile = dramaProfile(cfg, w.Profile.Mode, w.Profile.Language)
		w.ProfileHash = profileHash(w.Profile)
		w.ID = uid()
		w.Created = now()
		w.Language = "zh-CN"
		// Existing provider intents use this metadata shell only; creative content lives in Markdown.
		shell := Project{ID: w.ID, Title: w.Title, AspectRatio: w.Aspect, Duration: w.Duration, Created: w.Created}
		e := s.store.transaction(func(tx *sql.Tx) error {
			if _, err := tx.Exec("INSERT INTO projects(id,body) VALUES(?,?)", w.ID, jsonBytes(shell)); err != nil {
				return err
			}
			_, err := tx.Exec("INSERT INTO drama_works VALUES(?,?)", w.ID, jsonBytes(w))
			return err
		})
		result(c, w, e)
	})
	g.GET("/works/:id", func(c *gin.Context) { w, e := s.dramaWork(c.Param("id")); result(c, w, e) })
	g.GET("/works/:id/documents", func(c *gin.Context) {
		s.dramaMu.Lock()
		defer s.dramaMu.Unlock()
		out := []DramaDoc{}
		for _, k := range []string{"develop", "script", "assets", "images", "storyboard", "motion", "edit", "review"} {
			d, e := s.dramaDoc(c.Param("id"), k)
			if e != nil {
				result(c, nil, e)
				return
			}
			out = append(out, d)
		}
		result(c, out, nil)
	})
	g.PUT("/works/:id/documents/:kind", func(c *gin.Context) {
		var v struct {
			Base   string `json:"base"`
			Text   string `json:"text"`
			Accept bool   `json:"accept"`
		}
		if !bind(c, &v) {
			return
		}
		s.dramaMu.Lock()
		defer s.dramaMu.Unlock()
		d, e := s.saveDramaDoc(c.Param("id"), c.Param("kind"), v.Base, v.Text, v.Accept, nil)
		result(c, d, e)
	})
	g.GET("/works/:id/export", func(c *gin.Context) {
		s.dramaMu.Lock()
		defer s.dramaMu.Unlock()
		id := c.Param("id")
		w, e := s.dramaWork(id)
		if e != nil {
			result(c, nil, e)
			return
		}
		var buf bytes.Buffer
		z := zip.NewWriter(&buf)
		for kind := range dramaPaths {
			d, err := s.dramaDoc(id, kind)
			if err != nil {
				result(c, nil, err)
				return
			}
			if d.Text == "" {
				continue
			}
			f, err := z.Create(d.Path)
			if err != nil {
				result(c, nil, err)
				return
			}
			f.Write([]byte(d.Text))
		}

		files, err := s.dramaMedias(id)
		if err != nil {
			result(c, nil, err)
			return
		}
		total := 0
		for _, m := range files {
			if err = s.verifyDramaMedia(m); err != nil {
				result(c, nil, err)
				return
			}
			b, err := os.ReadFile(s.media(m.File))
			if err != nil {
				result(c, nil, err)
				return
			}
			total += len(b)
			if total > 500<<20 {
				result(c, nil, errors.New("项目超过500MB，请从本机项目目录分批复制素材"))
				return
			}
			f, err := z.Create(m.Path)
			if err != nil {
				result(c, nil, err)
				return
			}
			if _, err = f.Write(b); err != nil {
				result(c, nil, err)
				return
			}
		}
		f, _ := z.Create("short-drama.json")
		f.Write(jsonBytes(dramaProjectConfig(w)))
		if w.Context != nil {
			f, _ = z.Create("系列与前情快照.json")
			f.Write(jsonBytes(w.Context))
			v, err := s.dramaContinuity(id)
			if err != nil {
				result(c, nil, err)
				return
			}
			f, _ = z.Create("本集结束状态.json")
			f.Write(jsonBytes(v))
		}
		if e = z.Close(); e != nil {
			result(c, nil, e)
			return
		}
		c.Header("Content-Disposition", "attachment; filename="+dramaEpisodeCode(w)+".zip")
		c.Data(200, "application/zip", buf.Bytes())
	})
	s.dramaPackageRoutes(g)
	s.dramaSeriesRoutes(g)
	s.dramaReuseRoutes(g)
	s.dramaFrameBindingRoutes(g)
	s.dramaVideoStartRoutes(g)
	s.dramaProfileRoutes(g)
	s.dramaJobRoutes(g)
	s.dramaProductionRoutes(g)
}
