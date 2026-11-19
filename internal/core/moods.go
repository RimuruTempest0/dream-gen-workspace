package core

// ---- mood guide (0.7.26) -------------------------------------------------------
//
// Users could not say what they wanted, and the images had nothing to do with
// them. People recognise more easily than they describe, and a feeling is easier
// to point at as a metaphor (weather, time of day, distance) than as an
// adjective. So in the pro mode the user picks a weather, a time of day, a
// distance and an expectation, and the mood decides the image's atmosphere while
// the prompt still decides what is drawn.
//
// The mood is form state, not a prompt edit: each image of a submission is held
// (nothing sent) while the AI 模型 writes the user's prompt, the mood, an optional
// concept and the rendering into one prompt, then it is sent. Without an AI 模型
// the mood points are appended to the prompt instead.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"imageworkbench/internal/i18n"
)

// MoodOption is one choice of a guide step. Phrase builds the confirmation
// sentence and Short the label; Rules (what to draw) and Hint (the fallback) stay
// in the backend.
type MoodOption struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Short  string `json:"short"`
	Phrase string `json:"phrase"`
	Rules  string `json:"-"`
	Hint   string `json:"-"`
}

type MoodLibrary struct {
	Weather     []MoodOption `json:"weather"`
	Time        []MoodOption `json:"time"`
	Distance    []MoodOption `json:"distance"`
	Expectation []MoodOption `json:"expectation"`
	Renderings  []MoodOption `json:"renderings"`
	Nudges      []MoodOption `json:"nudges"`
}

// MoodChoice is the mood the user picked (option IDs).
type MoodChoice struct {
	Weather     string `json:"weather"`
	Time        string `json:"time"`
	Distance    string `json:"distance"`
	Expectation string `json:"expectation"`
}

// ConceptChoice is a concept picked as form state while a mood is set: it joins
// the mood in the one rewrite at generation time instead of editing the prompt.
type ConceptChoice struct {
	Form     string `json:"form"`
	Stance   string `json:"stance"`
	Strength int    `json:"strength"`
}

// Rules only describe what can be seen, with the wording rule of
// builtin_skills/image-prompting.md section 6: no artists, real places or brands.
var moodWeather = []MoodOption{
	{ID: "clear", Name: "晴", Short: "晴", Phrase: "天很晴", Hint: "晴朗通透，阴影边缘清楚", Rules: `明暗：整体明亮，亮部干净，阴影边缘清楚
色彩：饱和度中等偏高，天空是干净的蓝
对比：明暗对比清楚但不刺眼
动静：画面安定，只有树叶、水面这类轻微的动
空气：通透，没有雾气和水汽，远处的东西也看得清`},
	{ID: "thin-cloud", Name: "薄云", Short: "薄云", Phrase: "飘着一层薄云", Hint: "光被薄云滤过，阴影浅而柔", Rules: `明暗：阳光被一层薄云滤过，亮度中等，阴影浅、边缘柔
色彩：饱和度中等，颜色柔和，天空是带白的浅蓝
对比：偏低，亮部不刺眼
动静：云缓慢移动，画面平静
空气：略带白，远景比近景稍淡`},
	{ID: "overcast", Name: "阴", Short: "阴", Phrase: "阴着天", Hint: "灰白云层盖满天空，几乎没有影子", Rules: `明暗：平而灰的天光从整片云层透下，几乎看不到影子
色彩：饱和度低，所有颜色都蒙着一层灰
对比：低，亮部和暗部挨得很近
动静：天空是一整块灰白的云，没有缝隙；画面静止
空气：发闷，远处发灰`},
	{ID: "drizzle", Name: "细雨", Short: "细雨", Phrase: "下着细雨", Hint: "细雨，地面湿亮有反光，空气里有水汽", Rules: `明暗：天光偏暗，没有直射的阳光
色彩：偏冷，饱和度低；湿的表面颜色更深
对比：中等偏低，亮处主要来自湿地面和玻璃上的反光
动静：细密的雨丝斜斜落下，水洼里有雨点打出的小圈，窗玻璃上挂着水珠
空气：有水汽，远景发白、轮廓变软`},
	{ID: "fog", Name: "大雾", Short: "大雾", Phrase: "起了大雾", Hint: "浓雾，近处清楚，稍远只剩轮廓", Rules: `明暗：光是散的，没有方向，也没有影子
色彩：颜色几乎褪成灰白，只有近处留一点本色
对比：很低
动静：画面安静，没有动势
空气：浓雾；近处的东西清楚，几步之外只剩轮廓，再远完全消失在白里`},
	{ID: "storm", Name: "雷暴", Short: "雷暴", Phrase: "雷雨交加", Hint: "压低的黑灰厚云，强风大雨，局部被闪电照亮", Rules: `明暗：天空压得很低，大面积很暗，局部被一道闪电或一束光照亮
色彩：偏冷的蓝灰，带一点青，饱和度低
对比：强，亮处与暗处反差大
动静：动势强烈：大风吹弯树和草，雨斜着打下，水面起浪
空气：雨幕让远处模糊，厚重的黑灰色云层翻滚`},
	{ID: "snow", Name: "雪", Short: "雪", Phrase: "下着雪", Hint: "雪覆盖地面，光柔而冷，影子浅蓝灰", Rules: `明暗：大面积的白，光柔和，影子是浅蓝灰
色彩：饱和度很低，只剩少数几处颜色
对比：低到中等，白里有细微的层次
动静：雪花缓慢飘落，或刚停；画面很静
空气：远景被飘雪淡化，地面和物体顶上覆着一层雪`},
}

