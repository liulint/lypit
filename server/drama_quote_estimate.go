package main

import (
	"fmt"
	"strconv"
	"strings"
)

func applyDramaImageCallScope(q *DramaQuote, cfg Settings) {
	if cfg.Mode != "remote" {
		return
	}
	items := []DramaProduction{q.Spec}
	if q.Spec.Kind == "batch" {
		items = q.Spec.Items
	}
	maxCalls := 0
	for _, p := range items {
		if p.Kind != "image" {
			return
		}
		maxCalls++
		if p.ImageMaxCalls == 2 && p.ImageAspect != "" && p.ImageRetryPrompt != "" {
			maxCalls++
		}
	}
	if maxCalls > len(items) {
		q.Reason = fmt.Sprintf("费用未知，无可靠上限；交付%d张图片，常规%d次、最多%d次模型调用。仅已返回图片画幅不符时自动重生1次，每次均可能计费；结果未知不重提，两次仍不合格则停止并保留回执。", len(items), len(items), maxCalls)
	}
}

// Adapt the immutable production scope to the shared video estimator. Only the
// items in this quote count; previously generated media are outside its scope.
func applyDramaVideoEstimate(q *DramaQuote, w DramaWork, cfg Settings) {
	if cfg.Mode != "remote" {
		return
	}
	items := []DramaProduction{q.Spec}
	if q.Spec.Kind == "batch" {
		items = q.Spec.Items
	}
	if len(items) == 0 {
		return
	}
	p := Project{VideoResolution: dramaProductionResolution(&q.Spec), AspectRatio: w.Aspect, AudioMode: "dialogue"}
	legacy := Quote{Stage: "videos"}
	for _, item := range items {
		if item.Kind != "video" {
			return // Never present a partial video estimate as a mixed-media total.
		}
		if item.Duration <= 0 || item.Duration > 600 || item.Duration != float64(int(item.Duration)) {
			return
		}
		p.Shots = append(p.Shots, Shot{ID: item.Entry, Duration: int(item.Duration)})
		legacy.VideoInputs = append(legacy.VideoInputs, VideoInput{ShotID: item.Entry, Mode: item.Mode})
	}
	applyVideoEstimate(&legacy, p, cfg)
	q.Estimate, q.EstimateReason = legacy.Estimate, legacy.EstimateReason
	if q.Spec.Kind == "batch" {
		var cents int64
		for _, item := range items {
			one := Quote{Stage: "shot_video", VideoInputs: []VideoInput{{ShotID: item.Entry, Mode: item.Mode}}}
			applyVideoEstimate(&one, Project{VideoResolution: dramaProductionResolution(&item), AspectRatio: w.Aspect, AudioMode: "dialogue", Shots: []Shot{{ID: item.Entry, Duration: int(item.Duration)}}}, cfg)
			q.ItemEstimates = append(q.ItemEstimates, DramaItemEstimate{Entry: item.Entry, Estimate: one.Estimate, Reason: one.EstimateReason})
			if one.Estimate != nil {
				// Estimator amounts have exactly two decimals. Sum displayed cents
				// so the reviewed rows and total cannot differ due to rounding.
				amount, err := strconv.ParseInt(strings.ReplaceAll(one.Estimate.Amount, ".", ""), 10, 64)
				if err != nil {
					q.Estimate = nil
					q.EstimateReason = "逐镜费用汇总失败，无法可靠估算总额。"
				} else {
					cents += amount
				}
			} else {
				q.Estimate = nil
			}
		}
		if q.Estimate != nil {
			q.Estimate.Amount = fmt.Sprintf("%d.%02d", cents/100, cents%100)
			q.Estimate.Basis += " 逐镜估算四舍五入至分后合计；仅包含本次待生成镜头。"
		}
	}
	if q.Estimate != nil {
		q.Reason = "已按本次视频范围估算费用；实际按厂商用量结算，非固定总价或费用上限。每项仅执行一次。"
	}
}
