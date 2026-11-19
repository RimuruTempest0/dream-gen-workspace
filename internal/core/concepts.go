package core

// ---- concept stage (0.7.18) ---------------------------------------------------
//
// The workbench's concept stage pairs an architectural form with one to three
// artistic stances. Appending "Brutalism" to a prompt barely changes an image, so
// every concept is a card of visible decisions (scale, material, light,
// composition, colour, what to avoid) and the configured LLM writes those
// decisions into the user's prompt. Rewriting generates no image; the user still
// presses "生成" as usual. Without an AI 模型 the card points are appended as they are.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"
)

// Concept is one card. Rules are the visible decisions for the LLM and the
// fallback; they are not sent to the frontend.
type Concept struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	En    string `json:"en"`
	Group string `json:"group"`
	Look  string `json:"look"`
	Rules string `json:"-"`
}

// ConceptPick is a curated pairing of one form and one stance (by ID).
type ConceptPick struct {
	Name   string `json:"name"`
	Form   string `json:"form"`
	Stance string `json:"stance"`
}

type ConceptLibrary struct {
	Forms   []Concept     `json:"forms"`
	Stances []Concept     `json:"stances"`
	Picks   []ConceptPick `json:"picks"`
}

type ConceptInput struct {
	Prompt  string   `json:"prompt"`
	Form    string   `json:"form"`
	Stances []string `json:"stances"`
	// Strength is how much the concept takes over: 0 accent, 1 balanced, 2 dominant.
	Strength      int  `json:"strength"`
	HasReferences bool `json:"hasReferences"`
}

// ConceptResult holds one prompt per stance; Labels name them ("巨物 × 浪漫主义").
type ConceptResult struct {
	Prompts []string `json:"prompts"`
	Labels  []string `json:"labels"`
	UsedLLM bool     `json:"usedLLM"`
}

