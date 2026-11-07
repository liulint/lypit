package main

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
)

func shotHasImage(q Shot) bool {
	return q.Selected >= 0 && q.Selected < len(q.Candidates) && q.Candidates[q.Selected].File != ""
}

// Explicit replacement is a new, separately confirmed authorization. The old
// request/receipt remains immutable and can never be resumed after replacement.
func (s *Studio) boardReplacement(p Project, stage, shotID, oldID string) (Task, string, error) {
	var old Task
	if stage != "board_retry" || oldID == "" {
		return old, "", errors.New("仅缺失分镜图片可替代原任务重新报价")
	}
	old, err := s.store.task(oldID)
	if err != nil {
		return old, "", err
	}
	if old.ProjectID != p.ID || (old.Stage != "boards" && !(old.Stage == "board_retry" && old.ShotID == shotID)) || (old.Status != "failed" && old.Status != "interrupted" && old.Status != "cancelled") {
		return old, "", errors.New("原分镜任务已变化，请刷新后重新报价")
	}
	s.mu.Lock()
	_, running := s.running[oldID]
	s.mu.Unlock()
	if running {
		return old, "", errors.New("原任务仍在回收已提交结果，请等待停止")
	}
	if old.Stage == "boards" {
		if _, err = mergeProgressiveTask(p, old); err != nil {
			return old, "", err
		}
	} else if old.ShotInputHash != "" {
		if _, err = mergeSingleShotTask(p, old); err != nil {
			return old, "", err
		}
	} else if p.Revision != old.Input.Revision {
		return old, "", errConflict
	}
	found := false
	for _, shot := range p.Shots {
		if shot.ID == shotID && !shotHasImage(shot) {
			found = true
		}
	}
	if !found {
		return old, "", errors.New("替代报价只能补生成当前缺失的一张图")
	}
	tasks, err := s.store.tasks(p.ID)
	if err != nil {
		return old, "", err
	}
	for _, t := range tasks {
		if t.ID != oldID && taskUnresolved(t.Status) {
			return old, "", errors.New("请先处理其他未完成任务")
		}
	}
	rows, err := s.store.db.Query("SELECT id,status,response FROM attempts WHERE task_id=? ORDER BY id", oldID)
	if err != nil {
		return old, "", err
	}
	defer rows.Close()
	type receipt struct{ ID, Status, Hash string }
	receipts := []receipt{}
	for rows.Next() {
		var r receipt
		var body []byte
		if err = rows.Scan(&r.ID, &r.Status, &body); err != nil {
			return old, "", err
		}
		r.Hash = digest(body)
		receipts = append(receipts, r)
		targetKey := oldID + ":shot-" + shotID + "-0"
		if old.Stage == "board_retry" {
			targetKey = oldID + ":shot-" + shotID + "-retry"
		}
		if (r.Status == "unknown" || r.Status == "submitting") && r.ID != targetKey {
			return old, "", errors.New("其他原请求仍存在未知提交，不能替代当前任务")
		}
	}
	if err = rows.Err(); err != nil {
		return old, "", err
	}
	return old, digest(jsonBytes(struct {
		Task     Task
		Input    Project
		Receipts []receipt
	}{old, old.Input, receipts})), nil
}

func taskUnresolved(status string) bool {
	return status == "running" || status == "queued" || status == "failed" || status == "interrupted" || status == "cancelled"
}

// A recovery click is a read-only preflight before queuing: missing/empty CLI
// receipts must produce an immediate actionable error, never a new inference.
func (s *Studio) checkBoardRecovery(t Task) error {
	if t.Stage != "boards" && t.Stage != "board_retry" && t.Stage != "board_edit" {
		return nil
	}
	ep, err := resolveEndpoint(t.Config, "image")
	if err != nil {
		return err
	}
	if ep.Transport != "cli" {
		return nil
	}
	check := func(key, title string) error {
		var state string
		var body []byte
		err := s.store.db.QueryRow("SELECT status,response FROM attempts WHERE id=? AND task_id=?", t.ID+":"+key, t.ID).Scan(&state, &body)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("无法恢复「%s」：没有已提交请求的回执。恢复不会重新生图，请为缺失图片重新报价", title)
		}
		if err != nil {
			return err
		}
		if state == "received" {
			return nil
		}
		intent, e := decodeBody[cliImageIntent](body)
		if e != nil || intent.Root == "" || intent.Started.IsZero() {
			return fmt.Errorf("无法恢复「%s」：原提交记录不完整；不会重复调用模型", title)
		}
		file := filepath.Join(s.data, "cli", t.ID, key, "image-receipt.json")
		if _, e = collectCLIImage(file, intent, t.Input, key, false); e != nil {
			return fmt.Errorf("无法恢复「%s」：原图片回执缺失、为空或图片无效（%v）。未重新调用模型；请为缺失图片重新报价，原请求可能已计费", title, cliImageFailureReason(filepath.Dir(file), e))
		}
		return nil
	}
	for _, shot := range t.Input.Shots {
		if t.Stage == "board_retry" || t.Stage == "board_edit" {
			if shot.ID != t.ShotID {
				continue
			}
			suffix := "retry"
			if t.Stage == "board_edit" {
				suffix = "edit"
			}
			key := "shot-" + shot.ID + "-" + suffix
			if shotHasImage(shot) && shot.Candidates[shot.Selected].File == t.ProjectID+"/"+t.ID+"-"+key+".png" {
				return nil
			}
			return check(key, shot.Title)
		}
		for i := 0; i < boardImagesPerShot(t); i++ {
			if i < len(shot.Candidates) && shot.Candidates[i].File != "" {
				continue
			}
			if e := check(fmt.Sprintf("shot-%s-%d", shot.ID, i), shot.Title); e != nil {
				return e
			}
		}
	}
	return nil
}
