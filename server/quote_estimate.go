package main

import (
	"fmt"
	"strconv"
	"strings"
)

// Estimates are saved with the quote's project/settings revision. Unknown still
// requires consent: the API's actual usage, rather than this estimate, is billed.
type QuoteEstimate struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
	Model    string `json:"model"`
	Basis    string `json:"basis"`
	Source   string `json:"source"`
	URL      string `json:"url"`
	Checked  string `json:"checked"`
}

func applyVideoEstimate(q *Quote, p Project, cfg Settings) {
	if cfg.Mode != "remote" || (q.Stage != "shot_video" && q.Stage != "videos") {
		return
	}
	q.EstimateReason = "当前厂商或模型没有已核实的计费单价，无法估算；自定义接入点 ID 也不能直接推断底层模型。"
	ep, err := resolveEndpoint(cfg, "video")
	if err != nil {
		return
	}
	var pricing *ModelPricing
	for _, vendor := range cfg.Vendors {
		if vendor.ID == ep.VendorID {
			pricing = officialModelPricing(vendor, ep.Model)
			break
		}
	}
	if pricing == nil {
		return
	}
	if q.Stage == "videos" && !dialogueMode(p) && p.Voice == "" {
		q.EstimateReason = "本次还包含独立旁白，旁白费用尚无法估算，不能将视频费用作为本次总额。"
		return
	}
	seconds := 0
	for _, input := range q.VideoInputs {
		if input.Mode == "continue" || input.Mode == "edit_video" {
			q.EstimateReason = "本次包含原视频输入计费及最低 Token 用量限制，当前尚未核实完整用量，无法可靠估算；请确认接受实际用量计费后继续。"
			return
		}
		found := false
		for _, shot := range p.Shots {
			if shot.ID == input.ShotID {
				seconds += shot.Duration
				found = true
				break
			}
		}
		if !found {
			return
		}
	}
	if seconds <= 0 {
		return
	}
	// Both video request builders currently submit 480p, with default 24 fps.
	// Pixel sizes follow the official model-specific table, not local render size.
	w, h := 864, 496
	if strings.Contains(ep.Model, "seedance-2-5") || strings.Contains(ep.Model, "seedance-2.5") {
		w, h = 854, 480
	} else if strings.Contains(ep.Model, "seedance-1") {
		w, h = 864, 480
	}
	if projectAspect(p) == "9:16" {
		w, h = h, w
	}
	row := pricing.Rows[0] // All supported tables start with the no-video 480p tier.
	rate, err := strconv.ParseFloat(row.Amount, 64)
	if err != nil || row.Currency != "CNY" || row.Unit != "百万视频 Token" {
		return
	}
	tokens := float64(seconds*w*h*24) / 1024
	q.Estimate = &QuoteEstimate{
		Amount: fmt.Sprintf("%.2f", tokens*rate/1_000_000), Currency: "CNY", Model: ep.Model,
		Basis:  fmt.Sprintf("480p · 按项目画幅 %s（%d×%d）、24帧/秒、合计%d秒、不含视频输入估算：约%.0f视频Token × ¥%s/百万Token。首帧自适应可能改变实际像素；以返回用量及厂商账单结算，非固定总价或费用上限。", projectAspect(p), w, h, seconds, tokens, row.Amount),
		Source: pricing.Source, URL: pricing.URL, Checked: pricing.Checked,
	}
	q.EstimateReason = ""
}