// Rules only describe what can be seen: no living artists, real buildings or
// brands (the same wording rule as builtin_skills/image-prompting.md section 6).
var conceptForms = []Concept{
	{ID: "beton", Name: "砼核", En: "Béton brut", Group: "重", Look: "粗砺的清水混凝土体量，模板痕迹清晰", Rules: `尺度：建筑级的厚重体量，墙体厚实，开口少而深
材质：清水混凝土，木模板留下的横向纹理、孔洞和接缝清晰可见
光线：低角度侧光扫过墙面，让粗糙肌理投下细小阴影
构图：大块实墙占据画面，主体靠在或立在混凝土体块前
色彩：混凝土的冷灰为主，主体是画面里唯一的暖色或亮色
避免：抛光的光滑表面、装饰线脚、鲜艳涂装`},
	{ID: "mega", Name: "巨物", En: "Megastructure", Group: "重", Look: "远超人体尺度的巨构，占满大半画面", Rules: `尺度：结构远超人体，人或主体只占画面很小一部分，用来衬出尺度
材质：重复的巨型构件——高墙、巨柱、桥架，表面有风化痕迹
光线：大气透视，远处的结构在薄雾里逐渐变淡
构图：低机位仰视或远景，巨构占满大半画面，主体放在前景或底部
色彩：灰蓝与石色的大面积，主体用一小块高对比色点出
避免：可识别的真实地标、拥挤的城市杂物、卡通式夸张`},
	{ID: "monolith", Name: "磐石", En: "Monolith", Group: "重", Look: "一整块未经雕饰的独石，沉默而完整", Rules: `尺度：一个完整的独立体块，比主体大几倍，四周留出空地
材质：一整块石材或实心体，只有自然的纹理和细微缺口
光线：单一方向的光，体块一面亮一面暗，轮廓清楚
构图：体块居中或略偏，地平线低，四周大量留白
色彩：石色、沙色或深灰，背景干净单一
避免：雕刻花纹、铭文、多个体块并列`},
	{ID: "levitation", Name: "悬浮", En: "Levitation", Group: "轻", Look: "沉重的体块离地悬空，底下留出阴影", Rules: `尺度：沉重的体块（或承托主体的台面）离地悬空，下方留出清楚的空隙
材质：看起来很重的材料——石、混凝土、金属块，强化轻与重的反差
光线：顶光或高侧光，在地面投下一块边缘清楚、与物体分离的阴影
构图：物体与阴影之间的空隙是画面重点，地面平整干净
色彩：中性色背景，阴影是画面里最深的颜色
避免：可见的支撑或吊线、魔法光效、四处漂浮的碎屑`},
	{ID: "shell", Name: "薄壳", En: "Thin shell", Group: "轻", Look: "连续起伏的薄曲面，像被风撑起", Rules: `尺度：大跨度的薄曲面屋顶或罩面，边缘极薄
材质：白色混凝土或织物般的连续曲面，看不到梁柱
光线：柔和的漫射光沿曲面形成平缓的明暗过渡
构图：曲面的边缘线横穿画面，主体在曲面之下或之前
色彩：白、浅灰与天空色，整体明亮
避免：直角盒子、厚重屋檐、繁杂的结构杆件`},
	{ID: "glass", Name: "玻璃盒", En: "Glass pavilion", Group: "轻", Look: "近乎消失的透明盒子，映着周围环境", Rules: `尺度：一个近乎透明的方盒，框架纤细，主体在盒内或盒前
材质：大块玻璃、细金属边框、平整的地面
光线：自然日光，玻璃上叠着周围树木和天空的反射与透射
构图：正面或两点透视，盒子边线干净，四周是自然环境
色彩：环境的绿或天色透过玻璃，框架是细的深色线
避免：脏污的反光、粗厚窗框、贴膜或文字标识`},
	{ID: "vault", Name: "穹顶", En: "Vault", Group: "空", Look: "高耸的拱顶之下，一束光从顶部落下", Rules: `尺度：高耸的拱顶内部空间，人或主体远小于空间
材质：石或抹灰的拱面，平整而略带岁月痕迹
光线：一束光从顶部开口落下，形成可见的光柱，其余部分较暗
构图：中轴对称，主体位于光柱的落点，视线被引向上方
色彩：暖灰与深色阴影，光柱处是最亮的暖白
避免：宗教符号、彩色玻璃图案、吊灯和繁复装饰`},
	{ID: "courtyard", Name: "天井", En: "Courtyard void", Group: "空", Look: "四面围合、向天空敞开的方形空洞", Rules: `尺度：四面围合的方形空洞，墙高于主体，顶部向天空敞开
材质：素墙、石地面，墙上被光影切出几何形
光线：天光从上方洒入，墙面一半亮一半暗
构图：俯视院子，或从院中仰望方框里的天空，主体在院子中央
色彩：墙面的浅色与一块天空的蓝，主体的颜色落在中心
避免：杂乱的盆栽、堆放的家具、密布的窗户`},
	{ID: "labyrinth", Name: "迷宫", En: "Labyrinth", Group: "空", Look: "层层叠叠没有尽头的阶梯与拱廊", Rules: `尺度：层层叠叠的阶梯、拱廊与通道，看不到尽头
材质：统一的石色或抹灰墙体，重复的拱和台阶
光线：光从多处开口进入，形成交错的明暗区
构图：多条透视线交错，主体位于某一段阶梯或拱廊里
色彩：单一的暖白或砂色，靠明暗区分层次
避免：违反透视的混乱拼贴、指示牌和文字、过多人物`},
	{ID: "ruin", Name: "废墟", En: "Ruin", Group: "序", Look: "残缺的墙与柱，被植物和时间慢慢收回", Rules: `尺度：残缺的墙、断柱与开口，比例接近真实建筑
材质：风化的石或砖，裂缝里长出苔藓、藤蔓和小草
光线：午后斜阳，或薄雾里的柔光
构图：残墙作为画框，主体在废墟中央或前景，自身保持完好
色彩：土色、石灰色与植物的绿
避免：战争破坏、爆炸火焰、垃圾和涂鸦`},
	{ID: "frame", Name: "构架", En: "Exposed frame", Group: "序", Look: "外露的钢梁、桁架与节点，结构即外表", Rules: `尺度：外露的钢梁、桁架和节点组成的结构，尺度大于主体
材质：钢构件、螺栓、焊缝，表面有涂层或轻微锈色
光线：明亮日光穿过结构，投下网格状的阴影
构图：结构线条形成清晰的网格或斜撑，主体置于其中
色彩：钢灰或单一的深色涂装，主体与之形成对比
避免：施工杂物、安全标识、真实建筑的辨识特征`},
	{ID: "hive", Name: "蜂巢", En: "Hive", Group: "序", Look: "成百上千个相同单元密集堆叠", Rules: `尺度：成百上千个相同单元密集重复，单元与主体大小相近或更大
材质：统一的模块——格子、壳体或盒子，表面一致
光线：均匀的光，让重复的节奏清楚可读
构图：单元铺满画面，主体是其中唯一不同的那一个
色彩：单元用同一种中性色，主体是唯一的强调色
避免：无序的排列、五颜六色的单元、真实的蜂类`},
}