var moodTime = []MoodOption{
	{ID: "dawn", Name: "清晨", Short: "清晨", Phrase: "清晨", Hint: "清晨低角度的柔光，色调偏冷带淡金", Rules: `光的方向：太阳很低，光从一侧斜着照进来，很柔
冷暖：整体偏冷，地平线附近有一点淡粉或淡金
影子：很长、很淡
人造光：没有，或只剩零星几盏还没关的灯
其他：地面可能有露水，低处有一层薄薄的雾气`},
	{ID: "noon", Name: "正午", Short: "正午", Phrase: "正午", Hint: "正午顶光，影子短而硬", Rules: `光的方向：光从头顶直射下来
冷暖：中性偏白
影子：很短，落在物体正下方，边缘硬
人造光：没有
其他：物体顶面最亮，侧面和下方较暗`},
	{ID: "dusk", Name: "黄昏", Short: "黄昏", Phrase: "黄昏", Hint: "黄昏低角度暖光，天空由橙到紫，远处亮起零星灯光", Rules: `光的方向：太阳贴近地平线，低角度侧光或逆光
冷暖：亮部暖橙，天空由橙过渡到紫，暗部开始转冷
影子：很长
人造光：远处开始亮起少量窗灯或路灯
其他：物体边缘有一圈暖色的轮廓光`},
	{ID: "night", Name: "深夜", Short: "深夜", Phrase: "深夜", Hint: "深夜，没有天光，只有零星的灯", Rules: `光的方向：没有阳光；光来自一小块一小块的人造光：路灯、窗里的灯、车灯（不要带文字的招牌）；晴天可以有月光和星星
冷暖：灯光暖黄，夜色冷蓝，两者并存
影子：灯下的影子短而集中，其余地方大面积处在暗里
人造光：有，是主要光源，但数量少
其他：暗部保留细节，不要一片死黑`},
}

