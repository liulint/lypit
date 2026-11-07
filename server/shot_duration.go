package main

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

type shotDurationPolicy struct {
	Min             int    `json:"min"`
	Max             int    `json:"max"`
	Model           string `json:"model"`
	Known           bool   `json:"known"`
	SettingsVersion int    `json:"settings_version"`
}

// Editorial shots remain capped at 12 seconds. Native Seedance 2.x has a
// four-second generation floor; unknown/custom model IDs are not guessed.
func videoDurationPolicy(cfg Settings) shotDurationPolicy {
	v := shotDurationPolicy{Min: 1, Max: 12, SettingsVersion: cfg.Version}
	ep, err := resolveEndpoint(cfg, "video")
	if err != nil {
		return v
	}
	v.Model = ep.Model
	if cfg.Mode == "mock" {
		v.Known = true
		return v
	}
	model := strings.ToLower(ep.Model)
	if strings.HasPrefix(model, "doubao-seedance-2-0") || strings.HasPrefix(model, "doubao-seedance-2.0") || strings.HasPrefix(model, "doubao-seedance-2-5") || strings.HasPrefix(model, "doubao-seedance-2.5") {
		v.Min = 4
		v.Known = true
	}
	return v
}
func validateVideoDuration(cfg Settings, q Shot) error {
	v := videoDurationPolicy(cfg)
	if q.Duration < v.Min || q.Duration > v.Max {
		return fmt.Errorf("镜头「%s」为%d秒；当前制作在视频模型%s下允许的单镜时长为%d～%d秒，请先调整时长并重新报价", q.Title, q.Duration, v.Model, v.Min, v.Max)
	}
	return nil
}
func validatePlanVideoDurations(cfg Settings, shots []Shot) error {
	for _, q := range shots {
		if err := validateVideoDuration(cfg, q); err != nil {
			return err
		}
	}
	return nil
}
func validatePlanningDurations(p Project, cfg Settings) error {
	beats, err := scriptBeats(p)
	if err != nil {
		return err
	}
	v := videoDurationPolicy(cfg)
	for _, b := range beats {
		n := b.End - b.Start
		if n < v.Min || (n+v.Max-1)/v.Max > n/v.Min {
			return fmt.Errorf("剧本%d—%d秒段落无法拆成%d～%d秒的有效镜头，请先调整剧本时间段；不会自动合并场景或增加生成时长", b.Start, b.End, v.Min, v.Max)
		}
	}
	return nil
}

