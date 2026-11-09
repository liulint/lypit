package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	urlpath "path"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
)

type Studio struct {
	dataGate    sync.RWMutex
	dramaOnly   bool
	dramaMu     sync.Mutex
	catalogMu   sync.Mutex
	catalog     map[string]ModelCatalog
	toolMu      sync.Mutex
	toolInstall ToolInstallState
	store       *Store
	root, data  string
	ctx         context.Context
	mu          sync.Mutex
	running     map[string]context.CancelFunc
	wg          sync.WaitGroup
}

func newStudio(ctx context.Context, root, data string) (*Studio, error) {
	st, e := openStore(data)
	if e != nil {
		return nil, e
	}
	s := &Studio{store: st, root: root, data: data, ctx: ctx, running: map[string]context.CancelFunc{}}
	if e := s.initDrama(); e != nil {
		st.close()
		return nil, e
	}
	return s, nil
}
func main() {
	root := flag.String("root", "..", "项目根目录（默认从server目录启动）")
	data := flag.String("data", "", "SQLite与素材目录，默认<root>/data")
	userData := flag.Bool("user-data", false, "使用系统用户数据目录（发行版）")
	openBrowser := flag.Bool("open-browser", false, "启动成功后打开默认浏览器")
	listen := flag.String("listen", "127.0.0.1:8080", "本机监听地址")
	freshDrama := flag.Bool("reset-drama", false, "归档旧数据，仅保留厂商、模型、并发和密文凭据后启动新流程")
	flag.Parse()
	host, _, e := net.SplitHostPort(*listen)
	if e != nil {
		log.Fatal(e)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		log.Fatal("无登录模式仅允许监听本机回环地址")
	}
	*root, e = filepath.Abs(*root)
	if e != nil {
		log.Fatal(e)
	}
	if *data == "" {
		if *userData {
			*data, e = userDataDir()
			if e != nil {
				log.Fatal(e)
			}
		} else {
			*data = filepath.Join(*root, "data")
		}
	}
	*data, e = filepath.Abs(*data)
	if e != nil {
		log.Fatal(e)
	}
	if *freshDrama {
		archive, err := resetDramaData(*data)
		if err != nil {
			log.Fatal(err)
		}
		if archive != "" {
			log.Printf("旧数据已归档：%s", archive)
		}
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	s, e := newStudio(ctx, *root, *data)
	if e != nil {
		log.Fatal(e)
	}
	s.dramaOnly = true
	log.Printf("用户数据目录：%s", s.data)
	srv := &http.Server{Addr: *listen, Handler: s.router(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		s.mu.Lock()
		for _, c := range s.running {
			c()
		}
		s.mu.Unlock()
		stop, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		_ = srv.Shutdown(stop)
	}()
	if e = serveLocalHTTP(ctx, srv, *openBrowser, openDefaultBrowser); e != nil && !errors.Is(e, http.ErrServerClosed) {
		log.Print(e)
		cancel()
	}
	<-shutdownDone
	s.wg.Wait()
	if e = s.store.close(); e != nil {
		log.Print(e)
	}
}
func (s *Studio) media(file string) string {
	return filepath.Join(s.data, "media", filepath.FromSlash(file))
}
func result(c *gin.Context, v any, e error) {
	if e != nil {
		code := 400
		if errors.Is(e, errConflict) {
			code = 409
		}
		c.JSON(code, gin.H{"error": dbError(e).Error()})
		return
	}
	c.JSON(200, v)
}
func bind(c *gin.Context, v any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2<<20)
	if e := c.ShouldBindJSON(v); e != nil {
		c.JSON(400, gin.H{"error": "请求内容不完整或格式错误"})
		return false
	}
	return true
}
func (s *Studio) router() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(func(c *gin.Context) {
		// Loopback binding alone does not protect a local API against a malicious webpage.
		host, _, e := net.SplitHostPort(c.Request.Host)
		if e != nil {
			host = c.Request.Host
		}
		if host != "localhost" && host != "127.0.0.1" && host != "::1" {
			c.AbortWithStatus(403)
			return
		}
		if c.Request.Method != "GET" && c.Request.Method != "HEAD" && c.GetHeader("X-Lypit-Client") != "studio" {
			c.AbortWithStatusJSON(403, gin.H{"error": "请从工作台提交操作"})
			return
		}
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("Cache-Control", "no-store")
		c.Next()
	})
	a := r.Group("/api")
	a.Use(func(c *gin.Context) {
		if s.dramaOnly {
			p := c.Request.URL.Path
			if strings.HasPrefix(p, "/api/projects") || strings.HasPrefix(p, "/api/prompts") || (strings.HasPrefix(p, "/api/quotes/")) {
				c.AbortWithStatusJSON(410, gin.H{"error": "旧创作流程已停用，请使用Drama Skills工作台"})
				return
			}
		}
		c.Next()
	})
	a.Use(func(c *gin.Context) {
		if c.Request.URL.Path == "/api/system/data/backup" {
			s.dataGate.Lock()
			defer s.dataGate.Unlock()
		} else {
			s.dataGate.RLock()
			defer s.dataGate.RUnlock()
		}
		c.Next()
	})
	s.dataRoutes(a)
	s.dramaRoutes(a)
	a.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true, "mode": "local-single-user", "database": "sqlite"})
	})
	a.GET("/projects", func(c *gin.Context) { v, e := s.store.projects(); result(c, v, e) })
	a.POST("/projects", func(c *gin.Context) {
		var v struct {
			Title       string  `json:"title"`
			Idea        string  `json:"idea"`
			Style       string  `json:"style"`
			AspectRatio *string `json:"aspect_ratio"`
			Duration    *int    `json:"duration"`
			AudioMode   *string `json:"audio_mode"`
		}
		if !bind(c, &v) {
			return
		}
		v.Title = strings.TrimSpace(v.Title)
		v.Idea = strings.TrimSpace(v.Idea)
		if v.Title == "" || len([]rune(v.Title)) > 80 || v.Idea == "" || len(v.Idea) > 20000 {
			result(c, nil, errors.New("请填写标题（最多80字）和故事（最多20000字节）"))
			return
		}
		if v.Style == "" {
			v.Style = "温暖电影感"
		}
		aspect, duration := defaultAspect, defaultDuration
		if v.AspectRatio != nil {
			aspect = *v.AspectRatio
		}
		if v.Duration != nil {
			duration = *v.Duration
		}
		mode := "dialogue"
		if v.AudioMode != nil {
			mode = *v.AudioMode
		}
		p, e := s.store.create(v.Title, v.Idea, v.Style, aspect, duration, mode)
		result(c, p, e)
	})
	a.GET("/projects/:id", func(c *gin.Context) { v, e := s.store.project(c.Param("id")); result(c, v, e) })
	a.PUT("/projects/:id", s.editProject)
	a.POST("/projects/:id/assets/:asset/approve", s.approveAsset)
	a.POST("/projects/:id/assets/lock", func(c *gin.Context) { s.lockProject(c, "assets") })
	a.PUT("/projects/:id/board-plan", s.reviewBoardPlan)
	a.POST("/projects/:id/boards/lock", func(c *gin.Context) { s.lockProject(c, "boards") })
	a.PUT("/projects/:id/shots/:shot", s.editShot)
	a.GET("/projects/:id/shots/:shot/duration", s.shotDurationInfo)
	a.PUT("/projects/:id/shots/:shot/duration", s.editShotDuration)
	a.PUT("/projects/:id/shots/:shot/content", s.editShotContent)
	a.GET("/tasks/:id/rewrite", s.getShotRewrite)
	a.POST("/tasks/:id/rewrite/apply", s.applyShotRewrite)
	a.GET("/projects/:id/history", func(c *gin.Context) { v, e := s.store.history(c.Param("id")); result(c, v, e) })
	a.GET("/projects/:id/export", s.exportProject)
	a.POST("/projects/:id/quotes", s.quoteHandler)
	a.POST("/quotes/:id/confirm", s.confirmHandler)
	a.GET("/tasks/:id/preview", s.boardPreview)
	a.GET("/usage", func(c *gin.Context) { v, e := s.usageRecords(); result(c, v, e) })
	a.GET("/tasks", func(c *gin.Context) {
		v, e := s.store.tasks(c.Query("project_id"))
		for i := range v {
			t := &v[i]
			if (t.Stage == "board_retry" || t.Stage == "board_edit") && t.ShotID != "" && (t.Status == "failed" || t.Status == "interrupted" || t.Status == "cancelled") {
				suffix := "retry"
				if t.Stage == "board_edit" {
					suffix = "edit"
				}
				dir := filepath.Join(s.data, "cli", t.ID, "shot-"+t.ShotID+"-"+suffix)
				t.Message = cliImageFailureReason(dir, errors.New(t.Message)).Error()
			}
		}
		result(c, v, e)
	})
	a.POST("/tasks/:id/:action", s.taskAction)
	a.GET("/prompts", s.getPrompts)
	a.PUT("/prompts", s.putPrompts)
	a.GET("/settings", s.getSettings)
	a.PUT("/settings", s.putSettings)
	a.GET("/vendors/:id/models", s.vendorModels)
	a.GET("/system", s.systemInfo)
	a.POST("/system/tools/install", s.installTools)
	r.GET("/media/*file", func(c *gin.Context) {
		file := strings.TrimPrefix(c.Param("file"), "/")
		if !safeMedia(file) {
			c.Status(404)
			return
		}
		c.File(s.media(file))
	})
	r.NoRoute(func(c *gin.Context) {
		if c.Request.Method != "GET" || strings.HasPrefix(c.Request.URL.Path, "/api") || strings.HasPrefix(c.Request.URL.Path, "/media") {
			c.Status(404)
			return
		}
		web := filepath.Join(s.root, "web", "dist")
		if strings.ContainsAny(c.Request.URL.Path, "\\:") {
			c.Status(404)
			return
		}
		file := filepath.Join(web, filepath.FromSlash(strings.TrimPrefix(urlpath.Clean("/"+c.Request.URL.Path), "/")))
		if info, e := os.Stat(file); e == nil && !info.IsDir() {
			c.File(file)
			return
		}
		// Missing versioned chunks must not receive the SPA's HTML document.
		if strings.HasPrefix(c.Request.URL.Path, "/assets/") {
			c.Status(404)
			return
		}
		index := filepath.Join(web, "index.html")
		if _, e := os.Stat(index); e == nil {
			c.File(index)
			return
		}
		c.String(503, "前端文件缺失，请完整解压发行包；源码开发请运行对应系统的 scripts/start 启动脚本。")
	})
	return r
}
func safeMedia(file string) bool {
	if file == "" || strings.ContainsAny(file, "\\:") || strings.Contains(file, "..") || strings.HasPrefix(file, "/") {
		return false
	}
	switch strings.ToLower(filepath.Ext(file)) {
	case ".png", ".jpg", ".jpeg", ".webp", ".mp4", ".wav", ".mp3", ".srt":
		return true
	}
	return false
}
