package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync/atomic"
)

// Workers own only their result; the coordinator alone saves input and usage.
func (s *Studio) fillVideos(ctx context.Context, t *Task, makeVideo videoMaker) error {
	frozen, err := decodeBody[Task](jsonBytes(*t))
	if err != nil {
		return err
	}
	// Config and Input are excluded from public task JSON.
	frozen.Config = t.Config
	frozen.Input, err = decodeBody[Project](jsonBytes(t.Input))
	if err != nil {
		return err
	}
	limit := generationConcurrency(t.Config.VideoConcurrency)
	type event struct {
		index int
		file  string
		err   error
		usage *VideoUsage
		ack   chan error
	}
	events := make(chan event, limit)
	persistUsage := func(u VideoUsage) error {
		for _, old := range t.VideoUsage {
			if old.ShotID == u.ShotID && old.RemoteID == u.RemoteID {
				return nil
			}
		}
		t.VideoUsage = append(t.VideoUsage, u)
		return s.store.saveTask(*t)
	}
	completed := 0
	save := func(i int, file string) error {
		if file == "" {
			return errors.New("供应商未返回镜头视频")
		}
		t.Input.Shots[i].Video = file
		recordVideoInput(t, i)
		completed++
		return s.progress(t, completed*85/len(t.Input.Shots), fmt.Sprintf("视频已完成%d/%d，最多%d路并行：%s", completed, len(t.Input.Shots), limit, t.Input.Shots[i].Title))
	}
	fresh := []int{}
	// Recover all existing submissions before admitting any new paid request.
	for i, shot := range frozen.Input.Shots {
		if shot.Video != "" {
			completed++
			continue
		}
		var state string
		err := s.store.db.QueryRow("SELECT status FROM attempts WHERE id=? AND task_id=?", t.ID+":video-"+shot.ID, t.ID).Scan(&state)
		if errors.Is(err, sql.ErrNoRows) {
			fresh = append(fresh, i)
			continue
		}
		if err != nil {
			return err
		}
		worker := frozen
		worker.usageRecorder = persistUsage
		file, err := makeVideo(ctx, &worker, shot)
		if err != nil {
			return err
		}
		if err = save(i, file); err != nil {
			return err
		}
	}
	var stop atomic.Bool
	active, next := 0, 0
	var firstError error
	start := func() {
		for active < limit && next < len(fresh) && !stop.Load() && ctx.Err() == nil {
			i := fresh[next]
			next++
			active++
			go func() {
				worker := frozen
				worker.usageRecorder = func(u VideoUsage) error {
					ack := make(chan error, 1)
					events <- event{usage: &u, ack: ack}
					return <-ack
				}
				file, err := makeVideo(ctx, &worker, frozen.Input.Shots[i])
				if err != nil {
					stop.Store(true)
					_, _ = s.store.db.Exec("UPDATE attempts SET status='unknown' WHERE id=? AND task_id=? AND status='submitting'", t.ID+":video-"+frozen.Input.Shots[i].ID, t.ID)
				}
				events <- event{index: i, file: file, err: err}
			}()
		}
	}
	start()
	for active > 0 {
		e := <-events
		if e.usage != nil {
			err := persistUsage(*e.usage)
			if err != nil {
				stop.Store(true)
			}
			e.ack <- err
			continue
		}
		active--
		err := e.err
		if err == nil {
			err = save(e.index, e.file)
		}
		if err != nil {
			stop.Store(true)
			if firstError == nil {
				firstError = err
			}
		}
		start()
	}
	// Keep collecting already submitted siblings after failure; only explicit
	// cancellation stops their contexts. Never retry an unknown submission.
	if firstError != nil {
		return firstError
	}
	return ctx.Err()
}
