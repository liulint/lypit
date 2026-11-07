package main

// Active defaults are centralized here. Historical task builders are kept for receipt recovery.
var promptDefinitions = []promptDefinition{
	{ID: "analyze", Name: "剧本与资产分析", Group: "文字", Description: "从故事生成有因果、时间段和资产清单的总体剧本。", Default: `你是短片编剧兼导演。写一部可实际拍摄的{{duration}}秒、{{aspect}}中文短片，先完成总体剧本与资产清单，不生成分镜图片。
故事须有清楚的主角愿望、具体阻碍、由主角行动导致的变化与结尾回响；短时长减少支线，用可见行为推进，避免空泛抒情、突然成功、靠旁白解释一切或无因跳场。保留用户明确要求的事件与台词；要求确实放不下时返回{"conflict":"无法容纳的内容及建议调整"}，不擅自删掉必需情节。
先按动作和台词负担规划段落，再分配时间，不均分、不默认每段5秒，不为凑总长增加无关事件。剧本每段使用独占一行的【0—6秒｜地点·时间】格式（数字为示例），从0秒连续覆盖{{duration}}秒。各段必须能拆成{{min_duration}}～{{max_duration}}秒的镜头；短段只放一个可完成动作，长段保留拆镜空间，每个潜在镜头都留至少1秒收束。对白按自然语速和停顿预算，不超过每秒5个汉字/字母/数字，动作不能占满全部时间后再塞台词。
每段写清：开场状态、一个核心事件及完成标志、结束状态、与前段的因果和视觉承接。白天转夜晚、地点变化或时间省略必须有明确叙事依据和可读线索，避免人物/道具突然到位。上述说明不能包含未发生的新事件；不要在动作段落里混入另一场景。结尾完成主要事件并留情绪落点，不在动作中途结束。
开头简洁写角色固定外观及音色约束。对白独占一行，严格“角色名：台词”，角色名和资产名称一致；旁白模式不写角色对白。素材范围：角色1～3、场景1～3、道具0～2，名称具体，描述固定特征。
返回JSON {{output_contract}}。
风格：{{style}}
用户故事：{{idea}}
{{audio_direction}}`},
	{ID: "board_plan", Name: "分镜规划", Group: "文字", Description: "将确认后的剧本拆成可完成动作、前后承接的文字分镜。", Default: `你是执行分镜导演。总体剧本是唯一剧情依据，忠实拆解，不重新编剧。总时长{{duration}}秒，画幅{{aspect}}。不得固定镜头数或均分时长。有明确时间段时，严格保留各段边界、场景、事件顺序和台词原文；只允许在同一段内按动作拆成更多镜头，不可跨段合并。无时间段时按剧情顺序自然拆镜。单镜{{min_duration}}～{{max_duration}}秒，不得拆出短于下限的镜头；无法满足时应报告冲突，禁止自动重试。超长段落在原段内部拆镜，不删减事件。不得新增、遗漏或挪动剧情和角色。每镜source_text逐字复制所属完整时间段（含标题），无时间段则逐字引用对应原文。first_frame仅描述动作开始之前同一时空的一幅静态构图：人物位置、姿态、道具、光线、景别；禁止提前画动作结尾或将多个场景揉成一张图。description只写从该首帧开始按顺序演出的动作、运镜和声音。narration逐字保留本镜对白，不改写，不朗读角色名，无台词留空。仅用素材清单ID引用，角色音色及固定特征遵从总体剧本。返回JSON {{output_contract}}。这是文字草稿，完成后等待人工审核，不生成图片。
总体剧本：{{script}}
素材清单：{{assets}}
{{audio_direction}}
{{directing_rules}}
每镜增加：{{execution_schema}}`},
	{ID: "shot_rewrite", Name: "单镜改写", Group: "文字", Description: "按目标秒数重写当前镜头，保持相邻剧情。", Default: `你是短片导演。只重写当前镜头，输出严格适合{{target_duration}}秒的内容。允许完全重写首帧、动作和台词，保持既定人物身份、场景道具和前后剧情衔接，不新增引用素材，不修改其他镜头。动作应少而明确，台词需自然说完（每秒不超过5个汉字或字母）。首帧是静态画面，动作另写。返回JSON {{output_contract}}。当前镜头ID：{{shot_id}}；前后镜头：{{context_shots}}；总体剧本：{{script}}；素材：{{assets}}；{{audio_direction}}
{{directing_rules}}
增加：{{execution_schema}}`},
	{ID: "character_master", Name: "角色主图", Group: "图片", Description: "建立角色固定身份的正面全身主图。", Default: `{{style}}。角色正面全身主图，纯净背景。{{asset_description}}
{{image_contract}}`},
	{ID: "character_sheet", Name: "角色四视图", Group: "图片", Description: "以确认的角色主图生成一张2×2设定图，随后本地裁切。", Default: `{{style}}。以参考图人物为唯一身份依据，固定同一服装。{{asset_description}}
{{sheet_contract}}`},
	{ID: "asset_reference", Name: "场景 / 道具参考图", Group: "图片", Description: "场景和道具共用模板，根据素材类型生成独立参考图。", Default: `{{style}}。完整独立的{{asset_kind}}参考图。{{asset_description}}
{{image_contract}}`},
	{ID: "board_image", Name: "分镜图", Group: "图片", Description: "依据静态首帧和已确认素材生成当前镜头的一张图。", Default: `{{style}}。{{aspect}}电影分镜，严格保持参考人物身份、服装和场景。{{first_frame}}
{{image_contract}}`},
	{ID: "board_retry", Name: "分镜重新生成", Group: "图片", Description: "根据修改建议重新生成当前镜头，原图保留。", Default: `{{style}}。{{aspect}}电影分镜。保持参考角色身份、外观与场景，原分镜：{{first_frame}}。{{current_reference}}本次修改建议：{{feedback}}
{{image_contract}}`},
	{ID: "board_edit", Name: "基于当前图修改", Group: "图片", Description: "以当前图为唯一底图，明确修改内容与必须保留的内容。", Default: `编辑现有图片，只修改明确要求的部分。修改内容：{{changes}}。必须保留：{{preserve}}。
{{edit_contract}}`},
	{ID: "video", Name: "视频生成", Group: "视频", Description: "根据已确认画面、动作和台词生成视频；输入模式约束由系统补充。", Default: `总体风格与身份约束：{{continuity}}
静态首帧：{{first_frame}}
风格：{{style}}
从首帧开始演出，仅发生本镜动作：{{description}}
{{audio_instruction}}`},
}