var moodDistance = []MoodOption{
	{ID: "far", Name: "很远地看", Short: "很远", Phrase: "远远地看着它", Hint: "远景，主体很小，大片留白", Rules: `景别：远景或大全景
主体大小：主体只占画面很小一部分（约十分之一以内）
留白：大片留给天空、地面或水面
人：如果有人，只是很小的身影，看不清面孔
景深：深，从近到远都比较清楚`},
	{ID: "within", Name: "站在其中", Short: "其中", Phrase: "站在它中间", Hint: "人眼高度的中景，像站在场景里", Rules: `景别：中景，人眼高度的视角，是站在这个场景里看到的画面（不出现观看者的身体）
主体大小：主体约占画面三分之一
留白：不多，四周的环境包围着观看者；前景里有近处的东西（栏杆、草、门框、桌沿）
人：可以有一两个人，不看镜头
景深：中等，前景略虚`},
	{ID: "close", Name: "贴得很近", Short: "很近", Phrase: "贴得很近", Hint: "近景特写，背景虚化", Rules: `景别：近景或特写
主体大小：主体或主体的一个局部占满大半画面
留白：几乎没有
人：画面里没有人
景深：浅，背景虚成色块和光斑；看得清材质、水珠和纹理`},
}

// The expectations must give clearly different images: resonance draws the
// weather as it is, soothing keeps it but softens it and offers a place to stay,
// lifting keeps it and opens one way out. Lifting never turns into the opposite weather.
var moodExpectation = []MoodOption{
	{ID: "resonate", Name: "陪我待一会儿", Short: "待一会儿", Phrase: "陪你待一会儿", Hint: "照实画，不美化，不加转折", Rules: `照实画：完全按上面的天气和时刻画，光线、颜色和明暗都不调亮、不美化、不加滤镜感。
不加任何转折的元素：不要云缝里的光、彩虹、变亮的天边、飞鸟、花开、远处的暖灯这类“好起来”的暗示。
画面里放一个和观看者处境相同的东西——一把空着的椅子、一个独自站着的身影、一盏同样被雨打着的路灯、一只停着的船——它和观看者一起待在这个天气里，没有被照顾，也没有被拯救。
构图平稳，不制造戏剧性。`},
	{ID: "soothe", Name: "让我缓一缓", Short: "缓一缓", Phrase: "帮你缓一缓", Hint: "同样的天气但更柔，留一处可以待的暖处", Rules: `天气和时刻不变，但整体对比降低一档，暗部提亮一点，边缘放软，颜色往暖、往柔收一点。
画面里必须有一处“可以待的地方”，清楚可见：一扇亮着暖光的窗、屋檐下没被雨淋到的一块干地、一处挡风的角落、一张搭着毯子的椅子、伞下的一小块地方。它比周围更暖、更亮、更安稳，只占画面一小块，周围仍是原来的天气。
没有强烈的动势：风变小，雨变细，水面平静。
不要把天气改成晴天，不要画成阳光普照。`},
	{ID: "lift", Name: "拉我一把", Short: "拉一把", Phrase: "拉你一把", Hint: "同样的天气里给一个出口，不要改成晴天", Rules: `从同样的天气和时刻出发：画面大部分仍是原来的天气，云、雨、雾、暗都保留。
在画面里给一个“出口”，只有一个，看得见但很小，只占画面很小的一部分，而且要合乎这个时刻：白天是云层裂开的一道缝里露出的亮、远处地平线上正在变亮的一条亮带、雾里前方渐渐清楚起来的一段路；夜里是远处一扇亮着的门或窗、路尽头的一盏灯、云缝里露出的一小片月色或几颗星。出口在画面的远处或上方。不要舞台追光式的光柱，不要从天而降的光束。
天气本来就晴朗时，出口是一条通向远处的路、一扇打开的门或窗、看得见的地平线，画面有向外的开阔感。
画面有方向感：路、河、光线或人的朝向都指向这个出口；如果有人，是朝出口走去或抬头看向它，而不是背对它。
不要改成相反的天气，不要画成晴朗明亮的画面，不要彩虹，不要大面积蓝天和阳光；出口之外的部分保持原来的暗和湿。`},
}

const (
	renderingRealistic = "realistic"
	renderingPainterly = "painterly"
)