// Only explicit rejections, with no accepted or unknown receipt, can be ended
// together with a timing edit. Never reinterpret an immutable task snapshot.
func (s *Store) durationRejectedTask(t Task, shotID string, trial bool) bool {
	if t.Stage != "shot_video" || t.ShotID != shotID || t.PlanTrial != trial || t.Status != "failed" {
		return false
	}
	var total, unsafe int
	err := s.db.QueryRow("SELECT count(*),coalesce(sum(CASE WHEN status!='rejected' OR remote_id!='' THEN 1 ELSE 0 END),0) FROM attempts WHERE task_id=?", t.ID).Scan(&total, &unsafe)
	return err == nil && total > 0 && unsafe == 0
}
func (s *Studio) shotDurationInfo(c *gin.Context) {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	p, err := s.store.project(c.Param("id"))
	if err != nil {
		result(c, nil, err)
		return
	}
	cfg, err := s.store.settings()
	if err != nil {
		result(c, nil, err)
		return
	}
	tasks, err := s.store.tasks(p.ID)
	if err != nil {
		result(c, nil, err)
		return
	}
	trial := c.Query("plan_trial") == "true"
	rejected := []string{}
	blocked := false
	for _, t := range tasks {
		if taskUnresolved(t.Status) {
			if s.store.durationRejectedTask(t, c.Param("shot"), trial) {
				rejected = append(rejected, t.ID)
			} else {
				blocked = true
			}
		}
	}
	result(c, gin.H{"policy": videoDurationPolicy(cfg), "rejected_tasks": rejected, "blocked": blocked}, nil)
}
func retimeShot(p Project, shotID string, duration int, trial bool, cfg Settings) (Project, error) {
	next, err := decodeBody[Project](jsonBytes(p))
	if err != nil {
		return p, err
	}
	if p.Archived {
		return p, errors.New("已归档作品只读")
	}
	if !trial && (p.PlanVersion != 2 || len(p.PlannedShots) > 0) {
		return p, errors.New("请先采用当前分镜规划，再调整正式镜头时长")
	}
	shots := next.Shots
	if trial {
		shots = next.PlannedShots
	}
	index := -1
	start := 0
	for i, q := range shots {
		if q.ID == shotID {
			index = i
			break
		}
		start += q.Duration
	}
	if index < 0 {
		return p, errors.New("镜头不存在")
	}
	old := shots[index]
	candidate := old
	candidate.Duration = duration
	if err := validateVideoDuration(cfg, candidate); err != nil {
		return p, err
	}
	delta := duration - old.Duration
	if delta == 0 {
		return p, nil
	}
	if err := validateSpec(projectAspect(p), projectDuration(p)+delta); err != nil {
		return p, err
	}
	beats, err := scriptBeats(p)
	if err != nil {
		return p, err
	}
	next.Duration = projectDuration(p) + delta
	if len(beats) > 0 {
		matches := beatHeader.FindAllStringSubmatchIndex(p.Script, -1)
		var b strings.Builder
		last := 0
		found := false
		for i, m := range matches {
			a, z := beats[i].Start, beats[i].End
			if start >= a && start < z {
				if start+old.Duration > z {
					return p, errors.New("当前镜头跨越剧本时间段，不能直接调整")
				}
				z += delta
				found = true
			} else if a > start {
				a += delta
				z += delta
			}
			b.WriteString(p.Script[last:m[2]])
			b.WriteString(strconv.Itoa(a))
			b.WriteString(p.Script[m[3]:m[4]])
			b.WriteString(strconv.Itoa(z))
			last = m[5]
		}
		if !found {
			return p, errors.New("当前镜头与剧本时间段不一致")
		}
		b.WriteString(p.Script[last:])
		next.Script = b.String()
		updated, err := scriptBeats(next)
		if err != nil {
			return p, err
		}
		mapping := map[string]string{}
		for i, b := range beats {
			mapping[b.Text] = updated[i].Text
		}
		for i := range shots {
			if text, ok := mapping[shots[i].SourceText]; ok {
				shots[i].SourceText = text
			} else {
				return p, errors.New("分镜原文依据与剧本不一致，请先核对规划")
			}
		}
	}
	if old.Video != "" {
		next.PreviousShots = append(next.PreviousShots, old)
	}
	q := &shots[index]
	q.Duration = duration
	q.Approved = false
	q.ReviewStatus = "pending"
	q.Video = ""
	q.VideoMode = ""
	q.VideoTaskID = ""
	q.VideoSourceFile = ""
	q.VideoEndShotID = ""
	q.VideoEndImageHash = ""
	if trial {
		next.PlannedShots = shots
	} else {
		next.Shots = shots
	}
	next.BoardsLocked = false
	next.Voice = ""
	next.Output = ""
	if err := validateFaithfulPlan(next, shots); err != nil {
		return p, err
	}
	return next, nil
}
func (s *Studio) editShotDuration(c *gin.Context) {
	var v struct {
		Revision        int  `json:"revision"`
		Duration        int  `json:"duration"`
		PlanTrial       bool `json:"plan_trial"`
		SettingsVersion int  `json:"settings_version"`
		FinishRejected  bool `json:"finish_rejected"`
	}
	if !bind(c, &v) {
		return
	}
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	p, err := s.store.project(c.Param("id"))
	if err != nil {
		result(c, nil, err)
		return
	}
	if p.Revision != v.Revision {
		result(c, nil, errConflict)
		return
	}
	cfg, err := s.store.settings()
	if err != nil {
		result(c, nil, err)
		return
	}
	if cfg.Version != v.SettingsVersion {
		result(c, nil, errors.New("模型配置已变化，请重新打开时长调整面板"))
		return
	}
	tasks, err := s.store.tasks(p.ID)
	if err != nil {
		result(c, nil, err)
		return
	}
	finish := []Task{}
	for _, t := range tasks {
		if taskUnresolved(t.Status) {
			if !v.FinishRejected || !s.store.durationRejectedTask(t, c.Param("shot"), v.PlanTrial) {
				result(c, nil, errors.New("仍有执行中或结果待核对的任务，不能修改其输入；请先在当前页恢复或结束任务"))
				return
			}
			finish = append(finish, t)
		}
	}
	updated, err := retimeShot(p, c.Param("shot"), v.Duration, v.PlanTrial, cfg)
	if err != nil {
		result(c, nil, err)
		return
	}
	if updated.Duration == p.Duration {
		result(c, p, nil)
		return
	}
	updated.Revision++
	updated.Updated = now()
	err = s.store.transaction(func(tx *sql.Tx) error {
		for _, t := range finish {
			t.Status = "abandoned"
			t.Updated = now()
			t.Message = "调整时长后结束已明确拒绝的失败任务；原回执与历史记录保留"
			if _, e := tx.Exec("UPDATE tasks SET status=?,body=? WHERE id=?", t.Status, jsonBytes(t), t.ID); e != nil {
				return e
			}
		}
		return s.store.saveProject(tx, updated)
	})
	result(c, updated, err)
}
