package main

import "strings"

// Local, syntax-only repairs. Never invent IDs, fields, references or durations.
type DramaCandidateRepair struct {
	Line    int    `json:"line"`
	Message string `json:"message"`
}
type DramaCandidateCheck struct {
	Version      int                    `json:"version"`
	LinksChecked bool                   `json:"links_checked,omitempty"`
	Hash         string                 `json:"hash"`
	Repairs      []DramaCandidateRepair `json:"repairs"`
	Issues       []DramaIssue           `json:"issues"`
}

func checkDramaCandidate(kind, episode, original string) (string, DramaCandidateCheck) {
	text := original
	check := DramaCandidateCheck{Version: 2, Repairs: []DramaCandidateRepair{}, Issues: []DramaIssue{}}
	// Only an unambiguous, single outer Markdown fence may be unwrapped.
	lines := strings.Split(text, "\n")
	end := len(lines) - 1
	for end >= 0 && strings.TrimSpace(lines[end]) == "" {
		end--
	}
	if len(lines) > 1 && (lines[0] == "```markdown" || lines[0] == "```md") && end > 0 && lines[end] == "```" {
		innerFence := false
		for _, line := range lines[1:end] {
			if strings.HasPrefix(strings.TrimSpace(line), "```") {
				innerFence = true
			}
		}
		if !innerFence && strings.TrimSpace(strings.Join(lines[1:end], "\n")) != "" {
			text = strings.Join(lines[1:end], "\n") + "\n"
			check.Repairs = append(check.Repairs, DramaCandidateRepair{1, "移除包裹整份正文的 Markdown 代码围栏"})
		}
	}
	prefix := map[string]string{"images": "IMG-", "storyboard": "SHOT-", "motion": "MOTION-", "edit": "CUT-"}[kind]
	notes := map[string]bool{"交接、待选择项与文字审查": true, "文字自审与交接": true, "地点与道具的待选择项": true, "文字审查与交接": true, "自审与交接": true}
	if kind == "motion" {
		notes["逐镜挂图清单与投产边界"] = true
		notes["待选择项与文字审查"] = true
	}
	lines = strings.Split(text, "\n")
	hasEntry := false
	fencedEntry := false
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") || strings.HasPrefix(strings.TrimSpace(line), "~~~") {
			fencedEntry = !fencedEntry
			continue
		}
		if !fencedEntry && prefix != "" && strings.HasPrefix(line, "## "+prefix) {
			fields := strings.Fields(strings.TrimPrefix(line, "## "))
			if len(fields) > 0 && dramaEntryID.MatchString(fields[0]) {
				hasEntry = true
			}
		}
	}
	if hasEntry {
		fenced := false
		for i, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "```") || strings.HasPrefix(strings.TrimSpace(line), "~~~") {
				fenced = !fenced
				continue
			}
			if fenced || !strings.HasPrefix(line, "## ") || !notes[strings.TrimSpace(strings.TrimPrefix(line, "## "))] {
				continue
			}
			// A heading with production fields may be a damaged real entry. Leave it for review.
			productionFields := false
			for n := i + 1; n < len(lines) && !strings.HasPrefix(lines[n], "## "); n++ {
				if !strings.HasPrefix(lines[n], "- ") && !strings.HasPrefix(lines[n], "* ") {
					continue
				}
				field := strings.TrimSpace(lines[n][2:])
				key := field
				if colon := strings.IndexAny(field, "：:"); colon >= 0 {
					key = strings.TrimSpace(field[:colon])
				}
				switch key {
				case "来源", "时长", "起点", "终点", "唯一动作", "分镜", "入点", "出点", "输入参考图", "视觉依据", "用途", "参考", "声音", "目的", "景别/机位", "运镜", "取舍":
					productionFields = true
				}
			}
			if productionFields {
				continue
			}
			lines[i] = "#" + line
			check.Repairs = append(check.Repairs, DramaCandidateRepair{i + 1, "将说明标题改为三级标题：" + strings.TrimPrefix(line, "## ")})
		}
	}
	text = strings.Join(lines, "\n")
	_, check.Issues = dramaParseEpisode(kind, text, episode)
	check.Hash = digest([]byte(text))
	return text, check
}
func prepareDramaCandidate(w DramaWork, j *DramaJob, original string) {
	if j.CandidateCheck != nil && j.CandidateCheck.Version == 2 && j.CandidateCheck.Hash == digest([]byte(original)) {
		return
	}
	text, check := checkDramaCandidate(j.Kind, dramaEpisodeCode(w), original)
	if text != original && j.CandidateOriginal == "" {
		j.CandidateOriginal = original
	}
	j.Candidate = text
	j.CandidateCheck = &check
}

// Run after the pure format pass, and re-evaluate on local checks/adoption because upstream documents may change.
func (s *Studio) checkDramaCandidateLinks(j *DramaJob) {
	if j.CandidateCheck == nil || (j.Kind != "storyboard" && j.Kind != "motion") {
		return
	}
	issues := []DramaIssue{}
	for _, issue := range j.CandidateCheck.Issues {
		if issue.Rule != "LINK" {
			issues = append(issues, issue)
		}
	}
	j.CandidateCheck.LinksChecked = true
	if e := s.validateDramaLinks(j.WorkID, j.Kind, j.Candidate); e != nil {
		line := 1
		entries, _ := dramaParse(j.Kind, j.Candidate)
		for _, entry := range entries {
			if strings.HasPrefix(e.Error(), entry.ID+" ") {
				line = entry.Start + 1
				break
			}
		}
		issues = append(issues, DramaIssue{Line: line, Rule: "LINK", Message: e.Error()})
	}
	j.CandidateCheck.Issues = issues
}
