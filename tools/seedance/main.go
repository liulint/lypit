package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
)

func tokenText(v *int64) string {
	if v == nil {
		return "未返回"
	}
	return fmt.Sprint(*v)
}
func moneyRange(min, max *float64) string {
	if min == nil {
		return "未知"
	}
	if max == nil {
		return fmt.Sprintf("至少 ¥%.4f，上界未知", *min)
	}
	if *min == *max {
		return fmt.Sprintf("约 ¥%.4f", *min)
	}
	return fmt.Sprintf("约 ¥%.4f–%.4f", *min, *max)
}
func printQuote(w io.Writer, q Quote) {
	o := q.Options
	audio := "无声"
	if o.Audio {
		audio = "有声"
	}
	fmt.Fprintf(w, "模型：%s（计价：%s）\n分辨率：%s（%d×%d）\n声音：%s\n时长：%d 秒\n比例：%s\n提示词：%s\n数量：%d 条\n", o.Model, o.PriceModel, o.Resolution, q.Width, q.Height, audio, o.Duration, o.Ratio, o.Prompt, o.Count)
	if len(q.Media) == 0 {
		fmt.Fprintln(w, "参考素材：无")
	} else {
		for _, m := range q.Media {
			fmt.Fprintf(w, "参考素材：%s %s", m.Kind, m.Source)
			if m.Kind == "video" {
				fmt.Fprintf(w, "（%.3f 秒）", m.Duration)
			}
			fmt.Fprintln(w)
		}
	}
	if q.Unit == nil {
		fmt.Fprintln(w, "计费单价：未知")
	} else {
		fmt.Fprintf(w, "计费单价：%.2f 元/百万 token\n", *q.Unit)
	}
	fmt.Fprintf(w, "单条预估：%s\n总预估：%s\n计价依据：%s\n来源：%s\n核实日期：%s\n可提交：%t\nconfirmation_id：%s\n", moneyRange(q.MinYuan, q.MaxYuan), moneyRange(q.TotalMin, q.TotalMax), q.Basis, q.Source, q.Verified, q.CanSubmit, q.ConfirmationID)
	for _, v := range q.Warnings {
		fmt.Fprintln(w, "注意："+v)
	}
	fmt.Fprintln(w, "请先由用户确认以上全部最终参数和报价，再使用 -confirm 提交。")
}
func printRecord(w io.Writer, r *Record, path string) {
	fmt.Fprintln(w, "任务记录："+path)
	for _, t := range r.Tasks {
		audio := "未返回"
		if t.Audio != nil {
			if *t.Audio {
				audio = "有声"
			} else {
				audio = "无声"
			}
		}
		fmt.Fprintf(w, "任务：%s 状态：%s\n实际模型：%s\n实际规格：%s / %s / %d 秒 / 声音 %s（空字段或 0 秒表示未返回）\n", t.ID, t.Status, t.Model, t.Resolution, t.Ratio, t.Duration, audio)
		var c, total *int64
		if t.Usage != nil {
			c = t.Usage.Completion
			total = t.Usage.Total
		}
		fmt.Fprintf(w, "completion_tokens：%s；total_tokens：%s\n费用：%s\n依据：%s\n", tokenText(c), tokenText(total), moneyRange(t.EstimatedFee, t.EstimatedFee), t.FeeBasis)
		if t.File != "" {
			fmt.Fprintln(w, "视频："+t.File)
		}
		if t.DownloadError != "" {
			fmt.Fprintln(w, t.DownloadError)
		}
	}
}
func execute(ctx context.Context, args []string, stdout, stderr io.Writer, e *Engine) error {
	o, err := parseOptions(args, stderr)
	if err != nil {
		return err
	}
	if o.Resume != "" {
		if e.Key == "" {
			return errors.New("缺少 ARK_API_KEY；请在本地环境变量配置，不要在对话中发送密钥")
		}
		r, path, err := e.resume(ctx, o)
		if r != nil {
			if o.JSON {
				_ = json.NewEncoder(stdout).Encode(struct {
					Record *Record `json:"record"`
					Path   string  `json:"record_path"`
				}{r, path})
			} else {
				printRecord(stdout, r, path)
			}
		}
		return err
	}
	req, media, warnings, valid, err := prepare(ctx, o, e.Media)
	if err != nil {
		return err
	}
	q := makeQuote(o, media, warnings, valid)
	if o.QuoteOnly || o.DryRun {
		if o.JSON {
			return json.NewEncoder(stdout).Encode(q)
		}
		printQuote(stdout, q)
		return nil
	}
	if e.Key == "" {
		return errors.New("缺少 ARK_API_KEY；请在本地环境变量配置，不要在对话中发送密钥")
	}
	r, path, err := e.submit(ctx, o, q, req)
	if r != nil {
		if o.JSON {
			_ = json.NewEncoder(stdout).Encode(struct {
				Record *Record `json:"record"`
				Path   string  `json:"record_path"`
			}{r, path})
		} else {
			printRecord(stdout, r, path)
		}
	}
	return err
}
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := execute(ctx, os.Args[1:], os.Stdout, os.Stderr, newEngine(os.Getenv("ARK_API_KEY"))); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintln(os.Stderr, "错误："+err.Error())
		os.Exit(1)
	}
}
