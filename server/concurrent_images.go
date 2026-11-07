package main

import "reflect"

func singleImageStage(stage string) bool {
	return stage == "board_retry" || stage == "board_edit"
}

// Admission runs under store.mu in both quote and confirmation handlers.
// Unknown/failed work keeps the barrier; only healthy, distinct shots coexist.
func (s *Store) generationBusy(id, stage, shotID string, trial bool) bool {
	if (!singleImageStage(stage) && stage != "shot_video") || shotID == "" {
		return s.operationBusy(id, stage == "shot_video" && !trial)
	}
	tasks, err := s.tasks(id)
	if err != nil {
		return true
	}
	cfg, err := s.settings()
	if err != nil {
		return true
	}
	limit := generationConcurrency(cfg.ImageConcurrency)
	if stage == "shot_video" {
		limit = generationConcurrency(cfg.VideoConcurrency)
	}
	active := 0
	for _, t := range tasks {
		if !taskUnresolved(t.Status) {
			continue
		}
		// A healthy bulk image task may coexist with one or more standalone videos.
		if stage == "shot_video" && !trial && t.Stage == "boards" && (t.Status == "running" || t.Status == "queued") {
			var n int
			if err := s.db.QueryRow("SELECT count(*) FROM attempts WHERE task_id=? AND status IN ('unknown','rejected')", t.ID).Scan(&n); err != nil || n > 0 {
				return true
			}
			continue
		}
		sameKind := singleImageStage(stage) && singleImageStage(t.Stage) || stage == "shot_video" && t.Stage == "shot_video"
		if (t.Status != "running" && t.Status != "queued") || !sameKind || t.PlanTrial != trial || t.ShotID == shotID || t.ShotInputHash == "" {
			return true
		}
		var n int
		if err := s.db.QueryRow("SELECT count(*) FROM attempts WHERE task_id=? AND status IN ('unknown','rejected')", t.ID).Scan(&n); err != nil || n > 0 {
			return true
		}
		active++
	}
	return active >= limit
}

// Merge only the owned shot, checking the exact original input rather than a
// revision changed by a sibling's completion. Legacy tasks retain old semantics.
func mergeSingleShotTask(p Project, t Task) (Project, error) {
	if (!singleImageStage(t.Stage) && t.Stage != "shot_video") || t.ShotInputHash == "" {
		return p, errConflict
	}
	current := p
	if t.PlanTrial {
		var err error
		current, err = planTrialProject(p, t.ShotID)
		if err != nil {
			return p, errConflict
		}
	}
	if !reflect.DeepEqual(projectProductionBase(current), projectProductionBase(t.Input)) {
		return p, errConflict
	}
	merged, err := decodeBody[Project](jsonBytes(p))
	if err != nil {
		return p, err
	}
	shots := merged.Shots
	if t.PlanTrial {
		shots = merged.PlannedShots
	}
	for i, shot := range shots {
		if shot.ID != t.ShotID {
			continue
		}
		if digest(jsonBytes(shot)) != t.ShotInputHash {
			return p, errConflict
		}
		for _, generated := range t.Input.Shots {
			if generated.ID == t.ShotID {
				shots[i] = generated
				if !t.PlanTrial {
					merged.Output, merged.Mode = "", t.Input.Mode
				}
				return merged, nil
			}
		}
	}
	return p, errConflict
}
