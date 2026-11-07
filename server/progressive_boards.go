package main

import (
	"database/sql"
	"reflect"
)

// Only a healthy board-generation task may coexist with review or one video task.
// Failed/interrupted/unknown submissions retain the project's admission barrier.
func (s *Store) operationBusy(id string, review bool) bool {
	if !review {
		return s.busy(id)
	}
	tasks, err := s.tasks(id)
	if err != nil {
		return true
	}
	for _, t := range tasks {
		switch t.Status {
		case "running", "queued", "failed", "interrupted", "cancelled":
			if t.Stage != "boards" || (t.Status != "running" && t.Status != "queued") {
				return true
			}
			var n int
			if err := s.db.QueryRow("SELECT count(*) FROM attempts WHERE task_id=? AND status IN ('unknown','rejected')", t.ID).Scan(&n); err != nil || n > 0 {
				return true
			}
		}
	}
	return false
}

func projectProductionBase(p Project) Project {
	p.Shots = nil
	p.Revision = 0
	p.Updated, p.Output, p.Voice, p.Mode = "", "", "", ""
	return p
}

// Background work only owns its result fields. Never replace the project with
// an older snapshot after the user has confirmed dialogue or made a video.
func mergeProgressiveTask(p Project, t Task) (Project, error) {
	if !reflect.DeepEqual(projectProductionBase(p), projectProductionBase(t.Input)) {
		return p, errConflict
	}
	if len(p.Shots) == 0 && t.Stage == "boards" && p.Revision == t.Input.Revision {
		p.Shots = t.Input.Shots
		p.Mode = t.Input.Mode
		return p, nil
	}
	if len(p.Shots) != len(t.Input.Shots) {
		return p, errConflict
	}
	// Deep copy: merging must not mutate either task's immutable snapshot.
	merged, err := decodeBody[Project](jsonBytes(p))
	if err != nil {
		return p, err
	}
	found := false
	for i, source := range t.Input.Shots {
		current := &merged.Shots[i]
		if current.ID != source.ID || current.Title != source.Title || current.Description != source.Description || current.FirstFrame != source.FirstFrame || current.SourceText != source.SourceText || current.Duration != source.Duration || !reflect.DeepEqual(current.References, source.References) {
			return p, errConflict
		}
		if t.Stage == "boards" {
			if len(current.Candidates) > len(source.Candidates) {
				return p, errConflict
			}
			for len(current.Candidates) < len(source.Candidates) {
				current.Candidates = append(current.Candidates, Image{})
			}
			for j, im := range source.Candidates {
				if im.File == "" {
					continue
				}
				if current.Candidates[j].File != "" && !reflect.DeepEqual(current.Candidates[j], im) {
					return p, errConflict
				}
				current.Candidates[j] = im
			}
		} else if source.ID == t.ShotID {
			if !current.Approved || current.Selected != source.Selected || current.Narration != source.Narration || current.Selected < 0 || current.Selected >= len(current.Candidates) || source.Selected >= len(source.Candidates) || !reflect.DeepEqual(current.Candidates[current.Selected], source.Candidates[source.Selected]) {
				return p, errConflict
			}
			current.Video, current.VideoMode, current.VideoTaskID = source.Video, source.VideoMode, source.VideoTaskID
			current.VideoSourceFile, current.VideoEndShotID, current.VideoEndImageHash = source.VideoSourceFile, source.VideoEndShotID, source.VideoEndImageHash
			merged.Output = ""
			merged.Mode = t.Input.Mode
			found = true
		}
	}
	if t.Stage == "shot_video" && !found {
		return p, errConflict
	}
	return merged, nil
}

func (s *Studio) publishBoards(t Task) error {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	p, err := s.store.project(t.ProjectID)
	if err != nil {
		return err
	}
	merged, err := mergeProgressiveTask(p, t)
	if err != nil {
		return err
	}
	if reflect.DeepEqual(p, merged) {
		return nil
	}
	merged.Revision++
	merged.Updated = now()
	return s.store.transaction(func(tx *sql.Tx) error { return s.store.saveProject(tx, merged) })
}
