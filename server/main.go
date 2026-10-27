package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
)

type Studio struct {
	catalogs                      cliCatalogCache
	secretsMu                     sync.Mutex
	store                         *Store
	data, root, seedanceBin, web  string
	ctx                           context.Context
	mu                            sync.Mutex
	running                       map[string]context.CancelFunc
	wg                            sync.WaitGroup
	imageSem, videoSem, renderSem chan struct{}
	frontendPort                  int
}

func (s *Studio) abs(rel string) string { return filepath.Join(s.data, "media", rel) }
func newStudio(ctx context.Context, root, data, web string) (*Studio, error) {
	st, e := openStore(data)
	if e != nil {
		return nil, e
	}
	return &Studio{store: st, root: root, data: data, web: web, ctx: ctx, running: map[string]context.CancelFunc{}, imageSem: make(chan struct{}, 2), videoSem: make(chan struct{}, 2), renderSem: make(chan struct{}, 1), frontendPort: 5173, seedanceBin: os.Getenv("SEEDANCE_BIN")}, nil
}
func fail(c *gin.Context, e error) { c.JSON(http.StatusBadRequest, gin.H{"error": e.Error()}) }
func (s *Studio) router() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.MaxMultipartMemory = 8 << 20
	r.Use(func(c *gin.Context) {
		if c.Request.Method != "GET" && c.Request.Method != "HEAD" {
			o := c.GetHeader("Origin")
			if o != "" {
				u, e := url.Parse(o)
				port := strconv.Itoa(s.frontendPort)
				if e != nil || !(u.Host == c.Request.Host || u.Host == "127.0.0.1:"+port || u.Host == "localhost:"+port) {
					c.AbortWithStatusJSON(403, gin.H{"error": "跨站请求被拒绝"})
					return
				}
			}
		}
		c.Header("X-Content-Type-Options", "nosniff")
		c.Next()
	})
	api := r.Group("/api")
	s.storyboardRoutes(api)
	s.assetRoutes(api)
	api.GET("/health", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true, "mode": "local-single-user"}) })
	s.modelRoutes(api)
	api.GET("/projects", func(c *gin.Context) {
		s.store.mu.Lock()
		projects := []Project{}
		for _, p := range s.store.state.Projects {
			projects = append(projects, clone(*p))
		}
		s.store.mu.Unlock()
		slices.SortFunc(projects, func(a, b Project) int { return strings.Compare(b.Created, a.Created) })
		c.JSON(200, projects)
	})
	api.POST("/projects", func(c *gin.Context) {
		var v struct {
			Idea   string     `json:"idea"`
			Visual VisualPlan `json:"visual"`
		}
		if e := c.ShouldBindJSON(&v); e != nil || strings.TrimSpace(v.Idea) == "" || len(v.Idea) > 10000 {
			fail(c, errors.New("请输入1～10000字节故事想法"))
			return
		}
		p := Project{AssetWorkflow: true, Visual: visualDefaults(v.Visual, v.Idea), ID: uid(), Idea: v.Idea, Created: now(), Revision: 1, Status: "draft", References: []string{}, Boards: []Board{}, Tasks: []Task{}, Attempts: map[string]Attempt{}}
		if e := s.store.change(func(st *State) error { st.Projects[p.ID] = &p; return nil }); e != nil {
			fail(c, e)
			return
		}
		c.JSON(201, p)
	})
	api.GET("/projects/:id", func(c *gin.Context) {
		p, e := s.store.project(c.Param("id"))
		if e != nil {
			c.JSON(404, gin.H{"error": e.Error()})
			return
		}
		c.JSON(200, p)
	})
	api.POST("/projects/:id/quote", func(c *gin.Context) {
		var v struct {
			Stage       string `json:"stage"`
			Instruction string `json:"instruction"`
			ShotID      string `json:"shot_id"`
		}
		if e := c.ShouldBindJSON(&v); e != nil {
			fail(c, e)
			return
		}
		q, e := s.makeQuote(c.Param("id"), v.Stage, v.Instruction, v.ShotID)
		if e != nil {
			fail(c, e)
			return
		}
		c.JSON(200, q)
	})
	api.POST("/projects/:id/confirm", func(c *gin.Context) {
		var v struct {
			QuoteID      string `json:"quote_id"`
			AllowUnknown bool   `json:"allow_unknown"`
		}
		if e := c.ShouldBindJSON(&v); e != nil {
			fail(c, e)
			return
		}
		if e := s.confirm(c.Param("id"), v.QuoteID, v.AllowUnknown); e != nil {
			fail(c, e)
			return
		}
		c.JSON(202, gin.H{"message": "已确认，任务在后台执行"})
	})
	api.POST("/projects/:id/approve", func(c *gin.Context) {
		var v struct {
			Version int `json:"version"`
		}
		if e := c.ShouldBindJSON(&v); e != nil {
			fail(c, e)
			return
		}
		e := s.store.change(func(st *State) error {
			p := st.Projects[c.Param("id")]
			if p == nil {
				return errors.New("项目不存在")
			}
			b := board(*p)
			if b == nil || b.Version != v.Version || p.Status == "running" {
				return errors.New("分镜已变化或制作未完成")
			}
			if e := s.assertBoardAssetSnapshot(*p, *b); e != nil {
				return e
			}
			if e := validateApproval(*b); e != nil {
				return e
			}
			p.Boards[len(p.Boards)-1].Approved = true
			p.Revision++
			return nil
		})
		if e != nil {
			fail(c, e)
			return
		}
		c.JSON(200, gin.H{"message": "当前分镜已锁定"})
	})
	api.POST("/projects/:id/cancel", func(c *gin.Context) {
		s.mu.Lock()
		cancel := s.running[c.Param("id")]
		s.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		c.JSON(200, gin.H{"message": "已请求停止新增任务；供应商已接收的任务仍可能运行并计费"})
	})
	api.POST("/projects/:id/resume", func(c *gin.Context) {
		if e := s.resume(c.Param("id")); e != nil {
			fail(c, e)
			return
		}
		c.JSON(202, gin.H{"message": "恢复已确认任务"})
	})
	api.POST("/projects/:id/upload", s.upload)
	api.GET("/projects/:id/export", s.export)
	api.GET("/projects/:id/events", func(c *gin.Context) {
		if _, e := s.store.project(c.Param("id")); e != nil {
			c.Status(404)
			return
		}
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("X-Accel-Buffering", "no")
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		last := ""
		for {
			p, e := s.store.project(c.Param("id"))
			if e != nil {
				return
			}
			h := hash(p)
			if h != last {
				c.SSEvent("project", p)
				c.Writer.Flush()
				last = h
			}
			select {
			case <-c.Request.Context().Done():
				return
			case <-s.ctx.Done():
				return
			case <-ticker.C:
			}
		}
	})
	r.GET("/media/*path", func(c *gin.Context) {
		rel := strings.TrimPrefix(c.Param("path"), "/")
		if strings.Contains(rel, "..") || strings.Contains(rel, "\\") || (strings.HasSuffix(rel, "response.bin") || strings.HasSuffix(rel, "response.bin.native")) || strings.Contains(rel, "/seedance/") {
			c.Status(403)
			return
		}
		c.File(s.abs(rel))
	})
	r.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.Status(404)
			return
		}
		path := filepath.Join(s.web, filepath.Clean("/"+c.Request.URL.Path))
		if info, e := os.Stat(path); e == nil && !info.IsDir() {
			c.File(path)
			return
		}
		index := filepath.Join(s.web, "index.html")
		if _, e := os.Stat(index); e == nil {
			c.File(index)
			return
		}
		c.String(404, "前端未构建，请先在 web 目录运行 npm run build，或打开 Vite 开发页面。")
	})
	return r
}
func (s *Studio) upload(c *gin.Context) {
	pid := c.Param("id")
	p, e := s.store.project(pid)
	if e != nil {
		fail(c, e)
		return
	}
	if p.Status == "running" {
		fail(c, errors.New("请先结束制作再上传"))
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 35<<20)
	if e = c.Request.ParseMultipartForm(8 << 20); e != nil {
		fail(c, errors.New("文件过大或表单无效"))
		return
	}
	if c.Request.MultipartForm != nil {
		defer c.Request.MultipartForm.RemoveAll()
	}
	f, _, e := c.Request.FormFile("file")
	if e != nil {
		fail(c, e)
		return
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, 30<<20))
	if e != nil || len(b) == 0 || len(b) >= 30<<20 {
		fail(c, errors.New("文件须小于30MB"))
		return
	}
	kind := c.PostForm("kind")
	ext := ""
	mime := http.DetectContentType(b)
	if kind == "music" {
		ext = ".audio"
	} else {
		switch mime {
		case "image/png":
			ext = ".png"
		case "image/jpeg":
			ext = ".jpg"
		default:
			fail(c, errors.New("上传参考图仅支持PNG或JPEG"))
			return
		}
	}
	rel := filepath.Join(pid, "imports", uid()+ext)
	if e = os.MkdirAll(filepath.Dir(s.abs(rel)), 0700); e != nil {
		fail(c, e)
		return
	}
	if e = atomicWrite(s.abs(rel), b); e != nil {
		fail(c, e)
		return
	}
	if _, e = s.probe(c.Request.Context(), s.abs(rel)); e != nil {
		fail(c, errors.New("素材无法解码"))
		return
	}
	e = s.store.change(func(st *State) error {
		p := st.Projects[pid]
		if p.Status == "running" {
			return errors.New("制作已开始，请稍后上传")
		}
		if kind == "music" {
			p.Music = rel
		} else {
			if len(p.References) >= 3 {
				return errors.New("最多上传3张参考图")
			}
			p.References = append(p.References, rel)
			if len(p.Boards) > 0 {
				p.Boards[len(p.Boards)-1].Approved = false
			}
		}
		p.Revision++
		return nil
	})
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(201, gin.H{"file": rel})
}
func (s *Studio) export(c *gin.Context) {
	p, e := s.store.project(c.Param("id"))
	if e != nil {
		fail(c, e)
		return
	}
	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", "attachment; filename=storyboard-"+p.ID+".zip")
	w := zip.NewWriter(c.Writer)
	defer w.Close()
	entry, _ := w.Create("project.json")
	b, _ := json.MarshalIndent(p, "", "  ")
	_, _ = entry.Write(b)
	seen := map[string]bool{}
	add := func(rel string) {
		if rel == "" || seen[rel] {
			return
		}
		seen[rel] = true
		f, e := os.Open(s.abs(rel))
		if e != nil {
			return
		}
		defer f.Close()
		out, e := w.Create(filepath.ToSlash(rel))
		if e == nil {
			_, _ = io.Copy(out, f)
		}
	}
	for _, a := range p.AssetSets {
		for _, c := range a.Characters {
			for _, v := range c.Views {
				add(v.Image)
				for _, r := range v.References {
					add(r.File)
				}
			}
		}
		for _, v := range append(append([]SceneAsset{}, a.Scenes...), a.Props...) {
			add(v.Image)
			for _, r := range v.References {
				add(r.File)
			}
		}
	}
	for _, b := range p.Boards {
		for _, ch := range b.Characters {
			add(ch.Image)
			for _, view := range ch.Views {
				add(view.Image)
			}
		}
		for _, a := range append(append([]SceneAsset{}, b.Scenes...), b.Props...) {
			add(a.Image)
		}
		for _, q := range b.Shots {
			for _, candidate := range q.Candidates {
				add(candidate.Image)
				for _, ref := range candidate.References {
					add(ref.File)
				}
			}
			add(q.Image)
			add(q.Video)
			add(q.Voice)
		}
	}
	for _, attempt := range p.Attempts {
		add(attempt.File)
		for _, ref := range attempt.References {
			add(ref.File)
		}
	}
	for _, r := range p.References {
		add(r)
	}
	add(p.Output)
	add(p.Music)
}
func main() {
	root := flag.String("root", "..", "仓库根目录（从server启动时默认..）")
	data := flag.String("data", "", "本地数据目录，默认<root>/data")
	config := flag.String("config", "", "配置文件路径，默认<root>/config.yaml")
	listen := flag.String("listen", "", "覆盖config.yaml中的监听地址")
	flag.Parse()
	absRoot, e := filepath.Abs(*root)
	if e != nil {
		log.Fatal(e)
	}
	if *config == "" {
		*config = filepath.Join(absRoot, "config.yaml")
	}
	settings, e := loadStudioConfig(*config)
	if e != nil {
		log.Fatal(e)
	}
	if *listen == "" {
		*listen = settings.listenAddress()
	}
	if *data == "" {
		*data = filepath.Join(absRoot, "data")
	}
	absData, e := filepath.Abs(*data)
	if e != nil {
		log.Fatal(e)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	s, e := newStudio(ctx, absRoot, absData, filepath.Join(absRoot, "web", "dist"))
	if e != nil {
		log.Fatal(e)
	}
	s.frontendPort = settings.Frontend.Port
	srv := &http.Server{Addr: *listen, Handler: s.router(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		s.mu.Lock()
		for _, cancel := range s.running {
			cancel()
		}
		s.mu.Unlock()
		shutdown, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		_ = srv.Shutdown(shutdown)
	}()
	log.Printf("短片工作台 http://%s（本机单人模式）", *listen)
	if e = srv.ListenAndServe(); e != nil && e != http.ErrServerClosed {
		log.Print(e)
		cancel()
	}
	s.wg.Wait()
	_ = s.store.close()
}