var moodRenderings = []MoodOption{
	{ID: renderingRealistic, Name: "写实", Short: "写实", Phrase: "像照片", Hint: "像照片：真实镜头，自然颗粒", Rules: `像一张照片：真实的镜头视角和透视，自然的景深和虚化，轻微的胶片或传感器颗粒，真实的材质和光的衰减。
不要插画感，不要描边，不要过度锐化或电脑渲染的质感。`},
	{ID: renderingPainterly, Name: "写意", Short: "写意", Phrase: "像画", Hint: "像画：看得见笔触，形体概括", Rules: `像一幅画，不像照片：看得见笔触或刷痕，形体被概括，细节有取舍；可以是油画、水彩、水墨或版画式的概括，颜色成块。
不要照片式的真实质感，不要镜头光斑，不要相机式的景深虚化。`},
}

var moodNudges = []MoodOption{
	{ID: "quieter", Name: "更安静", Short: "更安静", Rules: "让画面更安静：动势变小（风、雨、波浪都减弱），物件和细节少一些，对比和饱和度降低一点。"},
	{ID: "stronger", Name: "更有力量", Short: "更有力量", Rules: "让画面更有力量：明暗对比和动势加强，光更集中，构图的张力更大。"},
	{ID: "brighter", Name: "更亮", Short: "更亮", Rules: "整体提亮一档：曝光提高，暗部更亮；天气和时刻不变。"},
	{ID: "darker", Name: "更暗", Short: "更暗", Rules: "整体压暗一档：曝光降低，暗部更深，亮处更集中；天气和时刻不变。"},
	{ID: "closer", Name: "更近", Short: "更近", Rules: "镜头往前推近一步：主体在画面里更大，四周的环境少一些，背景更虚。"},
	{ID: "farther", Name: "更远", Short: "更远", Rules: "镜头往后退一步：主体在画面里更小，露出更多周围的环境和天空。"},
}

// ListMoods returns the built-in mood library (compiled in, not stored).
func (s *Service) ListMoods() MoodLibrary {
	return MoodLibrary{
		Weather:     append([]MoodOption{}, moodWeather...),
		Time:        append([]MoodOption{}, moodTime...),
		Distance:    append([]MoodOption{}, moodDistance...),
		Expectation: append([]MoodOption{}, moodExpectation...),
		Renderings:  append([]MoodOption{}, moodRenderings...),
		Nudges:      append([]MoodOption{}, moodNudges...),
	}
}

func findMood(list []MoodOption, id string) (MoodOption, bool) {
	for _, o := range list {
		if o.ID == id {
			return o, true
		}
	}
	return MoodOption{}, false
}

// moodParts resolves a choice to its four options, in guide order.
type moodParts struct{ weather, time, distance, expectation MoodOption }

func resolveMood(m MoodChoice) (moodParts, error) {
	var p moodParts
	var ok [4]bool
	p.weather, ok[0] = findMood(moodWeather, m.Weather)
	p.time, ok[1] = findMood(moodTime, m.Time)
	p.distance, ok[2] = findMood(moodDistance, m.Distance)
	p.expectation, ok[3] = findMood(moodExpectation, m.Expectation)
	for _, v := range ok {
		if !v {
			return p, errors.New("心境选项无效，请重新选择心境")
		}
	}
	return p, nil
}

type conceptParts struct {
	form, stance Concept
	strength     int
}

func resolveConceptChoice(c *ConceptChoice) (*conceptParts, error) {
	if c == nil {
		return nil, nil
	}
	form, ok1 := findConcept(conceptForms, c.Form)
	stance, ok2 := findConcept(conceptStances, c.Stance)
	if !ok1 || !ok2 {
		return nil, errors.New("所选概念不存在，请重新选择")
	}
	if c.Strength < 0 || c.Strength >= len(conceptStrengths) {
		return nil, errors.New("概念强度无效")
	}
	return &conceptParts{form, stance, c.Strength}, nil
}

// moodLabel is the short label shown on results: "细雨 · 深夜 · 很远 · 缓一缓".
func moodLabel(m MoodChoice) string {
	p, e := resolveMood(m)
	if e != nil {
		return ""
	}
	return strings.Join([]string{p.weather.Short, p.time.Short, p.distance.Short, p.expectation.Short}, " · ")
}

