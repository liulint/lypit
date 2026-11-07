package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type cappedOutput struct {
	bytes.Buffer
	limit int
}

func (w *cappedOutput) Write(p []byte) (int, error) {
	n := len(p)
	remaining := w.limit - w.Len()
	if remaining <= 0 {
		return n, nil
	}
	if len(p) > remaining {
		p = p[:remaining]
	}
	_, e := w.Buffer.Write(p)
	return n, e
}
func cliArgs(ep Endpoint, dir, output string) []string {
	if ep.CLI == "codex" {
		args := []string{"exec", "--json", "--skip-git-repo-check", "--sandbox", "read-only", "--ephemeral", "--color", "never", "--cd", dir, "--output-last-message", output}
		if ep.Model != "" {
			args = append(args, "--model", ep.Model)
		}
		return append(args, "-")
	}
	args := []string{"--safe-mode", "--print", "--output-format", "json", "--tools", "", "--no-session-persistence"}
	if ep.Model != "" {
		args = append(args, "--model", ep.Model)
	}
	return args
}
func (s *Studio) cliTextJSON(ctx context.Context, t *Task, key, prompt string, ep Endpoint, out any) error {
	var e error
	id := t.ID + ":" + key
	dir := filepath.Join(s.data, "cli", t.ID, key)
	output := filepath.Join(dir, "final.txt")
	var state string
	var response []byte
	e = s.store.db.QueryRow("SELECT status,response FROM attempts WHERE id=?", id).Scan(&state, &response)
	if e == nil {
		if state == "received" {
			return json.Unmarshal(response, out)
		}
		// Codex may have saved its final response before the parent process was interrupted.
		if ep.CLI == "codex" {
			if b, e := readCLIFile(output); e == nil && json.Valid(b) {
				response = b
			} else {
				return errors.New("原CLI执行结果未知，已阻止重新执行；请核对本机账户用量与原任务")
			}
		} else {
			return errors.New("原CLI执行未保存完整响应，禁止自动重新调用")
		}
	} else if !errors.Is(e, sql.ErrNoRows) {
		return e
	} else {
		path, lookupErr := cliExecutable(ep)
		if lookupErr != nil {
			return lookupErr
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		if e = os.MkdirAll(dir, 0700); e != nil {
			return e
		}
		cmd, commandErr := newCLICommand(ctx, path, ep.CLI, cliArgs(ep, dir, output)...)
		if commandErr != nil {
			return commandErr
		}
		if _, e = s.store.db.Exec("INSERT INTO attempts(id,task_id,status) VALUES(?,?,'submitting')", id, t.ID); e != nil {
			return e
		}
		cmd.Dir = dir
		cmd.Stdin = strings.NewReader("只处理下面的文本规划任务，不读取本机文件，不调用外部工具。严格输出JSON对象，不要Markdown代码块。\n" + prompt)
		configureProcess(cmd)
		cmd.WaitDelay = 3 * time.Second
		stdout := &cappedOutput{limit: 2 << 20}
		if e = s.runCLIWithDiagnostics(cmd, t, dir, stdout); e != nil {
			return errors.New("CLI执行未完成；请确认安装版本、登录与模型权限。已保留提交记录，不会自动重跑")
		}
		if ep.CLI == "codex" {
			response, e = readCLIFile(output)
			if e != nil {
				return e
			}
		} else {
			var envelope struct {
				Result string `json:"result"`
				Error  bool   `json:"is_error"`
			}
			if e = json.Unmarshal(stdout.Bytes(), &envelope); e != nil || envelope.Error || envelope.Result == "" {
				return errors.New("CLI没有返回成功结果，禁止自动重跑")
			}
			response = []byte(envelope.Result)
		}
	}
	if _, e = s.store.db.Exec("UPDATE attempts SET status='received',response=? WHERE id=?", response, id); e != nil {
		return e
	}
	if e = json.Unmarshal(response, out); e != nil {
		return errors.New("CLI未返回有效JSON；原响应已保存，恢复只重新解析，不会重复调用")
	}
	return nil
}
func readCLIFile(path string) ([]byte, error) {
	info, e := os.Lstat(path)
	if e != nil || !info.Mode().IsRegular() || info.Size() > 2<<20 {
		return nil, errors.New("CLI最终响应缺失、过大或文件类型无效")
	}
	return os.ReadFile(path)
}
