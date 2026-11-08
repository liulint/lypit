package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
)

func (s *Studio) executeDramaBatch(ctx context.Context, t *Task, j *DramaJob, execute func(context.Context, *Task, *DramaJob) error) error {
	frozen := *t
	frozen.Input, _ = decodeBody[Project](jsonBytes(t.Input))
	frozen.VideoUsage = append([]VideoUsage(nil), t.VideoUsage...)
	items := j.Production.Items
	if len(items) == 0 {
		return errors.New("批次为空")
	}
	limit := generationConcurrency(t.Config.ImageConcurrency)
	if items[0].Kind == "video" {
		limit = generationConcurrency(t.Config.VideoConcurrency)
	}
	type event struct {
		err   error
		usage *VideoUsage
		ack   chan error
	}
	events := make(chan event, limit)
	var stop atomic.Bool
	persist := func(u VideoUsage) error {
		for _, v := range t.VideoUsage {
			if v.ShotID == u.ShotID && v.RemoteID == u.RemoteID {
				return nil
			}
		}
		t.VideoUsage = append(t.VideoUsage, u)
		return s.store.saveTask(*t)
	}
	worker := func(item DramaProduction, record func(VideoUsage) error) error {
		copyTask := frozen
		copyTask.Input, _ = decodeBody[Project](jsonBytes(frozen.Input))
		copyTask.VideoUsage = append([]VideoUsage(nil), frozen.VideoUsage...)
		copyTask.usageRecorder = record
		child := *j
		child.Production = &item
		return execute(ctx, &copyTask, &child)
	}
	fresh := []DramaProduction{}
	completed := 0
	// Reconcile every existing receipt before any new admission. Unknown receipts
	// cannot be displaced by a newer workflow, ordering, or batching choice.
	for _, item := range items {
		key := "shot-drama-" + digest([]byte(item.Entry))[:16]
		if item.Kind == "video" {
			key = "video-" + strings.TrimPrefix(item.Entry, "MOTION-")
		}
		var status string
		e := s.store.db.QueryRow("SELECT status FROM attempts WHERE id=?", t.ID+":"+key).Scan(&status)
		if errors.Is(e, sql.ErrNoRows) {
			fresh = append(fresh, item)
			continue
		}
		if e != nil {
			return e
		}
		if e = worker(item, persist); e != nil {
			return fmt.Errorf("恢复%s：%w", item.Entry, e)
		}
		completed++
	}
	active, next := 0, 0
	var first error
	start := func() {
		for active < limit && next < len(fresh) && !stop.Load() && ctx.Err() == nil {
			item := fresh[next]
			next++
			active++
			go func() {
				err := worker(item, func(u VideoUsage) error {
					ack := make(chan error, 1)
					events <- event{usage: &u, ack: ack}
					return <-ack
				})
				if err != nil {
					stop.Store(true)
				}
				events <- event{err: err}
			}()
		}
	}
	start()
	for active > 0 {
		e := <-events
		if e.usage != nil {
			err := persist(*e.usage)
			if err != nil {
				stop.Store(true)
				if first == nil {
					first = err
				}
			}
			e.ack <- err
			continue
		}
		active--
		if e.err != nil {
			stop.Store(true)
			if first == nil {
				first = e.err
			}
		} else {
			completed++
			t.Progress = completed * 100 / len(items)
			if err := s.store.saveTask(*t); err != nil {
				stop.Store(true)
				if first == nil {
					first = err
				}
			}
		}
		start()
	}
	if first != nil {
		return first
	}
	return ctx.Err()
}