const (
	rewordMood = "mood"
	moodStage  = "按心境改写提示词"
)

const moodInstruction = `你要为一张图写一条生图提示词。用户挑了天气、时刻、远近和期待，它们决定画面的气氛；画什么由用户的提示词决定。下面的规则写的都是看得见的画面决定。
要求：
1. 把规则转成具体的画面描述写进提示词：天气、光线、明暗、冷暖、远近、构图都要落到看得见的东西上。只写看得见的东西，不要写感受类的形容词，也不要出现“心境”“情绪”“治愈”“孤独”“希望”这类词；不要写艺术家、真实地点或品牌的名字。
2. 保留用户原提示词里的主体、数量、商品细节、尺寸数字和明确要求；引号里的画面文字连同引号逐字保留，不翻译、不改大小写；不要给画面添加用户没有写的文字、符号或图案；原提示词有画面文字时，在提示词里写明除这些文字外不出现其他文字、符号或表情图案。
3. “期待”一项最重要，必须照它的规则画，让人一眼能看出来；它和天气、时刻的规则一起成立，不能为了它改掉天气和时刻。提示词里不要写观看者自己，也不要写任何人的手、手臂或身体动作；这一条照做即可，不要在提示词里用“不出现……”的否定句把它们写出来。
4. %s
5. 画法：%s
%s%d. 只输出写好的提示词，不要解释、标题或代码块，也不要用引号把整条提示词包起来。

天气：%s
%s

时刻：%s
%s

远近：%s
%s

期待：%s
%s
%s
%s`

const moodNoRendering = "不限，按主体和上面的规则选择最合适的表现方式。"

// moodRequest builds the one rewriting request for an image: the user's prompt,
// the mood, an optional concept and the rendering together.
func moodRequest(base string, m moodParts, concept *conceptParts, rendering string, hasReferences bool) string {
	refs := conceptNoRefsNote
	if hasReferences {
		refs = "用户带了参考图：商品/主体的外观（造型、颜色、图案、文字、比例）以参考图为准，天气、时刻、远近和期待只作用于场景、光线和构图，不要改动主体本身。"
	}
	render := moodNoRendering
	renderBlock := ""
	if r, ok := findMood(moodRenderings, rendering); ok {
		render = r.Name + "——照下面“画法”的规则画，这一点必须一眼能看出来。"
		renderBlock = "\n画法：" + r.Name + "\n" + r.Rules + "\n"
	}
	extra, last, conceptBlock := "", 6, ""
	if concept != nil {
		extra = fmt.Sprintf("6. 还有一个画面概念（一个建筑形体和一个艺术立场）：概念管形体、材质和画法倾向，天气、时刻、远近和期待管光线、冷暖和远近；两者冲突时，光线、天气、冷暖和远近按这里的规则，形体和材质按概念。不要只罗列“%s”“%s”这类名称。概念强度：%s\n",
			concept.form.Name, concept.stance.Name, conceptStrengths[concept.strength])
		last = 7
		conceptBlock = fmt.Sprintf("\n形体：%s（%s）——%s\n%s\n\n立场：%s（%s）——%s\n%s\n",
			concept.form.Name, concept.form.En, concept.form.Look, concept.form.Rules,
			concept.stance.Name, concept.stance.En, concept.stance.Look, concept.stance.Rules)
	}
	prompt := "用户原提示词：\n" + base
	if strings.TrimSpace(base) == "" {
		prompt = "用户没有写提示词：请按上面的天气、时刻和远近自拟一个画面主体（一处风景、一个场景或一件日常的东西），选最能体现这些规则的场景，写出完整提示词。不要加入商品，不要加入画面文字。"
	}
	return fmt.Sprintf(moodInstruction, refs, render, extra, last,
		m.weather.Name, m.weather.Rules, m.time.Name, m.time.Rules,
		m.distance.Name, m.distance.Rules, m.expectation.Hint, m.expectation.Rules,
		renderBlock+conceptBlock, prompt)
}

