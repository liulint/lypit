package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
)

type DramaSharedOrigin struct {
	SeriesID string `json:"series_id"`
	Version  int    `json:"version"`
	MediaID  string `json:"media_id"`
	WorkID   string `json:"work_id"`
	Hash     string `json:"hash"`
}
type DramaReuseResult struct {
	WorkID   string   `json:"work_id"`
	State    string   `json:"state"`
	Error    string   `json:"error,omitempty"`
	MediaIDs []string `json:"media_ids"`
}
type DramaReuseReceipt struct {
	ID        string             `json:"id"`
	SeriesID  string             `json:"series_id"`
	InputHash string             `json:"input_hash"`
	Sources   []DramaMedia       `json:"sources"`
	Results   []DramaReuseResult `json:"results"`
	Version   int                `json:"version"`
}

func (s *Studio) copyDramaShared(target string, series string, version int, sources []DramaMedia) ([]string, error) {
	w, e := s.dramaWork(target)
	if e != nil {
		return nil, e
	}
	if w.SeriesID != series {
		return nil, errors.New("只能复用到本系列分集")
	}
	all, e := s.dramaMedias(target)
	if e != nil {
		return nil, e
	}
	ids := []string{}
	pending := []DramaMedia{}
	for _, source := range sources {
		var found *DramaMedia
		for _, m := range all {
			if (m.ID == source.ID || m.Parent == source.ID) && m.Hash == source.Hash && m.Approved {
				v := m
				found = &v
				break
			}
		}
		if found != nil {
			if e = s.verifyDramaMedia(*found); e != nil {
				return nil, e
			}
			ids = append(ids, found.ID)
			continue
		}
		m, e := s.dramaMediaByID(source.WorkID, source.ID)
		if e != nil {
			return nil, e
		}
		if m.Hash != source.Hash || !m.Approved || !s.dramaMediaCurrent(m) {
			return nil, errors.New("共用素材的确认状态或来源发生变化，请核对后重新选择")
		}
		if e = s.verifyDramaMedia(source); e != nil {
			return nil, e
		}
		raw, e := os.ReadFile(s.media(source.File))
		if e != nil {
			return nil, e
		}
		if digest(raw) != source.Hash {
			return nil, errors.New("复制期间源文件发生变化")
		}
		clone := source
		clone.ID = digest([]byte("shared:" + series + ":" + target + ":" + source.ID + ":" + source.Hash))[:24]
		clone.WorkID = target
		clone.Parent = source.ID
		clone.TaskID = ""
		clone.SharedOrigin = &DramaSharedOrigin{SeriesID: series, Version: version, MediaID: source.ID, WorkID: source.WorkID, Hash: source.Hash}
		clone.File = target + "/" + clone.ID + filepath.Ext(source.File)
		clone.Path = "剧集/" + dramaEpisodeCode(w) + "/共用素材/" + filepath.Base(clone.File)
		clone.Current = true
		// Deterministic paths allow recovery after a crash between copying and commit.
		// Never overwrite a file whose bytes differ from this exact source snapshot.
		existing, err := os.ReadFile(s.media(clone.File))
		if err == nil {
			if digest(existing) != clone.Hash {
				return nil, errors.New("目标副本已有不同内容，未覆盖")
			}
		} else if os.IsNotExist(err) {
			if _, e = s.writeMedia(target, filepath.Base(clone.File), raw); e != nil {
				return nil, e
			}
		} else {
			return nil, err
		}
		if e = s.verifyDramaMedia(clone); e != nil {
			return nil, e
		}
		ids = append(ids, clone.ID)
		pending = append(pending, clone)
	}
	e = s.store.transaction(func(tx *sql.Tx) error {
		for _, m := range pending {
			var raw []byte
			err := tx.QueryRow("SELECT body FROM drama_media WHERE id=?", m.ID).Scan(&raw)
			if err == nil {
				var old DramaMedia
				if json.Unmarshal(raw, &old) != nil || old.WorkID != target || old.Hash != m.Hash || old.File != m.File {
					return errConflict
				}
				continue
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if _, err = tx.Exec("INSERT INTO drama_media VALUES(?,?,?)", m.ID, target, jsonBytes(m)); err != nil {
				return err
			}
		}
		return nil
	})
	return ids, e
}
func (s *Studio) dramaReuseRoutes(g *gin.RouterGroup) {
	g.POST("/series/:series/reuse-media", func(c *gin.Context) {
		var req struct {
			OperationID string   `json:"operation_id"`
			Version     int      `json:"version"`
			SeriesHash  string   `json:"series_hash"`
			MediaIDs    []string `json:"media_ids"`
			WorkIDs     []string `json:"work_ids"`
		}
		if !bind(c, &req) {
			return
		}
		if !dramaID.MatchString(req.OperationID) || req.Version < 1 || len(req.MediaIDs) < 1 || len(req.MediaIDs) > 40 || len(req.WorkIDs) < 1 || len(req.WorkIDs) > 999 {
			result(c, nil, errors.New("请选择共用素材、目标分集和准确版本"))
			return
		}
		s.dramaMu.Lock()
		defer s.dramaMu.Unlock()
		series := c.Param("series")
		hash := digest(jsonBytes(req))
		var raw []byte
		var receipt DramaReuseReceipt
		e := s.store.db.QueryRow("SELECT body FROM drama_media_reuse WHERE id=?", req.OperationID).Scan(&raw)
		if e == nil {
			if e = json.Unmarshal(raw, &receipt); e != nil {
				result(c, nil, e)
				return
			}
			if receipt.InputHash != hash || receipt.SeriesID != series {
				result(c, nil, errConflict)
				return
			}
		} else if !errors.Is(e, sql.ErrNoRows) {
			result(c, nil, e)
			return
		} else {
			v, err := s.dramaSeries(series, req.Version)
			if err != nil {
				result(c, nil, err)
				return
			}
			if v.Hash != req.SeriesHash {
				result(c, nil, errConflict)
				return
			}
			receipt = DramaReuseReceipt{ID: req.OperationID, SeriesID: series, InputHash: hash, Version: v.Version, Sources: []DramaMedia{}, Results: []DramaReuseResult{}}
			seen := map[string]bool{}
			for _, id := range req.MediaIDs {
				if seen[id] {
					result(c, nil, errors.New("素材重复"))
					return
				}
				seen[id] = true
				found := false
				for _, m := range v.Media {
					if m.ID == id {
						receipt.Sources = append(receipt.Sources, m)
						found = true
						break
					}
				}
				if !found {
					result(c, nil, errors.New("素材不在所选共用版本中"))
					return
				}
			}
			seen = map[string]bool{}
			for _, id := range req.WorkIDs {
				w, err := s.dramaWork(id)
				if err != nil {
					result(c, nil, err)
					return
				}
				if seen[id] || w.SeriesID != series {
					result(c, nil, errors.New("目标须为本系列不同分集"))
					return
				}
				seen[id] = true
				receipt.Results = append(receipt.Results, DramaReuseResult{WorkID: id, State: "pending", MediaIDs: []string{}})
			}
			if _, e = s.store.db.Exec("INSERT INTO drama_media_reuse VALUES(?,?)", receipt.ID, jsonBytes(receipt)); e != nil {
				result(c, nil, e)
				return
			}
		}
		for i := range receipt.Results {
			r := &receipt.Results[i]
			if r.State == "completed" {
				continue
			}
			ids, err := s.copyDramaShared(r.WorkID, series, receipt.Version, receipt.Sources)
			r.Error = ""
			if err != nil {
				r.State = "failed"
				r.Error = err.Error()
			} else {
				r.State = "completed"
				r.MediaIDs = ids
			}
			if _, e = s.store.db.Exec("UPDATE drama_media_reuse SET body=? WHERE id=?", jsonBytes(receipt), receipt.ID); e != nil {
				result(c, nil, e)
				return
			}
		}
		result(c, receipt, nil)
	})
}
