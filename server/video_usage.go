package main

import (
	"fmt"
	"math/big"
	"strings"
)

// Only these public fields are exported; never persist the download URL or credentials here.
type VideoUsage struct {
	ShotID           string         `json:"shot_id"`
	Title            string         `json:"title"`
	RemoteID         string         `json:"remote_id"`
	Model            string         `json:"model"`
	CompletionTokens *int64         `json:"completion_tokens,omitempty"`
	TotalTokens      *int64         `json:"total_tokens,omitempty"`
	Cost             *QuoteEstimate `json:"cost,omitempty"`
	Reason           string         `json:"reason,omitempty"`
	Recorded         string         `json:"recorded"`
}

type videoTaskResult struct {
	Status      string `json:"status"`
	Model       string `json:"model"`
	Resolution  string `json:"resolution"`
	ServiceTier string `json:"service_tier"`
	Draft       bool   `json:"draft"`
	Content     struct {
		URL string `json:"video_url"`
	} `json:"content"`
	Usage *struct {
		CompletionTokens *int64 `json:"completion_tokens"`
		TotalTokens      *int64 `json:"total_tokens"`
	} `json:"usage"`
}

func validVideoTokens(n *int64) *int64 {
	if n == nil || *n < 0 {
		return nil
	}
	return n
}

func videoUsage(t Task, q Shot, ep Endpoint, remoteID string, result videoTaskResult) VideoUsage {
	u := VideoUsage{ShotID: q.ID, Title: q.Title, RemoteID: remoteID, Model: result.Model, Recorded: now()}
	if u.Model == "" {
		u.Model = ep.Model
	}
	if result.Usage != nil {
		u.CompletionTokens = validVideoTokens(result.Usage.CompletionTokens)
		u.TotalTokens = validVideoTokens(result.Usage.TotalTokens)
	}
	u.Reason = "未返回可用的视频生成 Token，无法折算费用；请查看厂商账单。"
	if u.CompletionTokens == nil {
		return u
	}
	u.Reason = "当前厂商或模型单价未知，无法折算费用；请查看厂商账单。"
	var pricing *ModelPricing
	for _, v := range t.Config.Vendors {
		if v.ID == ep.VendorID {
			pricing = officialModelPricing(v, u.Model)
			break
		}
	}
	if pricing == nil {
		return u
	}
	if result.Draft || (result.ServiceTier != "" && result.ServiceTier != "default") {
		u.Reason = "当前草稿或服务档位的单价尚未核实，无法折算费用。"
		return u
	}
	resolution := result.Resolution
	if resolution == "" {
		resolution = "480p" // Both current and historical request builders submit 480p.
	}
	condition := "不含视频输入"
	if input, ok := inputFor(t, q.ID); ok && (input.Mode == "continue" || input.Mode == "edit_video") {
		condition = "含视频输入"
	}
	u.Reason = "返回的分辨率没有已核实单价，无法折算费用。"
	for _, row := range pricing.Rows {
		if row.Condition != "在线推理" {
			parts := strings.Split(row.Condition, " · ")
			if len(parts) != 2 || parts[1] != condition {
				continue
			}
			matched := false
			for _, res := range strings.Split(parts[0], " / ") {
				matched = matched || res == resolution
			}
			if !matched {
				continue
			}
		}
		rate, ok := new(big.Rat).SetString(row.Amount)
		if !ok || rate.Sign() < 0 || row.Currency != "CNY" || row.Unit != "百万视频 Token" {
			continue
		}
		amount := new(big.Rat).Mul(new(big.Rat).SetInt64(*u.CompletionTokens), rate)
		amount.Quo(amount, big.NewRat(1_000_000, 1))
		u.Cost = &QuoteEstimate{
			Amount: amount.FloatString(4), Currency: row.Currency, Model: u.Model,
			Basis:  fmt.Sprintf("厂商返回 %d 视频生成Token × ¥%s/百万Token（%s）。仅视频费用参考折算，未含独立旁白、工具调用、合同折扣及最低用量调整；以厂商账单为准。", *u.CompletionTokens, row.Amount, row.Condition),
			Source: pricing.Source, URL: pricing.URL, Checked: pricing.Checked,
		}
		u.Reason = ""
		return u
	}
	return u
}

func (s *Studio) recordVideoUsage(t *Task, q Shot, ep Endpoint, remoteID string, result videoTaskResult) error {
	for _, existing := range t.VideoUsage {
		if existing.ShotID == q.ID && existing.RemoteID == remoteID {
			return nil // Keep the recorded rate/date unchanged on recovery.
		}
	}
	u := videoUsage(*t, q, ep, remoteID, result)
	if t.usageRecorder != nil {
		return t.usageRecorder(u)
	}
	t.VideoUsage = append(t.VideoUsage, u)
	return s.store.saveTask(*t)
}