// moodFallback appends the mood points when no AI 模型 can rewrite the prompt.
func moodFallback(base string, m moodParts, concept *conceptParts, rendering string) string {
	points := []string{m.weather.Hint, m.time.Hint, m.distance.Hint, m.expectation.Hint}
	if r, ok := findMood(moodRenderings, rendering); ok {
		points = append(points, r.Hint)
	}
	text := "画面气氛：" + strings.Join(points, "；") + "。"
	base = strings.TrimSpace(base)
	if base == "" {
		text = "一处与下面气氛相符的风景，不要商品，不要文字。\n" + text
	}
	if concept != nil {
		text = conceptFallback(text, concept.form, concept.stance, true)
	}
	if base == "" {
		return text
	}
	return base + "\n" + text
}

// moodCheck guards a mood rewrite like a concept rewrite: every quoted in-image
// text of the user's prompt survives, and the result is not overly long.
func moodCheck(base string) func(string) error {
	quoted := []string{}
	for _, q := range quotedText.FindAllStringSubmatch(base, -1) {
		quoted = append(quoted, q[1]+q[2])
	}
	return func(text string) error {
		for _, want := range quoted {
			if !strings.Contains(text, want) {
				return fmt.Errorf("改写结果没有保留画面文字“%s”", want)
			}
		}
		if len(text) > conceptPromptLimit || utf8.RuneCountInString(text) > 6000 {
			return errors.New("改写结果过长")
		}
		return nil
	}
}

// moodRewrite writes a held mood task's prompt with the AI 模型.
func (s *Service) moodRewrite(ctx context.Context, c Connection, key string, t Task) (string, error) {
	if t.MoodChoice == nil {
		return "", errors.New("任务没有心境")
	}
	m, e := resolveMood(*t.MoodChoice)
	if e != nil {
		return "", e
	}
	concept, e := resolveConceptChoice(t.ConceptChoice)
	if e != nil {
		return "", e
	}
	return s.llmRewrite(ctx, c, key, withPromptLanguage(t.Language, moodRequest(t.BasePrompt, m, concept, t.Rendering, len(t.References) > 0)), moodStage, moodCheck(t.BasePrompt))
}

// moodLLMLocked picks the AI 模型 line for a mood rewrite with the concept
// stage's licence rule: a trial only uses the main line.
func (s *Service) moodLLMLocked(t Task) (Connection, string, bool) {
	c, key, ok := s.rewordLLMLocked(t)
	if ok && s.trialLocked() && !mainLine(c) {
		return Connection{}, "", false
	}
	return c, key, ok
}

// prepareMoodTask turns a task built from the user's prompt into a mood task:
// held for the rewrite when an AI 模型 can write it, otherwise with the points
// appended.
func (s *Service) prepareMoodTask(t *Task, m moodParts, concept *conceptParts, llm bool) {
	t.BasePrompt = t.Prompt
	t.Mood = moodLabel(*t.MoodChoice)
	if concept != nil {
		t.Concept = concept.form.Name + " × " + concept.stance.Name
	}
	if llm {
		t.Rewording, t.RewordFor = true, rewordMood
		return
	}
	t.Prompt = moodFallback(t.Prompt, m, concept, t.Rendering)
}

// ---- guide mode ----------------------------------------------------------------

const guideModePrefKey = "guide-mode"

// GetGuideMode tells whether the mood guide is shown in the pro mode (off by default).
func (s *Service) GetGuideMode() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	var v bool
	if s.store.Get("preferences", guideModePrefKey, &v) != nil {
		return false
	}
	return v
}

func (s *Service) SetGuideMode(on bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.store.Put("preferences", guideModePrefKey, on)
}

// ---- 再靠近一点 ------------------------------------------------------------------

type NudgeInput struct {
	RequestID string `json:"requestID"`
	TaskID    string `json:"taskID"`
	AssetID   string `json:"assetID"`
	Direction string `json:"direction"`
}

