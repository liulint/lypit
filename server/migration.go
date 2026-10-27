package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const stateVersion = 4

func migrateState(st *State) error {
	if st.FormatVersion > stateVersion {
		return errors.New("数据格式较新，当前程序拒绝覆盖")
	}
	if st.FormatVersion == stateVersion {
		return nil
	}
	if st.FormatVersion >= 2 {
		migrateProviders(st)
		st.FormatVersion = stateVersion
		return nil
	}
	for pi := range st.Settings.Providers {
		p := &st.Settings.Providers[pi]
		if p.Adapter == "mock" {
			for i := range p.Models {
				if p.Models[i].ReferenceImages {
					p.Models[i].MaxReferences = max(9, p.Models[i].MaxReferences)
				}
			}
		}
	}
	for _, p := range st.Projects {
		p.Visual = visualDefaults(p.Visual, p.Idea)
		if p.Attempts == nil {
			p.Attempts = map[string]Attempt{}
		}
		for bi := range p.Boards {
			b := &p.Boards[bi]
			if b.Schema == 0 {
				b.Schema = 1
			}
			if b.Visual.Style == "" {
				b.Visual = p.Visual
			}
			if b.GenerationID == "" {
				b.GenerationID = fmt.Sprintf("legacy-%s-%d", p.ID, b.Version)
			}
			for i := range b.Characters {
				c := &b.Characters[i]
				if len(c.Views) == 0 && c.Image != "" {
					c.Views = []ReferenceView{{ID: c.ID + "-front", Kind: "front", Image: c.Image, Review: Review{State: "legacy", Reason: "迁移的历史角色图，未新增模型调用"}}}
				}
			}
			for i := range b.Shots {
				q := &b.Shots[i]
				if len(q.Candidates) == 0 && q.Image != "" {
					id := "legacy-" + q.ID
					q.Candidates = []Candidate{{ID: id, Intent: b.GenerationID + ":" + id, Round: 1, Prompt: q.ImagePrompt, VideoPrompt: q.VideoPrompt, Image: q.Image, Review: Review{State: "legacy", Reason: "迁移的历史分镜图，未新增模型调用"}}}
					q.Selected = id
					q.Recommended = id
				}
			}
		}
	}
	migrateProviders(st)
	st.FormatVersion = stateVersion
	return nil
}
func backupBeforeMigration(s *Store, dir string) error {
	path := filepath.Join(dir, fmt.Sprintf("studio-before-v%d-%s", stateVersion, uid())+".db")
	if _, e := s.db.Exec("VACUUM INTO ?", path); e != nil {
		return e
	}
	if e := os.Chmod(path, 0600); e != nil {
		return e
	}
	return nil
}

func migrateProviders(st *State) {
	for i := range st.Settings.Providers {
		p := &st.Settings.Providers[i]
		if p.Adapter != "mock" && p.Adapter != "cli" && p.CredentialSource == "" {
			p.CredentialSource = "env"
		}
	}
	if st.Probes == nil {
		st.Probes = map[string]ImageProbe{}
	}
}
