package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
)

func setProviderAuth(req *http.Request, p Provider) {
	key := p.credential
	if key == "" && p.CredentialSource != "file" {
		key = os.Getenv(p.KeyEnv)
	}
	switch p.Adapter {
	case "anthropic":
		req.Header.Set("x-api-key", key)
		req.Header.Set("anthropic-version", "2023-06-01")
	case "gemini":
		req.Header.Set("x-goog-api-key", key)
	default:
		req.Header.Set("Authorization", "Bearer "+key)
	}
}
func (s *Studio) nativeText(ctx context.Context, p Provider, m Model, prompt string, refs []string, responsePath string, recovering bool) ([]byte, error) {
	prompt = "只输出严格 JSON 对象，不要 Markdown。\n" + prompt
	var payload any
	var path string
	if p.Adapter == "anthropic" {
		content := []any{map[string]any{"type": "text", "text": prompt}}
		for _, ref := range refs {
			data, err := os.ReadFile(s.abs(ref))
			if err != nil {
				return nil, err
			}
			content = append(content, map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": http.DetectContentType(data), "data": base64.StdEncoding.EncodeToString(data)}})
		}
		payload = map[string]any{"model": m.ID, "max_tokens": 16000, "messages": []any{map[string]any{"role": "user", "content": content}}}
		path = "/messages"
	} else {
		parts := []any{map[string]any{"text": prompt}}
		for _, ref := range refs {
			data, err := os.ReadFile(s.abs(ref))
			if err != nil {
				return nil, err
			}
			parts = append(parts, map[string]any{"inline_data": map[string]any{"mime_type": http.DetectContentType(data), "data": base64.StdEncoding.EncodeToString(data)}})
		}
		payload = map[string]any{"contents": []any{map[string]any{"role": "user", "parts": parts}}, "generationConfig": map[string]any{"responseMimeType": "application/json"}}
		path = "/models/" + url.PathEscape(strings.TrimPrefix(m.ID, "models/")) + ":generateContent"
	}
	raw, err := os.ReadFile(responsePath + ".native")
	if os.IsNotExist(err) && !recovering {
		raw, err = postJSON(ctx, p, path, payload)
		if err == nil {
			err = atomicWrite(responsePath+".native", raw)
		}
	}
	if err != nil {
		return nil, err
	}
	text := ""
	var usage json.RawMessage
	if p.Adapter == "anthropic" {
		var r struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			Usage json.RawMessage `json:"usage"`
		}
		if json.Unmarshal(raw, &r) != nil {
			return nil, errors.New("Anthropic 响应无效")
		}
		for _, c := range r.Content {
			if c.Type == "text" {
				text += c.Text
			}
		}
		usage = r.Usage
	} else {
		var r struct {
			Candidates []struct {
				Content struct {
					Parts []struct {
						Text    string `json:"text"`
						Thought bool   `json:"thought"`
					} `json:"parts"`
				} `json:"content"`
			} `json:"candidates"`
			Usage json.RawMessage `json:"usageMetadata"`
		}
		if json.Unmarshal(raw, &r) != nil || len(r.Candidates) == 0 {
			return nil, errors.New("Gemini 响应无有效候选")
		}
		for _, part := range r.Candidates[0].Content.Parts {
			if !part.Thought {
				text += part.Text
			}
		}
		usage = r.Usage
	}
	// Retain the native response alongside the normalized envelope for recovery and usage audit.
	return json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": text}}}, "usage": usage, "native_response": json.RawMessage(raw)})
}