var conceptStances = []Concept{
	{ID: "romanticism", Name: "浪漫主义", En: "Romanticism", Group: "感性", Look: "人很小，自然与天光很大；雾、逆光、崇高感", Rules: `尺度：自然与天空占画面大部分，主体相对很小
材质：云雾、山石、水面等自然元素，空气里有湿度
光线：逆光或晨昏的光，从远处穿过雾气
构图：大面积天空，地平线偏低，主体在前景成剪影或半剪影
色彩：金色、玫瑰灰与深蓝的过渡，暗部保留细节
避免：平淡的正午顶光、拥挤的画面、甜腻的粉彩`},
	{ID: "expressionism", Name: "表现主义", En: "Expressionism", Group: "感性", Look: "倾斜变形的线条，强烈明暗，情绪压过准确", Rules: `尺度：周围的空间被拉伸或压缩，透视夸张，主体本身不变形
材质：粗犷的、带笔触感的表面，边缘略带锯齿
光线：强烈的明暗对比，硬阴影斜切画面
构图：倾斜的地平线和建筑线条，主体处在张力的中心
色彩：大面积深色加一两种强烈的颜色，如深红、硫黄黄
避免：柔和均匀的光、严格对称、甜美的色调`},
	{ID: "impressionism", Name: "印象主义", En: "Impressionism", Group: "感性", Look: "只抓这一刻的光，边缘松动，色彩颤动", Rules: `尺度：日常尺度，接近人眼的视角
材质：光斑、水面反光、空气里的微粒
光线：某个具体时刻的自然光——清晨、傍晚或穿过树叶的阳光
构图：像瞬间抓拍的随意取景，背景边缘松散，主体清楚
色彩：互补色的小色块并置，阴影里带蓝紫
避免：锐利的硬边背景、厚重的黑色、棚拍式均匀光`},
	{ID: "realism", Name: "现实主义", En: "Realism", Group: "克制", Look: "阴天平光，不美化；污渍、磨损、日常的人", Rules: `尺度：真实的生活尺度
材质：真实的磨损、灰尘、使用痕迹，普通的日常环境
光线：阴天或室内的平淡自然光，没有戏剧性
构图：平视，像随手记录，画面里可以有日常的人或物
色彩：自然、略低饱和，不做风格化调色
避免：美颜滤镜、完美无瑕的表面、夸张的光效`},
	{ID: "classicism", Name: "古典主义", En: "Classicism", Group: "克制", Look: "正面对称，比例严谨，光线均匀而庄重", Rules: `尺度：比例严谨，各部分关系清楚
材质：石材、抹灰、细致的线脚，表面整洁
光线：均匀柔和的光，阴影轻而清楚
构图：正面、中轴对称，主体在中心，前后层次分明
色彩：象牙白、石色与少量深色，稳重
避免：倾斜视角、杂乱背景、过度的戏剧光`},
	{ID: "minimalism", Name: "极简主义", En: "Minimalism", Group: "克制", Look: "大面积留白，只留一个主体和一道影子", Rules: `尺度：主体在画面中偏小或适中，四周大量空白
材质：平整、几乎没有纹理的背景面
光线：单一光源，主体投下一道清楚的影子
构图：只有一个主体和它的影子，其余全部留白
色彩：一种背景色加主体自身的颜色，全画面不超过三种颜色
避免：道具、装饰、多余的物件和花纹`},
	{ID: "abstraction", Name: "抽象主义", En: "Abstraction", Group: "形式", Look: "不再描绘物体，只剩面、线和色块的关系", Rules: `尺度：环境被简化成面、线与色块，主体仍清楚可辨
材质：平涂的色面、干净利落的边界
光线：弱化光影，用色面之间的关系代替明暗
构图：几何色块分割画面，主体与色块形成平衡
色彩：有限的几种饱和色加中性色
避免：写实的环境细节、杂乱的纹理`},
	{ID: "constructivism", Name: "构成主义", En: "Constructivism", Group: "形式", Look: "斜线、红黑灰、几何拼贴，带宣传画的力量", Rules: `尺度：大块斜向的几何面，主体被放大，显得有力量
材质：纸张拼贴感、平面的几何块
光线：平面化处理，明度对比强烈
构图：强烈的对角线，几何块面交错，主体沿斜线摆放
色彩：红、黑、灰与纸白
避免：柔和的渐变、圆润可爱的造型、额外的标语文字`},
	{ID: "futurism", Name: "未来主义", En: "Futurism", Group: "形式", Look: "速度与机械感，重复的运动轨迹，金属冷光", Rules: `尺度：有纵深的动态空间，带速度感
材质：金属、玻璃、光滑的机械表面
光线：冷色的金属反光，光线被拉成轨迹
构图：环境里有重复的运动轨迹或残影和斜向动势，主体本身清晰
色彩：银灰、冷蓝，加一点亮色
避免：静止呆板的摆放、复古木质材料`},
	{ID: "surrealism", Name: "超现实主义", En: "Surrealism", Group: "玄思", Look: "不可能的并置，梦一样清晰却不合逻辑", Rules: `尺度：尺度错位——小物变得巨大，或大物变得微小
材质：每个物体都画得清晰写实
光线：清澈安静的光，阴影长而清楚
构图：不可能的并置，例如室内出现地平线、物体悬在空中，主体是画面焦点
色彩：干净的天空色与暖色地面，安静平和
避免：恐怖元素、混乱的拼贴、模糊不清`},
	{ID: "existentialism", Name: "存在主义", En: "Existentialism", Group: "玄思", Look: "一个孤独的背影，面对巨大而冷漠的空间", Rules: `尺度：一个人或一个主体，面对巨大而空旷的空间
材质：冷硬的墙面、空旷的地面
光线：冷而平的光，或远处一个孤立的光源
构图：主体孤零零地放在大片空白的一角，人物背对或远离观者
色彩：冷灰、低饱和
避免：热闹的人群、温馨的装饰、明快的色彩`},
	{ID: "wabisabi", Name: "侘寂", En: "Wabi-sabi", Group: "玄思", Look: "残缺、朴素、旧的痕迹；低饱和，安静", Rules: `尺度：亲近的小尺度，近距离观看
材质：粗陶、旧木、麻布、泥墙，带裂纹和使用痕迹
光线：柔和的侧窗光，阴影温和
构图：不对称、留白，物件少而安静
色彩：土色、灰褐、米白，低饱和
避免：崭新光亮的表面、鲜艳的颜色、对称的陈设`},
}

