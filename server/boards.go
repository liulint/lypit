package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/gin-gonic/gin"
)

const maxGenerationConcurrency = 8

// Missing limits in historical task snapshots use the original default.
func generationConcurrency(n int) int {
	if n < 1 {
		return 2
	}
	if n > maxGenerationConcurrency {
		return maxGenerationConcurrency
	}
	return n
}

// A missing field belongs to the historical two-image authorization.
func boardImagesPerShot(t Task) int {
	if t.BoardImagesPerShot == 1 {
		return 1
	}
	return 2
}

// A local upgrade reservation is explicitly NOT a provider submission. A legacy
// worker stops when it reaches this occupied key, before invoking a CLI or POST.
// Only this exact marker may be cleared; submitting/rejected/unknown intents never are.
const boardDeferredState = "not_submitted"
const boardDeferredReceipt = `{"kind":"lypit-board-handoff@1","submitted":false}`

type boardImageJob struct {
	Shot, Candidate int
	Key, Prompt     string
	References      []Image
}
type imageMaker func(context.Context, *Task, string, string, []Image, bool) (Image, error)

// Only the coordinator mutates/persists Task.Input. Workers receive an immutable snapshot.
func (s *Studio) fillBoardImages(ctx context.Context, t *Task, makeImage imageMaker) error {
	p := &t.Input
	concurrency := generationConcurrency(t.Config.ImageConcurrency)
	if len(p.Shots) == 0 {
		return errors.New("分镜规划为空")
	}
	queue := []boardImageJob{}
	count := boardImagesPerShot(*t)
	completed, total := 0, len(p.Shots)*count
	for i := range p.Shots {
		q := &p.Shots[i]
		if len(q.Candidates) > count {
			return errors.New("分镜候选数量超出已确认范围")
		}
		slots := make([]Image, count)
		copy(slots, q.Candidates)
		q.Candidates = slots
		refs, err := s.taskBoardReferences(*t, *q)
		if err != nil {
			return err
		}
		// All missing shots must fit before admitting the first new image request.
		if t.Mode == "remote" && (t.ReferencePolicyVersion == referencePlanVersion || t.ReferencePolicyVersion == fourViewPolicyVersion) {
			missing := false
			for _, im := range slots {
				if im.File == "" {
					missing = true
				}
			}
			if missing {
				if _, _, e := s.prepareTaskReferences(t, "shot-"+q.ID+"-0", refs); e != nil {
					return e
				}
			}
		}
		for j, im := range slots {
			if im.File != "" {
				completed++
				continue
			}
			prompt := p.Style + "。" + aspectLabel(*p) + "电影分镜，严格保持参考人物身份、服装和场景。" + boardFrame(*q)
			if count == 2 {
				prompt += fmt.Sprintf("，候选构图方案%d", j+1)
			} else {
				prompt += "。仅生成一张完整画面，不要拼图或方案对比。"
			}
			prompt, err = t.renderPrompt("board_image", prompt, map[string]string{"style": p.Style, "aspect": aspectLabel(*p), "first_frame": boardFrame(*q)})
			if err != nil {
				return err
			}
			queue = append(queue, boardImageJob{Shot: i, Candidate: j, Key: fmt.Sprintf("shot-%s-%d", q.ID, j), Prompt: prompt, References: refs})
		}
	}
	frozenInput, err := decodeBody[Project](jsonBytes(*p))
	if err != nil {
		return err
	}
	// Preserve the complete quoted task scope (including Stage) for provider validation.
	frozen := *t
	frozen.Input = frozenInput
	report := func(last string) error {
		return s.progress(t, 5+completed*90/total, fmt.Sprintf("文字规划已完成：%d镜；分镜图已完成%d/%d张，最多%d张并行%s", len(p.Shots), completed, total, concurrency, last))
	}
	if err = report(""); err != nil {
		return err
	}
	save := func(job boardImageJob, im Image) error {
		if im.File == "" {
			return errors.New("生图未返回图片")
		}
		if t.ReferencePolicyVersion == fourViewPolicyVersion {
			plan := t.ShotReferencePlans[job.Key]
			im.ReferencePlan = &plan
		}
		p.Shots[job.Shot].Candidates[job.Candidate] = im
		completed++
		last := "；最近完成：" + p.Shots[job.Shot].Title
		if count == 2 {
			last += " · " + []string{"A", "B"}[job.Candidate]
		}
		return report(last)
	}
	// First collect ALL existing receipts sequentially. An unknown old submission must
	// block admission of new paid work, including jobs that would otherwise run alongside it.
	fresh := []boardImageJob{}
	deferred := []boardImageJob{}
	for _, job := range queue {
		var state string
		var receipt []byte
		err = s.store.db.QueryRow("SELECT status,response FROM attempts WHERE id=? AND task_id=?", t.ID+":"+job.Key, t.ID).Scan(&state, &receipt)
		if errors.Is(err, sql.ErrNoRows) {
			fresh = append(fresh, job)
			continue
		}
		if err != nil {
			return err
		}
		if state == boardDeferredState {
			if string(receipt) != boardDeferredReceipt {
				return errors.New("本地暂停标记无效，禁止清除原请求记录")
			}
			deferred = append(deferred, job)
			fresh = append(fresh, job)
			continue
		}
		worker := frozen
		im, err := makeImage(ctx, &worker, job.Key, job.Prompt, job.References, false)
		if err != nil {
			return err
		}
		if err = save(job, im); err != nil {
			return err
		}
	}
	// All old submissions have been recovered successfully before reserving new work.
	for _, job := range deferred {
		result, err := s.store.db.Exec("DELETE FROM attempts WHERE id=? AND task_id=? AND status=? AND response=?", t.ID+":"+job.Key, t.ID, boardDeferredState, []byte(boardDeferredReceipt))
		if err != nil {
			return err
		}
		if n, _ := result.RowsAffected(); n != 1 {
			return errors.New("本地暂停标记已变化，请重新读取原任务")
		}
	}
	type result struct {
		Job   boardImageJob
		Image Image
		Err   error
	}
	results := make(chan result, concurrency)
	var stop atomic.Bool
	active, next := 0, 0
	var firstError error
	start := func() {
		for active < concurrency && next < len(fresh) && !stop.Load() && ctx.Err() == nil {
			job := fresh[next]
			next++
			active++
			go func() {
				worker := frozen
				im, err := makeImage(ctx, &worker, job.Key, job.Prompt, job.References, false)
				if err != nil {
					stop.Store(true)
					// An ended request without a durable receipt is unknown, not in flight.
					_, _ = s.store.db.Exec("UPDATE attempts SET status='unknown' WHERE id=? AND task_id=? AND status='submitting'", t.ID+":"+job.Key, t.ID)
				}
				results <- result{Job: job, Image: im, Err: err}
			}()
		}
	}
	start()
	for active > 0 {
		r := <-results
		active--
		err := r.Err
		if err == nil {
			err = save(r.Job, r.Image)
		}
		if err != nil {
			stop.Store(true)
			if firstError == nil {
				firstError = err
				t.Status = "failed"
				t.Message = err.Error()
				_ = s.store.saveTask(*t)
			}
		}
		start()
	}
	// Do not cancel the other already-submitted request on a single-image failure:
	// wait for its receipt and save the result, rather than turning it into an unknown submission.
	if firstError != nil {
		return firstError
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if completed != total {
		return errors.New("分镜图片尚未全部完成，请查看原任务")
	}
	return nil
}

func (s *Studio) boardPreview(c *gin.Context) {
	t, err := s.store.task(c.Param("id"))
	if err != nil {
		result(c, nil, err)
		return
	}
	if t.Stage != "boards" {
		result(c, nil, errors.New("此任务没有分镜预览"))
		return
	}
	completed := 0
	for _, q := range t.Input.Shots {
		for _, im := range q.Candidates {
			if im.File != "" {
				completed++
			}
		}
	}
	// Explicit allowlist: no settings, credentials, raw provider responses or other task inputs.
	shots := t.Input.Shots
	if shots == nil {
		shots = []Shot{}
	}
	c.JSON(200, gin.H{"task_id": t.ID, "project_id": t.ProjectID, "revision": t.Input.Revision, "status": t.Status, "shots": shots, "completed": completed, "total": len(t.Input.Shots) * boardImagesPerShot(t)})
}