const nudgePromptMarker = "\n\n原图提示词（只用于保持没有要求修改的部分，不要重新构图）：\n"

// nudgePrompt changes one thing of the image and keeps the rest. It is a fixed
// template, so no AI 模型 is involved.
func nudgePrompt(base, rule string) string {
	for _, marker := range []string{nudgePromptMarker, nudgePromptMarkerEn} {
		if _, rest, ok := strings.Cut(base, marker); ok {
			base = rest
		}
	}
	return "以提供的第 1 张图片为底图修改，只改这一点：" + rule +
		"\n除这一点直接带来的变化外，其余保持一致：主体、构图、天气、时刻、颜色倾向、画法和画面里的文字都与底图相同。" +
		nudgePromptMarker + base
}

// ---- 再靠近一点 in an English 生图提示词 (0.8.0) ----------------------------------

const nudgePromptMarkerEn = "\n\nOriginal image prompt (only for keeping what is not asked to change; do not recompose):\n"

// nudgeRulesEn are the nudge rules for a non-Chinese interface, whose 生图提示词
// are English.
var nudgeRulesEn = map[string]string{
	"quieter":  "Make the image quieter: less motion (weaker wind, rain and waves), fewer objects and details, slightly lower contrast and saturation.",
	"stronger": "Make the image more powerful: stronger contrast between light and dark and more motion, more concentrated light, more tension in the composition.",
	"brighter": "Brighten the whole image by one step: higher exposure, brighter shadows; keep the weather and the time of day.",
	"darker":   "Darken the whole image by one step: lower exposure, deeper shadows, more concentrated highlights; keep the weather and the time of day.",
	"closer":   "Move the camera one step closer: the subject is larger in the frame, less of the surroundings, a softer background.",
	"farther":  "Move the camera one step back: the subject is smaller in the frame, showing more of the surroundings and the sky.",
}

// nudgePromptIn is nudgePrompt in the 生图提示词 language of lang.
func nudgePromptIn(lang, base string, nudge MoodOption) string {
	rule, ok := nudgeRulesEn[nudge.ID]
	if promptLanguage(lang) == i18n.ZhCN || !ok {
		return nudgePrompt(base, nudge.Rules)
	}
	for _, marker := range []string{nudgePromptMarker, nudgePromptMarkerEn} {
		if _, rest, ok := strings.Cut(base, marker); ok {
			base = rest
		}
	}
	return "Edit the first image provided, changing only this: " + rule +
		"\nApart from what this change directly causes, keep everything the same as the base image: subject, composition, weather, time of day, colour tendency, rendering and any text in the image." +
		nudgePromptMarkerEn + base
}

