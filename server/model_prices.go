package main

import (
	"encoding/json"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Reference metadata; an estimate never turns an unknown settlement into a fixed total.
type ModelPriceRow struct {
	Label     string `json:"label"`
	Condition string `json:"condition,omitempty"`
	Amount    string `json:"amount"`
	Unit      string `json:"unit"`
	Currency  string `json:"currency,omitempty"`
}
type ModelPricing struct {
	Rows    []ModelPriceRow `json:"rows"`
	Source  string          `json:"source"`
	URL     string          `json:"url"`
	Checked string          `json:"checked"`
	Note    string          `json:"note"`
}

const arkPriceURL = "https://www.volcengine.com/docs/82379/1544106"

func officialModelPricing(v Vendor, id string) *ModelPricing {
	u, err := url.Parse(v.Base)
	if err != nil || v.Type != "api" || v.Kind != "ark" || u.Scheme != "https" || u.Host != "ark.cn-beijing.volces.com" {
		return nil // Proxy prices and regions must not inherit the mainland list price.
	}
	// Explicit snapshots and documented aliases only. Future versions need review.
	models := map[string]string{
		"doubao-seedance-2-5-260628": "2.5", "doubao-seedance-2-5": "2.5", "doubao-seedance-2.5": "2.5",
		"doubao-seedance-2-0-260128": "2.0", "doubao-seedance-2-0": "2.0", "doubao-seedance-2.0": "2.0",
		"doubao-seedance-2-0-fast-260128": "fast", "doubao-seedance-2-0-fast": "fast", "doubao-seedance-2.0-fast": "fast",
		"doubao-seedance-2-0-mini-260615": "mini", "doubao-seedance-2-0-mini": "mini", "doubao-seedance-2.0-mini": "mini",
		"doubao-seedance-1-0-pro-250528": "pro", "doubao-seedance-1-0-pro": "pro", "doubao-seedance-1.0-pro": "pro",
		"doubao-seedance-1-0-pro-fast-251015": "pro-fast", "doubao-seedance-1-0-pro-fast": "pro-fast", "doubao-seedance-1.0-pro-fast": "pro-fast",
	}
	kind := models[id]
	if kind == "" {
		return nil
	}
	p := &ModelPricing{Rows: []ModelPriceRow{}, Source: "火山方舟官方参考价 · 在线推理", URL: arkPriceURL, Checked: "2026-11-06", Note: "视频不单列输入/输出 Token 单价；按视频 Token 用量结算。输入视频时长计入用量，且可能有最低用量。图片首帧属于不含视频输入档。实际费用以厂商账单为准，参考价不作为总价承诺。"}
	add := func(condition, amount string) {
		p.Rows = append(p.Rows, ModelPriceRow{Label: "视频生成", Condition: condition, Amount: amount, Unit: "百万视频 Token", Currency: "CNY"})
	}
	pair := func(res, without, with string) {
		add(res+" · 不含视频输入", without)
		add(res+" · 含视频输入", with)
	}
	switch kind {
	case "2.5":
		pair("480p / 720p", "70.00", "42.00")
		pair("1080p", "77.00", "46.00")
	case "2.0":
		pair("480p / 720p", "46.00", "28.00")
		pair("1080p", "51.00", "31.00")
		pair("4K", "26.00", "16.00")
	case "fast":
		pair("480p / 720p", "37.00", "22.00")
	case "mini":
		pair("480p / 720p", "23.00", "14.00")
	case "pro":
		add("在线推理", "15.00")
		p.Note = "视频按 Token 用量计费，不单列文本式输入/输出单价。此处为在线推理参考价，实际费用以厂商账单为准。"
	case "pro-fast":
		add("在线推理", "4.20")
		p.Note = "视频按 Token 用量计费，不单列文本式输入/输出单价。此处为在线推理参考价，实际费用以厂商账单为准。"
	}
	return p
}

// Bailian's documented list-models schema supplies price, unit and tier, but
// does not specify currency. Preserve that uncertainty instead of guessing CNY.
func catalogModelPricing(raw json.RawMessage, updated time.Time) *ModelPricing {
	var tiers []struct {
		Range  string `json:"range_name"`
		Prices []struct {
			Type  string          `json:"type"`
			Name  string          `json:"price_name"`
			Price json.RawMessage `json:"price"`
			Unit  string          `json:"price_unit"`
		} `json:"prices"`
	}
	if json.Unmarshal(raw, &tiers) != nil || len(tiers) > 50 {
		return nil
	}
	p := &ModelPricing{Rows: []ModelPriceRow{}, Source: "百炼模型目录返回价", URL: "https://help.aliyun.com/en/model-studio/list-models", Checked: updated.UTC().Format("2006-01-02"), Note: "按厂商目录原始单位与分档展示；目录未标明币种，请到厂商控制台确认。实际费用受地域、部署方式与合同折扣影响，不作为总价承诺。"}
	for _, tier := range tiers {
		for _, item := range tier.Prices {
			var amount string
			if json.Unmarshal(item.Price, &amount) != nil {
				amount = string(item.Price)
			}
			n, err := strconv.ParseFloat(amount, 64)
			if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || len(amount) > 40 || item.Unit == "" || len(item.Unit) > 100 || len(tier.Range) > 150 {
				continue
			}
			label := map[string]string{"input_token": "输入", "output_token": "输出", "image_number": "输出图片"}[item.Type]
			if label == "" {
				label = item.Name
			}
			if strings.TrimSpace(label) == "" || len(label) > 100 {
				continue
			}
			condition := tier.Range
			if condition == "Default" {
				condition = ""
			}
			p.Rows = append(p.Rows, ModelPriceRow{Label: label, Condition: condition, Amount: amount, Unit: item.Unit})
			if len(p.Rows) >= 100 {
				return p
			}
		}
	}
	if len(p.Rows) == 0 {
		return nil
	}
	return p
}
