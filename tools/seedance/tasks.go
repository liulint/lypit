package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Usage struct {
	Completion *int64 `json:"completion_tokens"`
	Total      *int64 `json:"total_tokens"`
}
type Task struct {
	ID         string `json:"id"`
	Status     string `json:"status"`
	Model      string `json:"model,omitempty"`
	Resolution string `json:"resolution,omitempty"`
	Ratio      string `json:"ratio,omitempty"`
	Duration   int    `json:"duration,omitempty"`
	Audio      *bool  `json:"generate_audio,omitempty"`
	Usage      *Usage `json:"usage,omitempty"`
	Content    struct {
		VideoURL string `json:"video_url,omitempty"`
	} `json:"content"`
	EstimatedFee  *float64 `json:"official_estimated_yuan"`
	FeeBasis      string   `json:"fee_basis"`
	File          string   `json:"downloaded_file,omitempty"`
	DownloadError string   `json:"download_error,omitempty"`
}
type Record struct {
	Version int    `json:"version"`
	Created string `json:"created_at"`
	Quote   Quote  `json:"quote"`
	Tasks   []Task `json:"tasks"`
}

type Engine struct {
	API   *http.Client
	Media *http.Client
	Base  string
	Key   string
}

func newEngine(key string) *Engine {
	return &Engine{Key: key, Base: apiBase, Media: publicClient(), API: &http.Client{Timeout: 90 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (e *Engine) call(ctx context.Context, method, path string, body []byte, target any) error {
	// GetBody deliberately unset: POSTs cannot be replayed by transport or redirects.
	var reader io.Reader
	if body != nil {
		reader = io.LimitReader(bytes.NewReader(body), int64(len(body)))
	}
	req, err := http.NewRequestWithContext(ctx, method, e.Base+path, reader)
	if err != nil {
		return errors.New("无法构建 API 请求")
	}
	req.Header.Set("Authorization", "Bearer "+e.Key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.API.Do(req)
	if err != nil {
		return errors.New("API 网络失败；POST 结果可能未知，不自动重试")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("API HTTP %d；为防止泄露凭据，不输出原始错误响应", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2_000_000)).Decode(target); err != nil {
		return errors.New("API 响应无法解析；如为 POST，请勿自动重提")
	}
	return nil
}

func saveRecord(path string, r *Record, initial bool) error {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if initial {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return errors.New("任务记录已存在或不可写；请使用 -resume，禁止覆盖")
		}
		_, err = f.Write(b)
		if err == nil {
			err = f.Sync()
		}
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		return syncDirectory(filepath.Dir(path))
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".run-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err != nil {
		return err
	}
	if ce != nil {
		return ce
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}
func syncDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
func lockDirectory(dir string) (func(), error) {
	path := filepath.Join(dir, ".seedance-lock")
	if err := os.Mkdir(path, 0700); err != nil {
		return nil, errors.New("任务目录已锁定或不可写；确认旧进程退出后，由 AI 检查并移除这个空锁目录")
	}
	return func() { _ = os.Remove(path) }, nil
}

func loadRecord(path string) (Record, error) {
	var r Record
	b, err := os.ReadFile(path)
	if err != nil {
		return r, errors.New("无法读取任务记录")
	}
	if err = json.Unmarshal(b, &r); err != nil {
		return r, errors.New("任务记录 JSON 无效")
	}
	if r.Version > 1 {
		return r, errors.New("任务记录版本较新，请更新工具")
	}
	// Legacy single-task records: usage may be absent. Never infer an input category or fee.
	if len(r.Tasks) == 0 {
		var old struct {
			Task
			TaskID string `json:"task_id"`
		}
		if json.Unmarshal(b, &old) != nil {
			return r, errors.New("无法解析旧任务记录")
		}
		if old.ID == "" {
			old.ID = old.TaskID
		}
		if old.ID == "" {
			return r, errors.New("记录中没有可恢复的任务")
		}
		r.Tasks = []Task{old.Task}
	}
	if len(r.Tasks) > 100 {
		return r, errors.New("任务记录数量异常")
	}
	for _, t := range r.Tasks {
		if t.ID != "" && !identifier.MatchString(t.ID) {
			return r, errors.New("任务 ID 无效")
		}
	}
	return r, nil
}

func (e *Engine) submit(ctx context.Context, o Options, q Quote, req Request) (*Record, string, error) {
	if !q.CanSubmit {
		return nil, "", errors.New("素材或计价模型尚未核实，不能提交")
	}
	if o.Confirm == "" || o.Confirm != q.ConfirmationID {
		return nil, "", errors.New("尚未确认本次最终参数：先 -quote，用户确认后原样传入 -confirm <confirmation_id>")
	}
	dir := o.Out
	if dir == "" {
		dir = filepath.Join("seedance-output", q.ConfirmationID)
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, "", err
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return nil, "", errors.New("无法创建输出目录")
	}
	if err = syncDirectory(filepath.Dir(dir)); err != nil {
		return nil, "", errors.New("输出目录无法可靠落盘，未提交任务")
	}
	unlock, err := lockDirectory(dir)
	if err != nil {
		return nil, "", err
	}
	defer unlock()
	path := filepath.Join(dir, "run.json")
	r := &Record{Version: 1, Created: time.Now().UTC().Format(time.RFC3339), Quote: q, Tasks: make([]Task, o.Count)}
	for i := range r.Tasks {
		r.Tasks[i].Status = "not_submitted"
	}
	if err = saveRecord(path, r, true); err != nil {
		return nil, path, err
	}
	body, _ := json.Marshal(req)
	for i := range r.Tasks {
		// Durable write-ahead intent. A crash after this point must never trigger a new POST.
		r.Tasks[i].Status = "submission_unknown"
		if err = saveRecord(path, r, false); err != nil {
			return r, path, err
		}
		var created struct {
			ID string `json:"id"`
		}
		if err = e.call(ctx, http.MethodPost, "", body, &created); err != nil {
			return r, path, err
		}
		if !identifier.MatchString(created.ID) {
			return r, path, errors.New("POST 未返回有效任务 ID，结果未知；不要自动重提")
		}
		r.Tasks[i].ID = created.ID
		r.Tasks[i].Status = "submitted"
		if err = saveRecord(path, r, false); err != nil {
			return r, path, fmt.Errorf("任务 %s 已创建但保存失败；保留该 ID，禁止重提", created.ID)
		}
	}
	err = e.finish(ctx, o, r, path)
	return r, path, err
}
func (e *Engine) resume(ctx context.Context, o Options) (*Record, string, error) {
	path, err := filepath.Abs(o.Resume)
	if err != nil {
		return nil, "", err
	}
	unlock, err := lockDirectory(filepath.Dir(path))
	if err != nil {
		return nil, path, err
	}
	defer unlock()
	r, err := loadRecord(path)
	if err != nil {
		return nil, path, err
	}
	err = e.finish(ctx, o, &r, path)
	return &r, path, err
}
func (e *Engine) finish(ctx context.Context, o Options, r *Record, path string) error {
	ctx, cancel := context.WithTimeout(ctx, o.Wait)
	defer cancel()
	var problems []string
	for i := range r.Tasks {
		t := &r.Tasks[i]
		if t.ID == "" {
			problems = append(problems, fmt.Sprintf("第 %d 条没有已保存的任务 ID（%s），恢复不会补提", i+1, t.Status))
			continue
		}
		// A completed local download remains usable after the remote task expires.
		if t.Status == "succeeded" && t.File == t.ID+".mp4" {
			if info, err := os.Lstat(filepath.Join(filepath.Dir(path), t.File)); err == nil && info.Mode().IsRegular() && info.Size() > 0 {
				calculateFee(t, r.Quote)
				if err := saveRecord(path, r, false); err != nil {
					return err
				}
				continue
			}
		}
		for {
			var actual Task
			if err := e.call(ctx, http.MethodGet, "/"+t.ID, nil, &actual); err != nil {
				problems = append(problems, err.Error())
				break
			}
			if actual.ID != "" && actual.ID != t.ID {
				problems = append(problems, "查询结果 ID 不一致")
				break
			}
			actual.ID = t.ID
			// Successful persisted usage survives an incomplete subsequent response.
			if actual.Usage == nil {
				actual.Usage = t.Usage
			} else if t.Usage != nil {
				if actual.Usage.Completion == nil {
					actual.Usage.Completion = t.Usage.Completion
				}
				if actual.Usage.Total == nil {
					actual.Usage.Total = t.Usage.Total
				}
			}
			if actual.Model == "" {
				actual.Model = t.Model
			}
			if actual.Resolution == "" {
				actual.Resolution = t.Resolution
			}
			if actual.Ratio == "" {
				actual.Ratio = t.Ratio
			}
			if actual.Duration == 0 {
				actual.Duration = t.Duration
			}
			if actual.Audio == nil {
				actual.Audio = t.Audio
			}
			if actual.Status == "succeeded" && actual.Content.VideoURL == "" {
				actual.Content = t.Content
			}
			actual.File = t.File
			*t = actual
			calculateFee(t, r.Quote)
			// Persist usage and pricing BEFORE any attempt to download.
			if err := saveRecord(path, r, false); err != nil {
				return err
			}
			switch t.Status {
			case "succeeded":
				if err := e.download(ctx, t, filepath.Dir(path)); err != nil {
					t.DownloadError = err.Error()
					problems = append(problems, err.Error())
				}
				if err := saveRecord(path, r, false); err != nil {
					return err
				}
			case "failed", "cancelled", "expired":
				problems = append(problems, "任务 "+t.ID+" 状态为 "+t.Status)
			case "queued", "running", "submitted":
				timer := time.NewTimer(o.Poll)
				select {
				case <-ctx.Done():
					timer.Stop()
					return errors.New("等待超时或中断；记录已保存，使用 -resume 继续同一任务")
				case <-timer.C:
					continue
				}
			default:
				problems = append(problems, "任务返回未知状态；记录已保存，请检查 API 文档后恢复")
			}
			break
		}
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "；"))
	}
	return nil
}
func calculateFee(t *Task, q Quote) {
	t.EstimatedFee = nil
	t.FeeBasis = "用量未返回；不从 total_tokens 或预估值推算实际费用"
	if t.Status != "succeeded" {
		t.FeeBasis = "尚无成功生成结果，不累计预估费用"
		return
	}
	if t.Usage == nil || t.Usage.Completion == nil {
		return
	}
	if *t.Usage.Completion < 0 {
		t.FeeBasis = "completion_tokens 无效"
		return
	}
	if q.Unit == nil {
		t.FeeBasis = "缺少已确认的计价依据（可能是旧记录），不推断费用"
		return
	}
	if f := family(t.Model); t.Model != "" && t.Model != q.Options.Model && (f == "" || f != q.Options.PriceModel) {
		t.FeeBasis = "返回模型与确认的计价模型不一致，费用未知"
		return
	}
	if t.Resolution != "" && t.Resolution != q.Options.Resolution {
		t.FeeBasis = "返回分辨率与确认规格不一致，费用未知"
		return
	}
	v := float64(*t.Usage.Completion) * (*q.Unit) / 1e6
	t.EstimatedFee = &v
	t.FeeBasis = fmt.Sprintf("usage.completion_tokens × %.2f 元/百万 token；刊例价，非账户账单。来源 %s，核实 %s", *q.Unit, q.Source, q.Verified)
}
func (e *Engine) download(ctx context.Context, t *Task, dir string) error {
	final := filepath.Join(dir, t.ID+".mp4")
	if info, err := os.Lstat(final); err == nil {
		if t.File == filepath.Base(final) && info.Mode().IsRegular() && info.Size() > 0 {
			return nil
		}
		return errors.New("目标视频已存在，禁止覆盖；请核实已有文件")
	} else if !os.IsNotExist(err) {
		return errors.New("无法检查目标视频")
	}
	if err := publicURL(t.Content.VideoURL); err != nil {
		return errors.New("结果缺少有效公网下载地址；可恢复查询")
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, t.Content.VideoURL, nil)
	resp, err := e.Media.Do(req)
	if err != nil {
		return errors.New("视频下载失败，可 -resume 重试下载")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("视频下载 HTTP %d；用量记录已保留", resp.StatusCode)
	}
	f, err := os.CreateTemp(dir, ".download-*.part")
	if err != nil {
		return errors.New("无法创建下载临时文件")
	}
	defer os.Remove(f.Name())
	n, err := io.Copy(f, io.LimitReader(resp.Body, 1_000_000_001))
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil || n == 0 || n > 1_000_000_000 || (resp.ContentLength >= 0 && n != resp.ContentLength) {
		return errors.New("视频下载不完整或超过 1 GB；用量记录已保留")
	}
	// Hard link publishes atomically without replacing a pre-existing file.
	if err = os.Link(f.Name(), final); err != nil {
		return errors.New("无法安装下载文件（已存在或文件系统不支持硬链接），禁止覆盖")
	}
	if err = syncDirectory(dir); err != nil {
		return errors.New("下载文件已写入但目录同步失败，请核实文件，禁止覆盖")
	}
	t.File = filepath.Base(final)
	t.DownloadError = ""
	return nil
}