// NudgeMoodImage queues one image that moves a finished mood image one step in a
// direction (quieter, brighter, closer …). It is a new, possibly billable
// request: the caller confirms it and the request ID makes repeated clicks idempotent.
func (s *Service) NudgeMoodImage(in NudgeInput) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if in.RequestID == "" || len(in.RequestID) > 128 {
		return "", errors.New("请求标识无效")
	}
	nudge, ok := findMood(moodNudges, in.Direction)
	if !ok {
		return "", errors.New("请选择一个方向")
	}
	var submitted string
	if s.store.db.QueryRow(`SELECT batch FROM submissions WHERE id=?`, in.RequestID).Scan(&submitted) == nil {
		return submitted, nil
	}
	var source Task
	if e := s.store.Get("task", in.TaskID, &source); e != nil {
		return "", errors.New("找不到这张图片的任务")
	}
	if source.MoodChoice == nil || source.AgentRunID != "" {
		return "", errors.New("只有按心境生成的图可以再靠近一点")
	}
	if source.Status != "succeeded" {
		return "", errors.New("只能对已完成的图片再靠近一点")
	}
	owned := false
	for _, id := range source.AssetIDs {
		owned = owned || id == in.AssetID
	}
	if !owned {
		return "", errors.New("图片不属于这个任务")
	}
	var asset Asset
	if e := s.store.Get("asset", in.AssetID, &asset); e != nil || asset.TaskID != source.ID {
		return "", errors.New("原图不可用，请刷新后重试")
	}
	var c Connection
	if e := s.store.Get("connection", source.Connection.ID, &c); e != nil {
		return "", errors.New("原图片服务已删除，请重新配置后再试")
	}
	if normalizeConnection(c).Kind != "image" {
		return "", errors.New("请选择图片服务")
	}
	if _, e := s.key(c.Credential); e != nil {
		return "", e
	}
	t := freshAttempt(source, c)
	t.References = []string{in.AssetID}
	t.Language = s.Language() // a new request: the language chosen now
	t.Prompt = nudgePromptIn(t.Language, source.Prompt, nudge)
	t.NudgeOf, t.Nudge = source.ID, nudge.Name
	if e := s.ensureCanGenerateLocked(1, c.ID); e != nil {
		return "", e
	}
	t.Trial = s.trialLocked()
	batch, e := s.store.Submit(in.RequestID, source.BatchID, []Task{t})
	if e != nil {
		return "", e
	}
	_ = s.store.appendLog("info", "image", source.BatchID, "已提交“再靠近一点”："+nudge.Name+"（基于已完成图片，原图保留）")
	s.signal()
	return batch, nil
}

// freshAttempt copies t as a new, unsent task on connection c; the mood, concept
// and rendering go along.
func freshAttempt(t Task, c Connection) Task {
	t.ID = NewID()
	t.CreatedAt = Now()
	t.UpdatedAt = t.CreatedAt
	t.Status = "queued"
	t.Error = ""
	t.Results = nil
	t.AssetIDs = []string{}
	t.Diagnostic = GenerationDiagnostic{}
	t.PreviousAssetIDs = nil
	t.RoutedFrom, t.RoutedFromName, t.RouteReason = "", "", ""
	t.AutoRetryOf, t.AutoRetriedAs, t.AutoDownloads, t.DownloadRetryAt = "", "", 0, ""
	t.PolicyRetryOf, t.PolicyRetriedAs, t.PolicyStep, t.Rewording, t.RewordFor = "", "", 0, false, ""
	t.NudgeOf, t.Nudge = "", ""
	t.Connection = c
	return t
}

// retryMoodLocked regenerates a mood image as a new submission: a nudge resends
// its prompt; any other image is written again from the user's prompt and mood.
func (s *Service) retryMoodLocked(source Task) (string, error) {
	var c Connection
	if e := s.store.Get("connection", source.Connection.ID, &c); e != nil {
		return "", missing(e, "这张图片使用的图片服务已删除")
	}
	if normalizeConnection(c).Kind != "image" {
		return "", errors.New("请选择图片服务")
	}
	if _, e := s.key(c.Credential); e != nil {
		return "", e
	}
	t := freshAttempt(source, c)
	t.BatchID = NewID()
	t.Language = s.Language() // a new request: the language chosen now
	if source.NudgeOf != "" {
		t.NudgeOf, t.Nudge = source.NudgeOf, source.Nudge
	} else {
		m, e := resolveMood(*source.MoodChoice)
		if e != nil {
			return "", e
		}
		concept, e := resolveConceptChoice(source.ConceptChoice)
		if e != nil {
			return "", e
		}
		if msg := s.licenseBlockMessageLocked(); msg != "" {
			return "", errors.New(msg)
		}
		t.Prompt = source.BasePrompt
		_, _, llm := s.moodLLMLocked(t)
		s.prepareMoodTask(&t, m, concept, llm)
	}
	if e := s.ensureCanGenerateLocked(1, c.ID); e != nil {
		return "", e
	}
	t.Trial = s.trialLocked()
	batch, e := s.store.Submit(NewID(), t.BatchID, []Task{t})
	if e != nil {
		return "", e
	}
	if t.Rewording {
		s.startRewordLocked(t.ID)
	}
	s.signal()
	return batch, nil
}
