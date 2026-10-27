package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"
)

const apiBase = "https://ark.cn-beijing.volces.com/api/v3/contents/generations/tasks"

type stringsFlag []string

func (s *stringsFlag) String() string     { return strings.Join(*s, ",") }
func (s *stringsFlag) Set(v string) error { *s = append(*s, v); return nil }

type Options struct {
	Model                   string        `json:"model"`
	PriceModel              string        `json:"price_model"`
	Resolution              string        `json:"resolution"`
	Audio                   bool          `json:"generate_audio"`
	Duration                int           `json:"duration"`
	Ratio                   string        `json:"ratio"`
	Prompt                  string        `json:"prompt"`
	Images                  stringsFlag   `json:"images"`
	Videos                  stringsFlag   `json:"videos"`
	Count                   int           `json:"count"`
	QuoteOnly, DryRun, JSON bool          `json:"-"`
	Confirm, Resume, Out    string        `json:"-"`
	Poll, Wait              time.Duration `json:"-"`
}

var models = map[string]string{
	"mini":     "doubao-seedance-2-0-mini-260615",
	"fast":     "doubao-seedance-2-0-fast-260128",
	"standard": "doubao-seedance-2-0-260128",
}
var identifier = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,199}$`)

func family(model string) string {
	if _, ok := models[model]; ok {
		return model
	}
	for alias, id := range models {
		if id == model {
			return alias
		}
	}
	return ""
}

func parseOptions(args []string, stderr io.Writer) (Options, error) {
	o := Options{}
	f := flag.NewFlagSet("seedance", flag.ContinueOnError)
	f.SetOutput(stderr)
	f.StringVar(&o.Model, "model", "mini", "mini / fast / standard / 完整模型 ID / 接入点 ID")
	f.StringVar(&o.PriceModel, "price-model", "", "接入点或未知模型必须显式关联 mini / fast / standard")
	f.StringVar(&o.Resolution, "resolution", "480p", "480p / 720p")
	f.BoolVar(&o.Audio, "audio", true, "输出声音；无声用 -audio=false")
	f.IntVar(&o.Duration, "duration", 5, "4–15 秒整数")
	f.StringVar(&o.Ratio, "ratio", "16:9", "16:9 / 4:3 / 1:1 / 3:4 / 9:16 / 21:9")
	f.StringVar(&o.Prompt, "prompt", "", "最终提示词")
	var promptFile string
	f.StringVar(&promptFile, "prompt-file", "", "UTF-8 最终提示词文件，避免 shell 转义")
	f.Var(&o.Images, "image", "本地参考图片，可重复")
	f.Var(&o.Videos, "video-url", "公网参考视频 URL，可重复")
	f.IntVar(&o.Count, "count", 1, "生成数量（每条分别创建付费任务）")
	f.BoolVar(&o.QuoteOnly, "quote", false, "校验并报价，不创建任务")
	f.BoolVar(&o.DryRun, "dry-run", false, "无付费演练；未传提示词时使用演练提示词")
	f.BoolVar(&o.JSON, "json", false, "输出 JSON")
	f.StringVar(&o.Confirm, "confirm", "", "用户确认后传入本次报价的 confirmation_id")
	f.StringVar(&o.Resume, "resume", "", "恢复已有 run.json，仅 GET 和下载，绝不 POST")
	f.StringVar(&o.Out, "out", "", "新任务目录；默认 ./seedance-output/<confirmation_id>")
	f.DurationVar(&o.Poll, "poll", 10*time.Second, "查询间隔")
	f.DurationVar(&o.Wait, "wait", 30*time.Minute, "本次等待时限；超时后可恢复")
	if err := f.Parse(args); err != nil {
		return o, err
	}
	if f.NArg() != 0 {
		return o, errors.New("存在未解析的位置参数，请使用 -prompt-file 或正确引用参数")
	}
	if o.Resume != "" {
		var invalid bool
		f.Visit(func(v *flag.Flag) {
			switch v.Name {
			case "resume", "json", "poll", "wait":
			default:
				invalid = true
			}
		})
		if invalid {
			return o, errors.New("-resume 只能搭配 -json、-poll、-wait，避免误改已有任务")
		}
		if o.Poll <= 0 || o.Wait <= 0 {
			return o, errors.New("poll 和 wait 必须大于零")
		}
		return o, nil
	}
	if promptFile != "" {
		if o.Prompt != "" {
			return o, errors.New("prompt 和 prompt-file 不能同时使用")
		}
		b, err := os.ReadFile(promptFile)
		if err != nil {
			return o, errors.New("无法读取提示词文件")
		}
		o.Prompt = string(b)
	}
	if o.DryRun && strings.TrimSpace(o.Prompt) == "" {
		o.Prompt = "演练：一只纸鹤在阳光下展开翅膀。"
	}
	if strings.TrimSpace(o.Prompt) == "" {
		return o, errors.New("必须提供最终提示词")
	}
	if !identifier.MatchString(o.Model) {
		return o, errors.New("模型 ID 格式无效")
	}
	originalFamily := family(o.Model)
	if id, ok := models[o.Model]; ok {
		o.Model = id
	}
	if o.PriceModel != "" && family(o.PriceModel) == "" {
		return o, errors.New("price-model 必须是已知计价模型")
	}
	if originalFamily != "" && o.PriceModel != "" && originalFamily != family(o.PriceModel) {
		return o, errors.New("price-model 与已知模型不一致")
	}
	if o.PriceModel == "" {
		o.PriceModel = originalFamily
	} else {
		o.PriceModel = family(o.PriceModel)
	}
	if o.Resolution != "480p" && o.Resolution != "720p" {
		return o, errors.New("仅支持 480p、720p")
	}
	if _, ok := dimensions[o.Resolution][o.Ratio]; !ok {
		return o, errors.New("不支持此比例；不提供 adaptive")
	}
	if o.Duration < 4 || o.Duration > 15 {
		return o, errors.New("时长必须为 4–15 秒整数")
	}
	if o.Count < 1 || o.Count > 100 {
		return o, errors.New("数量必须为 1–100")
	}
	if len(o.Images) > 9 || len(o.Videos) > 3 {
		return o, errors.New("最多 9 张图片、3 个视频")
	}
	if o.Poll <= 0 || o.Wait <= 0 {
		return o, fmt.Errorf("poll 和 wait 必须大于零")
	}
	return o, nil
}
