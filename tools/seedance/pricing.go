package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
)

const pricingSource = "https://ark.volcengine.com/region:cn-beijing/docs/model-pricing.md"
const apiSource = "https://ark.volcengine.com/region:cn-beijing/docs/create-video-generation-task-api.md"
const minimumSource = "https://bytedance.larkoffice.com/wiki/FXaYwxzJ5i5Zdik32ipcWzt7nxd?table=tblheIYDXXwYM43b&view=vew2CR2JYM"
const verifiedDate = "2026-10-21"

var rates = map[string][2]float64{"mini": {23, 14}, "fast": {37, 22}, "standard": {46, 28}}
var dimensions = map[string]map[string][2]int{
	"480p": {"16:9": {864, 496}, "4:3": {752, 560}, "1:1": {640, 640}, "3:4": {560, 752}, "9:16": {496, 864}, "21:9": {992, 432}},
	"720p": {"16:9": {1280, 720}, "4:3": {1112, 834}, "1:1": {960, 960}, "3:4": {834, 1112}, "9:16": {720, 1280}, "21:9": {1470, 630}},
}

type Quote struct {
	Options        Options  `json:"parameters"`
	Media          []Media  `json:"reference_materials"`
	CanSubmit      bool     `json:"can_submit"`
	ConfirmationID string   `json:"confirmation_id"`
	Unit           *float64 `json:"yuan_per_million_tokens"`
	Width          int      `json:"width"`
	Height         int      `json:"height"`
	FPS            int      `json:"fps"`
	FormulaTokens  *int64   `json:"formula_tokens_per_video"`
	MinYuan        *float64 `json:"estimated_yuan_per_video_min"`
	MaxYuan        *float64 `json:"estimated_yuan_per_video_max"`
	TotalMin       *float64 `json:"estimated_yuan_total_min"`
	TotalMax       *float64 `json:"estimated_yuan_total_max"`
	Basis          string   `json:"basis"`
	Source         string   `json:"pricing_source"`
	APISource      string   `json:"api_source"`
	MinimumSource  string   `json:"minimum_token_source,omitempty"`
	Verified       string   `json:"verified_date"`
	Warnings       []string `json:"warnings"`
}

func makeQuote(o Options, media []Media, warnings []string, valid bool) Quote {
	d := dimensions[o.Resolution][o.Ratio]
	q := Quote{Options: o, Media: media, CanSubmit: valid && o.PriceModel != "", Width: d[0], Height: d[1], FPS: 24, Source: pricingSource, APISource: apiSource, Verified: verifiedDate, Warnings: warnings}
	q.Basis = "按官方刊例价估算，不使用账户折扣；最终按实际 usage.completion_tokens 核算。声音不影响单价。"
	if rate, ok := rates[o.PriceModel]; ok {
		unit := rate[0]
		if len(o.Videos) > 0 {
			unit = rate[1]
		}
		q.Unit = &unit
	} else {
		q.Warnings = append(q.Warnings, "未知模型无报价；须用 -price-model 显式关联已核实的实际计价模型")
	}
	seconds := float64(o.Duration)
	allDurations := true
	for _, m := range media {
		if m.Kind == "video" {
			if m.Duration <= 0 {
				allDurations = false
			}
			seconds += m.Duration
		}
	}
	if allDurations {
		tokens := int64(math.Ceil(seconds * float64(d[0]*d[1]*24) / 1024))
		q.FormulaTokens = &tokens
		if q.Unit != nil {
			cost := float64(tokens) * (*q.Unit) / 1e6
			q.MinYuan = &cost
			if len(o.Videos) == 0 {
				q.MaxYuan = &cost
			}
		}
	}
	if len(o.Videos) > 0 {
		q.MinimumSource = minimumSource
		q.Basis += " 公式：(参考视频总秒数+输出秒数)×宽×高×24/1024；还须取官方最低 token 与公式值的较大者。"
		// Official examples provide rounded ranges, NOT a precise minimum-token table.
		// Use their stated range only for the exact example specification.
		if o.Duration == 5 && o.Ratio == "16:9" && q.Unit != nil && valid {
			ranges := map[string]map[string][2]float64{
				"480p": {"mini": {1.27, 2.81}, "fast": {1.99, 4.42}, "standard": {2.53, 5.62}},
				"720p": {"mini": {2.72, 6.05}, "fast": {4.28, 9.50}, "standard": {5.44, 12.10}},
			}
			r := ranges[o.Resolution][o.PriceModel]
			q.MinYuan = &r[0]
			q.MaxYuan = &r[1]
			q.Basis += " 本规格采用官方示例的四舍五入范围（输入总时长 2–15 秒），不冒充本次素材的精确费用。"
		} else {
			q.Warnings = append(q.Warnings, "最低 token 表尚未完整核实：如有 min 值，它仅是公式下界，max=null 表示上界未知，不能当作完整报价")
		}
	} else {
		q.Basis += " 公式：输出秒数×宽×高×24/1024；整数 token 向上取整。"
	}
	if q.MinYuan != nil {
		v := *q.MinYuan * float64(o.Count)
		q.TotalMin = &v
	}
	if q.MaxYuan != nil {
		v := *q.MaxYuan * float64(o.Count)
		q.TotalMax = &v
	}
	// Approval binds the complete normalized request, material hashes and pricing basis.
	b, _ := json.Marshal(q)
	digest := sha256.Sum256(b)
	q.ConfirmationID = hex.EncodeToString(digest[:])
	return q
}
