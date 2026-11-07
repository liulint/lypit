package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

const stagesVersion = 1

type Image struct {
	ReferencePlan *ReferencePlan `json:"reference_plan,omitempty"`
	Operation     string         `json:"operation,omitempty"`
	SourceFile    string         `json:"source_file,omitempty"`
	SourceHash    string         `json:"source_hash,omitempty"`
	File          string         `json:"file"`
	Hash          string         `json:"hash"`
	Approved      bool           `json:"approved"`
}
type Asset struct {
	ID          string  `json:"id"`
	Kind        string  `json:"kind"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Master      *Image  `json:"master,omitempty"`
	Sheet       *Image  `json:"sheet,omitempty"`
	Views       []Image `json:"views"`
}
type Shot struct {
	ContentOverride   *ShotContentOverride `json:"content_override,omitempty"`
	ReviewStatus      string               `json:"review_status,omitempty"`
	FirstFrame        string               `json:"first_frame,omitempty"`
	SourceText        string               `json:"source_text,omitempty"`
	VideoEndImageHash string               `json:"video_end_image_hash,omitempty"`
	VideoMode         string               `json:"video_mode,omitempty"`
	VideoEndShotID    string               `json:"video_end_shot_id,omitempty"`
	VideoSourceFile   string               `json:"video_source_file,omitempty"`
	VideoTaskID       string               `json:"video_task_id,omitempty"`
	ID                string               `json:"id"`
	Title             string               `json:"title"`
	Description       string               `json:"description"`
	Narration         string               `json:"narration"` // Legacy name; per-shot dialogue when audio_mode=dialogue.
	Duration          int                  `json:"duration"`
	References        []string             `json:"references"`
	Candidates        []Image              `json:"candidates"`
	Selected          int                  `json:"selected"`
	Approved          bool                 `json:"approved"`
	Video             string               `json:"video,omitempty"`
}
type Project struct {
	PlanVersion   int     `json:"plan_version,omitempty"`
	PlanApproved  bool    `json:"plan_approved,omitempty"`
	PlannedShots  []Shot  `json:"planned_shots,omitempty"`
	PreviousShots []Shot  `json:"previous_shots,omitempty"`
	AudioMode     string  `json:"audio_mode"`
	ID            string  `json:"id"`
	Title         string  `json:"title"`
	Idea          string  `json:"idea"`
	Script        string  `json:"script"`
	Style         string  `json:"style"`
	AspectRatio   string  `json:"aspect_ratio"`
	Duration      int     `json:"duration"`
	Revision      int     `json:"revision"`
	Created       string  `json:"created"`
	Updated       string  `json:"updated"`
	Archived      bool    `json:"archived"`
	Analyzed      bool    `json:"analyzed"`
	AssetsLocked  bool    `json:"assets_locked"`
	BoardsLocked  bool    `json:"boards_locked"`
	Assets        []Asset `json:"assets"`
	Shots         []Shot  `json:"shots"`
	Output        string  `json:"output,omitempty"`
	Voice         string  `json:"voice,omitempty"`
	Mode          string  `json:"mode"`
}
type Endpoint struct {
	KeyMode      string `json:"key_mode,omitempty"`
	CredentialID string `json:"credential_id,omitempty"`
	VendorID     string `json:"vendor_id,omitempty"`
	Transport    string `json:"transport,omitempty"`
	CLI          string `json:"cli,omitempty"`
	Executable   string `json:"executable,omitempty"`
	Base         string `json:"base"`
	Model        string `json:"model"`
	KeyEnv       string `json:"key_env"`
}
type Settings struct {
	PromptTemplates  map[string]string `json:"prompt_templates,omitempty"`
	ImageConcurrency int               `json:"image_concurrency,omitempty"`
	VideoConcurrency int               `json:"video_concurrency,omitempty"`
	RegistryVersion  int               `json:"registry_version"`
	Vendors          []Vendor          `json:"vendors"`
	Version          int               `json:"version"`
	Mode             string            `json:"mode"`
	Text             Endpoint          `json:"text"`
	Image            Endpoint          `json:"image"`
	Video            Endpoint          `json:"video"`
	Voice            Endpoint          `json:"voice"`
}
type Vendor struct {
	Kind         string `json:"kind,omitempty"`
	Region       string `json:"region,omitempty"`
	Workspace    string `json:"workspace,omitempty"`
	KeyMode      string `json:"key_mode,omitempty"`
	CredentialID string `json:"credential_id,omitempty"`
	ID           string `json:"id"`
	Name         string `json:"name"`
	Type         string `json:"type"`
	Protocol     string `json:"protocol,omitempty"`
	Base         string `json:"base,omitempty"`
	KeyEnv       string `json:"key_env,omitempty"`
	CLI          string `json:"cli,omitempty"`
	Executable   string `json:"executable,omitempty"`
}
type QuoteItem struct {
	Label string `json:"label"`
	Count int    `json:"count"`
}
type Quote struct {
	PromptTemplatesVersion     int                      `json:"prompt_templates_version,omitempty"`
	StoryDirectionVersion      int                      `json:"story_direction_version,omitempty"`
	TargetDuration             int                      `json:"target_duration,omitempty"`
	VideoDurationPolicyVersion int                      `json:"video_duration_policy_version,omitempty"`
	PlanTrial                  bool                     `json:"plan_trial,omitempty"`
	Estimate                   *QuoteEstimate           `json:"estimate,omitempty"`
	EstimateReason             string                   `json:"estimate_reason,omitempty"`
	ImageEdit                  *ImageEditSpec           `json:"image_edit,omitempty"`
	ReferenceIDs               []string                 `json:"reference_ids,omitempty"`
	ShotReferencePlans         map[string]ReferencePlan `json:"shot_reference_plans,omitempty"`
	ImageReferencePlan         *ReferencePlan           `json:"image_reference_plan,omitempty"`
	ReferencePolicyVersion     int                      `json:"reference_policy_version,omitempty"`
	ReplacesTaskID             string                   `json:"replaces_task_id,omitempty"`
	ReplacementFingerprint     string                   `json:"replacement_fingerprint,omitempty"`
	VideoInputs                []VideoInput             `json:"video_inputs,omitempty"`
	BoardImagesPerShot         int                      `json:"board_images_per_shot,omitempty"`
	ShotID                     string                   `json:"shot_id,omitempty"`
	Feedback                   string                   `json:"feedback,omitempty"`
	ID                         string                   `json:"id"`
	ProjectID                  string                   `json:"project_id"`
	Revision                   int                      `json:"revision"`
	SettingsVersion            int                      `json:"settings_version"`
	Stage                      string                   `json:"stage"`
	Items                      []QuoteItem              `json:"items"`
	Mode                       string                   `json:"mode"`
	Unknown                    bool                     `json:"unknown"`
	Expires                    string                   `json:"expires"`
	TaskID                     string                   `json:"task_id,omitempty"`
	AspectRatio                string                   `json:"aspect_ratio"`
	Duration                   int                      `json:"duration"`
}
type Task struct {
	PromptTemplatesVersion int                `json:"prompt_templates_version,omitempty"`
	StoryDirectionVersion  int                `json:"story_direction_version,omitempty"`
	Rewrite                *ShotRewriteResult `json:"rewrite,omitempty"`
	TargetDuration         int                `json:"target_duration,omitempty"`
	// Parallel workers report usage to the sole task coordinator before download.
	usageRecorder              func(VideoUsage) error
	VideoDurationPolicyVersion int                      `json:"video_duration_policy_version,omitempty"`
	ShotInputHash              string                   `json:"shot_input_hash,omitempty"`
	PlanTrial                  bool                     `json:"plan_trial,omitempty"`
	VideoUsage                 []VideoUsage             `json:"video_usage,omitempty"`
	ImageEdit                  *ImageEditSpec           `json:"image_edit,omitempty"`
	ShotReferencePlans         map[string]ReferencePlan `json:"shot_reference_plans,omitempty"`
	ImageReferencePlan         *ReferencePlan           `json:"image_reference_plan,omitempty"`
	ReferencePolicyVersion     int                      `json:"reference_policy_version,omitempty"`
	VideoInputs                []VideoInput             `json:"video_inputs,omitempty"`
	BoardImagesPerShot         int                      `json:"board_images_per_shot,omitempty"`
	ShotID                     string                   `json:"shot_id,omitempty"`
	Feedback                   string                   `json:"feedback,omitempty"`
	ID                         string                   `json:"id"`
	ProjectID                  string                   `json:"project_id"`
	QuoteID                    string                   `json:"quote_id"`
	Stage                      string                   `json:"stage"`
	Status                     string                   `json:"status"`
	Progress                   int                      `json:"progress"`
	Message                    string                   `json:"message"`
	Created                    string                   `json:"created"`
	Updated                    string                   `json:"updated"`
	Mode                       string                   `json:"mode"`
	Input                      Project                  `json:"-"`
	Config                     Settings                 `json:"-"`
}

func uid() string {
	b := make([]byte, 12)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func now() string            { return time.Now().UTC().Format(time.RFC3339Nano) }
func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func jsonBytes(v any) []byte {
	b, e := json.Marshal(v)
	if e != nil {
		panic(e)
	}
	return b
}
func defaultSettings() Settings {
	return Settings{ImageConcurrency: 2, VideoConcurrency: 2, Version: 1, Mode: "mock", Text: Endpoint{Base: "https://api.openai.com/v1", KeyEnv: "TEXT_API_KEY"}, Image: Endpoint{Base: "https://api.openai.com/v1", KeyEnv: "IMAGE_API_KEY"}, Video: Endpoint{Base: "https://ark.cn-beijing.volces.com/api/v3", KeyEnv: "ARK_API_KEY"}, Voice: Endpoint{Base: "https://api.openai.com/v1", Model: "tts-1", KeyEnv: "VOICE_API_KEY"}}
}