var conceptPicks = []ConceptPick{
	{Name: "崇高", Form: "mega", Stance: "romanticism"},
	{Name: "静默", Form: "beton", Stance: "minimalism"},
	{Name: "遗忘", Form: "ruin", Stance: "wabisabi"},
	{Name: "悬置", Form: "levitation", Stance: "surrealism"},
	{Name: "独行", Form: "labyrinth", Stance: "existentialism"},
	{Name: "光之殿", Form: "vault", Stance: "impressionism"},
	{Name: "机器颂", Form: "frame", Stance: "futurism"},
	{Name: "秩序", Form: "hive", Stance: "constructivism"},
}

// ListConcepts returns the built-in library (compiled in, not stored in the data folder).
func (s *Service) ListConcepts() ConceptLibrary {
	return ConceptLibrary{
		Forms:   append([]Concept{}, conceptForms...),
		Stances: append([]Concept{}, conceptStances...),
		Picks:   append([]ConceptPick{}, conceptPicks...),
	}
}

func findConcept(list []Concept, id string) (Concept, bool) {
	for _, c := range list {
		if c.ID == id {
			return c, true
		}
	}
	return Concept{}, false
}

var conceptStrengths = []string{
	"点缀：主体和用户原有的场景描述保持主导，概念只体现在光线、色彩和少量环境细节上。",
	"平衡：主体保持清楚，场景、光线、构图和色彩明显按概念来安排。",
	"主导：概念决定整个画面的场景、尺度、光线、构图和色彩，主体放进这个概念画面里。",
}

