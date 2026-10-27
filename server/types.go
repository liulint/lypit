package main

import "encoding/json"

type Model struct {
	CLIOptions      *CLIOptions `json:"cli_options,omitempty"`
	ImageEdit       *bool       `json:"image_edit,omitempty"`
	OutputFormats   []string    `json:"output_formats,omitempty"`
	VideoReference  string      `json:"video_reference,omitempty"`
	ID              string      `json:"id"`
	Capabilities    []string    `json:"capabilities"`
	ReferenceImages bool        `json:"reference_images"`
	MaxReferences   int         `json:"max_references"`
	Structured      bool        `json:"structured"`
	Async           bool        `json:"async"`
	Audio           bool        `json:"audio"`
	Sizes           []string    `json:"sizes"`
	MinDuration     int         `json:"min_duration"`
	MaxDuration     int         `json:"max_duration"`
	Price           *float64    `json:"price"`
	Unit            string      `json:"unit"`
	PriceSource     string      `json:"price_source"`
	Verified        string      `json:"verified"`
	PriceModel      string      `json:"price_model"`
	Voice           string      `json:"voice"`
}
type CLIOptions struct {
	Reasoning   string `json:"reasoning,omitempty"`
	ServiceTier string `json:"service_tier,omitempty"`
}
type Provider struct {
	CredentialSource  string            `json:"credential_source,omitempty"`
	CredentialVersion string            `json:"credential_version,omitempty"`
	CLI               *CLIConfig        `json:"cli,omitempty"`
	ImageVerified     map[string]string `json:"image_verified,omitempty"`
	credential        string
	ID                string  `json:"id"`
	Name              string  `json:"name"`
	Adapter           string  `json:"adapter"`
	BaseURL           string  `json:"base_url"`
	KeyEnv            string  `json:"key_env"`
	Models            []Model `json:"models"`
}
type CLIConfig struct {
	ImageMode         string `json:"image_mode,omitempty"`
	SkillDigest       string `json:"skill_digest,omitempty"`
	Kind              string `json:"kind"`
	Path              string `json:"path"`
	ImageInstructions string `json:"image_instructions"`
	SkillPath         string `json:"skill_path"`
	TimeoutSeconds    int    `json:"timeout_seconds"`
}
type Binding struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}
type Settings struct {
	Providers []Provider         `json:"providers"`
	Bindings  map[string]Binding `json:"bindings"`
}
type Character struct {
	Views         []ReferenceView `json:"views"`
	FixedFeatures string          `json:"fixed_features"`
	Sources       []string        `json:"sources"`
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Description   string          `json:"description"`
	Image         string          `json:"image"`
}
type Shot struct {
	VideoGenerationID string         `json:"video_generation_id,omitempty"`
	BaseVideoPrompt   string         `json:"base_video_prompt"`
	SceneID           string         `json:"scene_id"`
	PropID            string         `json:"prop_id"`
	Purpose           string         `json:"purpose"`
	Camera            CameraPlan     `json:"camera"`
	Continuity        ContinuityPlan `json:"continuity"`
	Candidates        []Candidate    `json:"candidates"`
	Selected          string         `json:"selected"`
	Recommended       string         `json:"recommended"`
	Stale             bool           `json:"stale"`
	VideoReview       Review         `json:"video_review"`
	ID                string         `json:"id"`
	Title             string         `json:"title"`
	Scene             string         `json:"scene"`
	Action            string         `json:"action"`
	Characters        []string       `json:"characters"`
	References        []string       `json:"references"`
	Duration          int            `json:"duration"`
	Narration         string         `json:"narration"`
	ImagePrompt       string         `json:"image_prompt"`
	VideoPrompt       string         `json:"video_prompt"`
	Transition        string         `json:"transition"`
	Image             string         `json:"image"`
	Video             string         `json:"video"`
	Voice             string         `json:"voice"`
	Warning           string         `json:"warning"`
}
type Board struct {
	AssetHash      string          `json:"asset_hash,omitempty"`
	Schema         int             `json:"schema"`
	GenerationID   string          `json:"generation_id"`
	Visual         VisualPlan      `json:"visual"`
	Story          StoryPlan       `json:"story"`
	Scenes         []SceneAsset    `json:"scenes"`
	Props          []SceneAsset    `json:"props"`
	SourceMappings []SourceMapping `json:"source_mappings"`
	ReferenceHash  string          `json:"reference_hash"`
	Version        int             `json:"version"`
	Title          string          `json:"title"`
	Synopsis       string          `json:"synopsis"`
	Characters     []Character     `json:"characters"`
	Shots          []Shot          `json:"shots"`
	Approved       bool            `json:"approved"`
}
type Task struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	State    string `json:"state"`
	Message  string `json:"message"`
	Started  string `json:"started"`
	Finished string `json:"finished"`
}
type Attempt struct {
	Prompt     string          `json:"prompt"`
	References []ReferenceUse  `json:"references"`
	Duration   int             `json:"duration"`
	Intent     string          `json:"intent"`
	QuoteID    string          `json:"quote_id"`
	Role       string          `json:"role"`
	ID         string          `json:"id"`
	Kind       string          `json:"kind"`
	State      string          `json:"state"`
	File       string          `json:"file"`
	Provider   string          `json:"provider"`
	Model      string          `json:"model"`
	Usage      json.RawMessage `json:"usage,omitempty"`
	Fee        *float64        `json:"fee"`
	Detail     string          `json:"detail"`
}
type Project struct {
	AssetWorkflow     bool               `json:"asset_workflow,omitempty"`
	AssetSets         []AssetSet         `json:"asset_sets,omitempty"`
	ReferenceMappings []SourceMapping    `json:"reference_mappings"`
	Visual            VisualPlan         `json:"visual"`
	ID                string             `json:"id"`
	Idea              string             `json:"idea"`
	Created           string             `json:"created"`
	Revision          int                `json:"revision"`
	Status            string             `json:"status"`
	Boards            []Board            `json:"boards"`
	References        []string           `json:"references"`
	Music             string             `json:"music"`
	Tasks             []Task             `json:"tasks"`
	Attempts          map[string]Attempt `json:"attempts"`
	Output            string             `json:"output"`
	Error             string             `json:"error"`
	ActiveQuote       string             `json:"active_quote"`
}
type RequestSpec struct {
	Count       int      `json:"count"`
	Conditional bool     `json:"conditional"`
	Role        string   `json:"role"`
	Kind        string   `json:"kind"`
	Provider    string   `json:"provider"`
	Model       string   `json:"model"`
	Prompt      string   `json:"prompt"`
	References  []string `json:"references"`
	Duration    int      `json:"duration"`
	Parameters  any      `json:"parameters,omitempty"`
	Estimate    *float64 `json:"estimate"`
}
type Quote struct {
	RefHashes     map[string]string `json:"ref_hashes,omitempty"`
	AssetHash     string            `json:"asset_hash,omitempty"`
	Schema        int               `json:"schema"`
	Scope         map[string]int    `json:"scope"`
	SelectionHash string            `json:"selection_hash"`
	ID            string            `json:"id"`
	ProjectID     string            `json:"project_id"`
	Revision      int               `json:"revision"`
	Stage         string            `json:"stage"`
	Instruction   string            `json:"instruction"`
	ShotID        string            `json:"shot_id"`
	SettingsHash  string            `json:"settings_hash"`
	Settings      Settings          `json:"-"`
	Requests      []RequestSpec     `json:"requests"`
	Estimate      *float64          `json:"estimate"`
	Notice        string            `json:"notice"`
	Used          bool              `json:"used"`
	Created       string            `json:"created"`
}

