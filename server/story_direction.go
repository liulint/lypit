package main

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

const storyDirectionVersion = 1

// Design references checked 2026-11-07 (principles only; prompts below are original):
// https://github.com/Emily2040/seedance-2.0 (MIT), one visible action / local endpoint.
// https://github.com/momorzq-oss/Continuity-Studio (Apache-2.0), start/end state handoff.

// These are editorial budgets, not a claim that a video model has been inspected.
// Compile them into editable description text so review, quotes and video inputs
// all use the same instructions without hidden, stale continuity metadata.
type shotExecution struct {
	StartState    string `json:"start_state"`
	MainAction    string `json:"main_action"`
	EndState      string `json:"end_state"`
	Bridge        string `json:"bridge"`
	SetupSeconds  int    `json:"setup_seconds"`
	ActionSeconds int    `json:"action_seconds"`
	SpeechSeconds int    `json:"speech_seconds"`
	SettleSeconds int    `json:"settle_seconds"`
	SpeechOverlap bool   `json:"speech_overlap"`
}

type directedShot struct {
	Shot
	Execution shotExecution `json:"execution"`
}

const executionSchema = `"execution":{"start_state":"本镜开场人物位置、朝向、道具归属和动作阶段","main_action":"唯一主要动作及可见完成标志","end_state":"切出前画面内看得见的完成状态","bridge":"与上一镜的因果和状态承接；换时空写明省略及视觉线索，首镜写开场建立","setup_seconds":1,"action_seconds":3,"speech_seconds":0,"settle_seconds":1,"speech_overlap":false}`

const directingRules = `
执行节奏规则：每镜只承担一个叙事任务、一个主要动作，最多一个简单伴随反应；运镜最多一种，禁止在单镜描述中暗藏切镜、特写插入、换场或多轮动作。5秒不是默认时长，按真实动作和台词负担分配整数秒，简单反应短、复杂动作和对白长，总时长不变。动作放不下先在原剧情和合法边界内拆镜；边界不允许时报告冲突，不加速、吞台词或让未完成动作被切断。主动作须有可见完成标志，并在结束前完成，预留至少1秒自然收势、反应和剪辑余量（仍有呼吸和环境运动，不是定格）。2秒短镜只做简单观察或反应，不承担复杂动作。
每镜必须给出execution。setup_seconds可为0，action_seconds至少1，settle_seconds至少1；speech_seconds是说完本镜全部台词/旁白的预算，无台词为0。仅当台词能自然伴随动作时speech_overlap=true，此时总时长=setup_seconds+max(action_seconds,speech_seconds)+settle_seconds；否则总时长为四项相加。中文按每秒最多5个汉字/字母/数字作宽松上限，情绪、停顿、换人要更慢，角色名和标点不计。预算是创作估计，不是模型执行保证。
先审查整条动作链，再写每镜：上一镜的完成状态必须能通向下一镜首帧；同场景保留位置、朝向、视线、运动方向、光线及道具归属，不重复已完成动作，不瞬移、不把道具无因换手。时间/场景变化必须来自剧本，写明因果、离场/抵达或可读的视觉呼应，不要求不同场景无缝延续。bridge只用于解释承接，不得让本镜提前演下一镜的剧情。first_frame应和start_state一致，description不得与main_action/end_state或时间预算冲突。
输出前自查动作密度、台词预算、各镜收束和相邻状态；检查失败返回{"conflict":"具体镜头、原因及需用户调整的剧本或时长"}，不输出凑数镜头，不自动重试。
`

