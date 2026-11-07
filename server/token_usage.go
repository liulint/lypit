package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
)

// Only numeric usage survives CLI stdout; prompts, tool output and credentials do not.
type tokenCounts struct {
	Input  *int64 `json:"input_tokens,omitempty"`
	Output *int64 `json:"output_tokens,omitempty"`
	Cached *int64 `json:"cached_input_tokens,omitempty"`
	Total  *int64 `json:"total_tokens,omitempty"`
}

type cliUsageWriter struct {
	line    []byte
	discard bool
	counts  *tokenCounts
}

func (w *cliUsageWriter) Write(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 {
		i := bytes.IndexByte(p, '\n')
		end := i
		if end < 0 {
			end = len(p)
		}
		if len(w.line)+end > 2<<20 {
			w.discard = true
			w.line = nil
		}
		if !w.discard {
			w.line = append(w.line, p[:end]...)
		}
		if i < 0 {
			break
		}
		w.finish()
		p = p[i+1:]
	}
	return n, nil
}

func (w *cliUsageWriter) finish() {
	var event struct {
		Type  string       `json:"type"`
		Usage *tokenCounts `json:"usage"`
	}
	if !w.discard && json.Unmarshal(w.line, &event) == nil && event.Type == "turn.completed" && event.Usage != nil {
		if w.counts == nil {
			w.counts = &tokenCounts{}
		}
		add := func(dst **int64, src *int64) {
			if src == nil || *src < 0 {
				return
			}
			if *dst == nil {
				v := int64(0)
				*dst = &v
			}
			if **dst <= (1<<63-1)-*src {
				**dst += *src
			}
		}
		add(&w.counts.Input, event.Usage.Input)
		add(&w.counts.Output, event.Usage.Output)
		add(&w.counts.Cached, event.Usage.Cached)
		if event.Usage.Input != nil && event.Usage.Output != nil && *event.Usage.Input >= 0 && *event.Usage.Output >= 0 && *event.Usage.Input <= (1<<63-1)-*event.Usage.Output {
			total := *event.Usage.Input + *event.Usage.Output
			add(&w.counts.Total, &total)
		}
	}
	w.line = nil
	w.discard = false
}

type UsageRecord struct {
	ID        string `json:"id"`
	TaskID    string `json:"task_id"`
	ProjectID string `json:"project_id"`
	Stage     string `json:"stage"`
	Status    string `json:"status"`
	Title     string `json:"title,omitempty"`
	Vendor    string `json:"vendor"`
	Model     string `json:"model"`
	Transport string `json:"transport"`
	Recorded  string `json:"recorded"`
	tokenCounts
	VideoTokens *int64         `json:"video_tokens,omitempty"`
	Cost        *QuoteEstimate `json:"cost,omitempty"`
	Reason      string         `json:"reason"`
}

// Read immutable task configuration, never today's vendor settings. Return a public allowlist.
// Attempts are one row per submission, including interrupted and failed requests.
func (s *Studio) usageRecords() ([]UsageRecord, error) {
	rows, err := s.store.db.Query(`SELECT a.id,a.status,CASE WHEN json_valid(a.response) THEN json_object('usage',json_extract(a.response,'$.usage'),'model',json_extract(a.response,'$.model')) ELSE NULL END,t.body,t.config FROM attempts a JOIN tasks t ON t.id=a.task_id ORDER BY a.rowid DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []UsageRecord{}
	for rows.Next() {
		var id, state string
		var response, body, config []byte
		if err := rows.Scan(&id, &state, &response, &body, &config); err != nil {
			return nil, err
		}
		var t Task
		if err := json.Unmarshal(body, &t); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(config, &t.Config); err != nil {
			return nil, err
		}
		key := strings.TrimPrefix(id, t.ID+":")
		cap := "image"
		if t.Stage == "analyze" || t.Stage == "board_plan" || t.Stage == "shot_rewrite" {
			cap = "text"
		}
		if strings.HasPrefix(key, "video-") {
			cap = "video"
		}
		ep, _ := resolveEndpoint(t.Config, cap)
		u := UsageRecord{ID: id, TaskID: t.ID, ProjectID: t.ProjectID, Stage: t.Stage, Status: t.Status, Model: ep.Model, Transport: ep.Transport, Recorded: t.Created, Vendor: "历史厂商未记录", Reason: "供应商未返回或历史未记录用量；费用未知，请查看厂商账单。"}
		for _, v := range t.Config.Vendors {
			if v.ID == ep.VendorID {
				u.Vendor = v.Name
				break
			}
		}
		if cap == "video" {
			for _, v := range t.VideoUsage {
				if key == "video-"+v.ShotID {
					u.Title = v.Title
					u.Model = v.Model
					u.VideoTokens = v.CompletionTokens
					u.Total = v.TotalTokens
					u.Cost = v.Cost
					u.Reason = v.Reason
					u.Recorded = v.Recorded
					break
				}
			}
		} else if ep.Transport == "cli" {
			// The key comes from our durable attempt ID, never from a request parameter.
			if filepath.Base(key) == key && key != "." && key != ".." {
				if b, err := readCLIFile(filepath.Join(s.data, "cli", t.ID, key, "usage.json")); err == nil {
					_ = json.Unmarshal(b, &u.tokenCounts)
				}
			}
			u.Reason = "CLI 未提供实际账单价格；Token 仅代表规划模型用量，可能不含生图等工具费用。历史未记录的 Token 无法补算。"
		} else if state == "received" {
			var envelope struct {
				Model string `json:"model"`
				Usage *struct {
					tokenCounts
					Prompt     *int64 `json:"prompt_tokens"`
					Completion *int64 `json:"completion_tokens"`
					Details    struct {
						Cached *int64 `json:"cached_tokens"`
					} `json:"prompt_tokens_details"`
				} `json:"usage"`
			}
			if json.Unmarshal(response, &envelope) == nil && envelope.Usage != nil {
				v := envelope.Usage
				if envelope.Model != "" {
					u.Model = envelope.Model
				}
				u.tokenCounts = v.tokenCounts
				if u.Input == nil {
					u.Input = v.Prompt
				}
				if u.Output == nil {
					u.Output = v.Completion
				}
				if u.Cached == nil {
					u.Cached = v.Details.Cached
				}
			}
		}
		u.Input = validVideoTokens(u.Input)
		u.Output = validVideoTokens(u.Output)
		u.Cached = validVideoTokens(u.Cached)
		u.Total = validVideoTokens(u.Total)
		out = append(out, u)
	}
	return out, rows.Err()
}