const (
	conceptPromptLimit = 32000 // same limit as one submitted prompt
	conceptTimeout     = rewordTimeout
	conceptStage       = "按概念改写提示词"
)

const conceptInstruction = `你要把一个“画面概念”融进一条生图提示词。概念由一个建筑形体和一个艺术立场组成，下面的规则写的是看得见的画面决定。
要求：
1. 把概念规则转成具体的画面描述写进提示词：场景、尺度、材质、光线、构图、色彩都要落到看得见的东西上。不要只罗列“%s”“%s”这类名称，也不要写艺术家、真实建筑或品牌的名字。
2. 保留用户原提示词里的主体、数量、商品细节、尺寸数字和明确要求；引号里的画面文字连同引号逐字保留，不翻译、不改大小写；不要给画面添加用户没有写的文字、符号或图案；原提示词有画面文字时，在提示词里写明除这些文字外不出现其他文字、符号或表情图案。
3. 概念强度：%s
4. %s
5. 规则里“避免”一行的内容不要出现在画面里；可以写成“不要……”。
6. %s
7. 只输出改写后的提示词，不要解释、标题或代码块，也不要用引号把整条提示词包起来。

形体：%s（%s）——%s
%s

立场：%s（%s）——%s
%s

%s`

const conceptRefsNote = "用户带了参考图：商品/主体的外观（造型、颜色、图案、文字、比例）以参考图为准，概念只作用于场景、光线和构图，不要改动主体本身。"
const conceptNoRefsNote = "没有参考图：主体按用户的描述画，不要改变用户写明的主体特征。"

// conceptRequest builds the rewriting request for one stance.
func conceptRequest(in ConceptInput, form, stance Concept, single bool) string {
	output := "输出可以分几行，按画面要素组织。"
	if !single {
		output = "输出必须是一整行，不要换行（这条会和其他立场的版本并排生成，每行一条）。"
	}
	refs := conceptNoRefsNote
	if in.HasReferences {
		refs = conceptRefsNote
	}
	prompt := "用户原提示词：\n" + in.Prompt
	if strings.TrimSpace(in.Prompt) == "" {
		prompt = "用户没有写提示词：请让这个形体本身成为画面主体（一座建筑或一处空间），自拟一个最能表现这个概念的场景，并写出完整提示词。不要加入商品。"
	}
	return fmt.Sprintf(conceptInstruction, form.Name, stance.Name, conceptStrengths[in.Strength], refs, output,
		form.Name, form.En, form.Look, form.Rules, stance.Name, stance.En, stance.Look, stance.Rules, prompt)
}

// conceptFallback appends the card points to the prompt when no LLM can rewrite it.
func conceptFallback(prompt string, form, stance Concept, single bool) string {
	points := func(rules string) string {
		lines := []string{}
		for _, line := range strings.Split(rules, "\n") {
			if _, v, ok := strings.Cut(line, "："); ok && !strings.HasPrefix(line, "避免") {
				lines = append(lines, v)
			} else if ok {
				lines = append(lines, "不要"+v)
			}
		}
		return strings.Join(lines, "；")
	}
	text := fmt.Sprintf("画面概念 %s × %s：%s；%s。", form.Name, stance.Name, points(form.Rules), points(stance.Rules))
	prompt = strings.TrimSpace(prompt)
	switch {
	case prompt == "":
		return text
	case single:
		return prompt + "\n" + text
	default:
		return oneLine(prompt) + " " + text
	}
}