func scriptDirectionPrompt(p Project, cfg Settings) string {
	v := videoDurationPolicy(cfg)
	v.Min = max(2, v.Min) // Action and settling each need a second.
	return fmt.Sprintf(`你是短片编剧兼导演。写一部可实际拍摄的%d秒、%s中文短片，先完成总体剧本与资产清单，不生成分镜图片。
故事须有清楚的主角愿望、具体阻碍、由主角行动导致的变化与结尾回响；短时长减少支线，用可见行为推进，避免空泛抒情、突然成功、靠旁白解释一切或无因跳场。保留用户明确要求的事件与台词；要求确实放不下时返回{"conflict":"无法容纳的内容及建议调整"}，不擅自删掉必需情节。
先按动作和台词负担规划段落，再分配时间，不均分、不默认每段5秒，不为凑总长增加无关事件。剧本每段使用独占一行的【0—6秒｜地点·时间】格式（数字为示例），从0秒连续覆盖%d秒。各段必须能拆成%d～%d秒的镜头；短段只放一个可完成动作，长段保留拆镜空间，每个潜在镜头都留至少1秒收束。对白按自然语速和停顿预算，不超过每秒5个汉字/字母/数字，动作不能占满全部时间后再塞台词。
每段写清：开场状态、一个核心事件及完成标志、结束状态、与前段的因果和视觉承接。白天转夜晚、地点变化或时间省略必须有明确叙事依据和可读线索，避免人物/道具突然到位。上述说明不能包含未发生的新事件；不要在动作段落里混入另一场景。结尾完成主要事件并留情绪落点，不在动作中途结束。
开头简洁写角色固定外观及音色约束。对白独占一行，严格“角色名：台词”，角色名和资产名称一致；旁白模式不写角色对白。素材范围：角色1～3、场景1～3、道具0～2，名称具体，描述固定特征。
返回JSON {"script":"完整中文剧本","assets":[{"kind":"character|scene|prop","name":"名称","description":"具体固定外观或环境"}]}。
风格：%s
用户故事：%s
%s`, projectDuration(p), aspectLabel(p), projectDuration(p), v.Min, v.Max, p.Style, p.Idea, audioDirection(p))
}

func compileShotExecution(p Project, q Shot, e shotExecution) (Shot, error) {
	for _, text := range []string{e.StartState, e.MainAction, e.EndState, e.Bridge} {
		if strings.TrimSpace(text) == "" || len(text) > 6000 {
			return q, errors.New("缺少或超长的起始状态、主要动作、完成状态或前镜承接")
		}
	}
	for _, n := range []int{e.SetupSeconds, e.ActionSeconds, e.SpeechSeconds, e.SettleSeconds} {
		if n < 0 || n > q.Duration {
			return q, errors.New("动作或台词预算超出镜头时长")
		}
	}
	if e.ActionSeconds < 1 || e.SettleSeconds < 1 {
		return q, errors.New("主动作和完成后的收束各须至少1秒；请拆镜或调整剧本时长")
	}
	middle := e.ActionSeconds + e.SpeechSeconds
	if e.SpeechOverlap {
		middle = max(e.ActionSeconds, e.SpeechSeconds)
	}
	if e.SetupSeconds+middle+e.SettleSeconds != q.Duration {
		return q, errors.New("建立、动作/台词及收束预算之和必须等于本镜时长")
	}
	text := q.Narration
	if dialogueMode(p) {
		text = ""
		for _, line := range strings.Split(q.Narration, "\n") {
			_, words, _ := strings.Cut(strings.ReplaceAll(line, ":", "："), "：")
			text += words
		}
	}
	n := 0
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			n++
		}
	}
	if n > e.SpeechSeconds*5 || (n == 0 && e.SpeechSeconds != 0) {
		return q, errors.New("台词与发声预算不符；请为自然说完和结尾收束留时，不加速硬塞")
	}
	if err := validateShotContent(p, q); err != nil {
		return q, err
	}
	mode := "与动作分开"
	if e.SpeechOverlap {
		mode = "与动作自然重叠"
	}
	q.Description = fmt.Sprintf("前镜承接（背景说明，不额外演出）：%s\n起始状态：%s\n本镜唯一主动作：%s\n执行：%s\n节奏（本镜相对秒数）：建立%d秒；动作%d秒；台词/旁白%d秒（%s）；最后%d秒收束。\n切出前完成状态：%s。主动作须在收束前完成，最后保持自然反应和环境运动，不定格、不提前演下一镜。", e.Bridge, e.StartState, e.MainAction, q.Description, e.SetupSeconds, e.ActionSeconds, e.SpeechSeconds, mode, e.SettleSeconds, e.EndState)
	return q, validateShotContent(p, q)
}