// Snapshots contain credential references only; secret values never enter SQLite or exports.
type StoredQuote struct {
	Quote    Quote    `json:"quote"`
	Settings Settings `json:"settings"`
}
type State struct {
	Probes        map[string]ImageProbe  `json:"probes,omitempty"`
	FormatVersion int                    `json:"format_version"`
	Settings      Settings               `json:"settings"`
	Projects      map[string]*Project    `json:"projects"`
	Quotes        map[string]StoredQuote `json:"quotes"`
}

type VisualPlan struct {
	Style       string `json:"style"`
	Photography string `json:"photography"`
	Palette     string `json:"palette"`
	Candidates  int    `json:"candidates"`
	Repairs     int    `json:"repairs"`
}
type StoryPlan struct {
	Synopsis      string   `json:"synopsis"`
	Relationships string   `json:"relationships"`
	Emotion       string   `json:"emotion"`
	Beats         []string `json:"beats"`
}
type CameraPlan struct {
	Scale       string `json:"scale"`
	Angle       string `json:"angle"`
	Composition string `json:"composition"`
	Movement    string `json:"movement"`
	View        string `json:"view"`
}
type ContinuityPlan struct {
	Time      string `json:"time"`
	Weather   string `json:"weather"`
	Lighting  string `json:"lighting"`
	Positions string `json:"positions"`
	Gaze      string `json:"gaze"`
	Direction string `json:"direction"`
	Props     string `json:"props"`
	EndState  string `json:"end_state"`
	Handoff   string `json:"handoff"`
}
type Review struct {
	State          string   `json:"state"`
	Identity       float64  `json:"identity"`
	Style          float64  `json:"style"`
	Composition    float64  `json:"composition"`
	Continuity     float64  `json:"continuity"`
	Technical      float64  `json:"technical"`
	Score          float64  `json:"score"`
	IdentityError  bool     `json:"identity_error"`
	TechnicalError bool     `json:"technical_error"`
	Issues         []string `json:"issues"`
	Reason         string   `json:"reason"`
	Correction     string   `json:"correction"`
	Accepted       string   `json:"accepted"`
}
type ReferenceUse struct {
	AssetID string `json:"asset_id"`
	File    string `json:"file"`
	Hash    string `json:"hash"`
	Purpose string `json:"purpose"`
}
type ReferenceView struct {
	Prompt     string         `json:"prompt,omitempty"`
	References []ReferenceUse `json:"references,omitempty"`
	ID         string         `json:"id"`
	Kind       string         `json:"kind"`
	Image      string         `json:"image"`
	Hash       string         `json:"hash"`
	Review     Review         `json:"review"`
}
type SceneAsset struct {
	Prompt      string         `json:"prompt,omitempty"`
	References  []ReferenceUse `json:"references,omitempty"`
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Image       string         `json:"image"`
	Hash        string         `json:"hash"`
	Review      Review         `json:"review"`
	Sources     []string       `json:"sources"`
}
type SourceMapping struct {
	File   string `json:"file"`
	Kind   string `json:"kind"`
	Target string `json:"target"`
	Reason string `json:"reason"`
}
type Candidate struct {
	ID          string         `json:"id"`
	Intent      string         `json:"intent"`
	Round       int            `json:"round"`
	Prompt      string         `json:"prompt"`
	VideoPrompt string         `json:"video_prompt"`
	Image       string         `json:"image"`
	Hash        string         `json:"hash"`
	References  []ReferenceUse `json:"references"`
	Review      Review         `json:"review"`
}
