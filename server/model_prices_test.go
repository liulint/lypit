package main

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestOfficialModelPrices(t *testing.T) {
	v := Vendor{Type: "api", Kind: "ark", Base: "https://ark.cn-beijing.volces.com/api/v3"}
	for _, tc := range []struct {
		id, amount string
		rows       int
	}{
		{"doubao-seedance-2-0-mini-260615", "23.00", 2},
		{"doubao-seedance-2-0-fast-260128", "37.00", 2},
		{"doubao-seedance-2-0-260128", "46.00", 6},
		{"doubao-seedance-2-5-260628", "70.00", 4},
		{"doubao-seedance-1-0-pro-250528", "15.00", 1},
		{"doubao-seedance-1-0-pro-fast-251015", "4.20", 1},
	} {
		p := describeModel(v, tc.id, "", nil).Pricing
		if p == nil || len(p.Rows) != tc.rows || p.Rows[0].Amount != tc.amount || p.Rows[0].Currency != "CNY" || p.Checked != "2026-11-06" {
			t.Fatalf("wrong price for %s: %+v", tc.id, p)
		}
		for _, row := range p.Rows {
			if row.Label == "输入" || row.Label == "输出" {
				t.Fatal("video tiers are not input/output token prices")
			}
		}
	}
	if officialModelPricing(v, "doubao-seedance-2-0-mini-299999") != nil {
		t.Fatal("unknown future snapshot inherited a price")
	}
	for _, base := range []string{"https://ark.cn-beijing.volces.com.evil.test/api/v3", "https://proxy.test/api/v3", "http://ark.cn-beijing.volces.com/api/v3"} {
		v.Base = base
		if officialModelPricing(v, "doubao-seedance-2-0-mini") != nil {
			t.Fatal("proxy inherited official price")
		}
	}
	v.Base, v.Kind = "https://ark.cn-beijing.volces.com/api/v3", "custom"
	if officialModelPricing(v, "doubao-seedance-2-0-mini") != nil {
		t.Fatal("custom provider inherited price")
	}
}

func TestCatalogPriceTiersAndUnknownCurrency(t *testing.T) {
	raw := json.RawMessage(`[{"range_name":"32k<Input<=128k","prices":[{"type":"input_token","price":"0","price_unit":"per million tokens"},{"type":"output_token","price":8,"price_unit":"per million tokens"},{"type":"output_token","price":"NaN","price_unit":"tokens"},{"type":"output_token","price":-1,"price_unit":"tokens"},{"type":"output_token","price":null,"price_unit":"tokens"}]}]`)
	p := catalogModelPricing(raw, time.Date(2026, 11, 6, 1, 0, 0, 0, time.UTC))
	if p == nil || len(p.Rows) != 2 || p.Rows[0].Amount != "0" || p.Rows[0].Currency != "" || p.Rows[1].Label != "输出" || p.Rows[1].Condition != "32k<Input<=128k" {
		t.Fatalf("lost tier/zero/currency semantics: %+v", p)
	}
	for _, raw := range []string{`null`, `[]`, `{}`, `[{"prices":[{"type":"input_token","price":"free","price_unit":"tokens"}]}]`} {
		if catalogModelPricing(json.RawMessage(raw), time.Now()) != nil {
			t.Fatalf("invalid price became zero: %s", raw)
		}
	}
}

func TestCatalogFetchIncludesPrices(t *testing.T) {
	v := Vendor{Type: "api", Kind: "bailian", Workspace: "test", Region: "cn-beijing", Base: "https://dashscope.aliyuncs.com/compatible-mode/v1"}
	cat, err := fetchModelCatalog(context.Background(), v, "key", catalogRoundTrip(func(r *http.Request) (*http.Response, error) {
		return catalogResponse(200, `{"output":{"total":1,"models":[{"model":"qwen-test","capabilities":["TG"],"prices":[{"range_name":"Default","prices":[{"type":"input_token","price":"2","price_unit":"per million tokens"},{"type":"output_token","price":"8","price_unit":"per million tokens"}]}]}]}}`), nil
	}))
	if err != nil || len(cat.Models) != 1 || cat.Models[0].Pricing == nil || len(cat.Models[0].Pricing.Rows) != 2 {
		t.Fatalf("missing response prices: %+v %v", cat, err)
	}
}