func oneLine(v string) string { return strings.Join(strings.Fields(v), " ") }

// ComposeConcept rewrites the prompt once per chosen stance. It sends no image
// and costs nothing at the image gateway; with an LLM line it uses that LLM.
func (s *Service) ComposeConcept(in ConceptInput) (ConceptResult, error) {
	lang := s.Language() // the whole composition keeps the language it started in
	form, ok := findConcept(conceptForms, in.Form)
	if !ok {
		return ConceptResult{}, errors.New("请选择一个形体")
	}
	if len(in.Stances) < 1 || len(in.Stances) > 3 {
		return ConceptResult{}, errors.New("请选择 1–3 个立场")
	}
	stances := []Concept{}
	for i, id := range in.Stances {
		st, ok := findConcept(conceptStances, id)
		if !ok {
			return ConceptResult{}, errors.New("立场不存在")
		}
		for _, prev := range in.Stances[:i] {
			if prev == id {
				return ConceptResult{}, errors.New("立场重复")
			}
		}
		stances = append(stances, st)
	}
	if in.Strength < 0 || in.Strength >= len(conceptStrengths) {
		return ConceptResult{}, errors.New("概念强度无效")
	}
	if len(in.Prompt) > conceptPromptLimit {
		return ConceptResult{}, errors.New("提示词过长")
	}
	single := len(stances) == 1
	result := ConceptResult{Prompts: make([]string, len(stances)), Labels: make([]string, len(stances))}
	for i, st := range stances {
		result.Labels[i] = form.Name + " × " + st.Name
	}

	// Same licence check as the start of a smart generation: an expired or invalid
	// licence blocks LLM use; a trial only uses the main LLM line.
	s.mu.Lock()
	if msg := s.licenseBlockMessageLocked(); msg != "" {
		s.mu.Unlock()
		return ConceptResult{}, errors.New(msg)
	}
	c, key, ok := s.rewordLLMLocked(Task{})
	if ok && s.trialLocked() && !mainLine(c) {
		ok = false
	}
	s.mu.Unlock()

	if !ok {
		for i, st := range stances {
			result.Prompts[i] = conceptFallback(in.Prompt, form, st, single)
		}
		s.mu.Lock()
		_ = s.store.appendLog("info", "image", "", "未配置 AI 模型，已直接附加概念要点")
		s.mu.Unlock()
		return result, nil
	}

	quoted := []string{}
	for _, q := range quotedText.FindAllStringSubmatch(in.Prompt, -1) {
		quoted = append(quoted, q[1]+q[2])
	}
	check := func(text string) error {
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
	ctx, cancel := context.WithTimeout(s.ctx, conceptTimeout)
	defer cancel()
	errs := make([]error, len(stances))
	var wg sync.WaitGroup
	for i, st := range stances {
		wg.Add(1)
		go func(i int, st Concept) {
			defer wg.Done()
			text, e := s.llmRewrite(ctx, c, key, withPromptLanguage(lang, conceptRequest(in, form, st, single)), conceptStage, check)
			if e == nil && !single {
				text = oneLine(text)
			}
			result.Prompts[i], errs[i] = text, e
		}(i, st)
	}
	wg.Wait()
	for i, e := range errs {
		if e != nil {
			return ConceptResult{}, fmt.Errorf("“%s”改写没有成功：%s", result.Labels[i], s.redact(e.Error(), key))
		}
	}
	result.UsedLLM = true
	// The prompt itself is not logged.
	s.mu.Lock()
	_ = s.store.appendLog("info", "image", "", "已按概念改写提示词")
	s.mu.Unlock()
	return result, nil
}
