Use this skill every time you write or rewrite the per-image `prompt` fields for save_suite_plan. It is about phrasing: how to write each prompt so the image model draws what the customer asked for. What the suite contains, and what each element looks like, comes from the other skills.

Priority: if p004-rules is enabled it wins over everything here; embroidery-rules, product-analysis and etsy-suite also win over this skill. When they conflict, follow them and drop the conflicting advice below.

Language: this skill is written in English, but the prompts you save keep the convention of the other skills: Chinese wording, with their fixed Chinese sentences copied verbatim. Text that must appear in the image stays in its original language, exactly as the customer wrote it. Everything you say to the user stays in Chinese.

## What the image model actually receives
- Your `prompt`, then the backend appends `统一锁定项：<locked>`, `允许修改：<editable>`, `画面比例：<ratio>` and, with P004, the fixed project rules.
- The reference images, in the exact order of the item's `references`.
- Nothing else. It never sees `purpose`, `scene`, `notes`, the product page, the other prompts or the user's chat. Anything this image needs must be in its prompt or in locked.
- Every image is generated on its own. Consistency across the suite comes only from the same locked spec, the same wording and the right references.

## 1. Pick one use case per image
The use case sets the style and polish level. Name it in the `用途` line.

| Use case | Typical Etsy image | Stress in the prompt |
| --- | --- | --- |
| 真实摄影（lifestyle） | model wearing or using the product in a real scene | "真实摄影", natural light, real textures, natural pose and hands, framing |
| 产品摄影（product shot） | flat lay, hanger, folded stack, gift packaging | clean silhouette, true colors, materials, even light, simple backdrop |
| 细节特写（detail close-up） | embroidery or print macro | which element, which sleeve or part, stitch type, thread color, how much of the frame it fills |
| 信息图（infographic） | size chart, color chart, thread colors, font options, how to order | exact panels and grid, every text string verbatim, large legible type, whitespace, nothing extra |
| 促销图（ad banner） | only when the user asks for a sale or promo image | exact headline and copy, audience, mood |

## 2. Write short labeled lines, not one paragraph
Image models follow short labeled lines more reliably than a long paragraph. Use these Chinese labels, in this order, and only the lines that help this image:

```
用途：<listing slot + use case, e.g. Etsy 第1张主图，真实摄影>
场景：<backdrop or environment>
主体：<product, model, and which locked elements are visible here>
风格：<真实摄影 / 干净的棚拍产品摄影 / 扁平信息图>
构图：<viewpoint, framing, placement, space kept for text panels>
光线：<light and mood>
材质：<fabric, stitch and thread details that must look real>
参考图：图1=<role>；图2=<role>
画面文字（逐字）："<exact text>"（<where, font style, color>）
本张变化：<only values from editable>
约束：<must keep in this image; what is out of frame>
避免：<things the model tends to add here>
```

- Order is scene → subject → details → constraints. Put the most important facts early.
- `用途` is required: the `purpose` field is not sent to the image model.
- Keep each prompt to about 250 Chinese characters including labels; locked is appended by the backend, so do not repeat it. If a prompt runs long, cut 光线 and 材质 detail first. Never cut 画面文字, 参考图 or 约束.

## 3. How much to add
- When the user's instruction is specific, turn it into the labeled lines without adding creative ideas of your own.
- When it is generic, add only what clearly improves the image: framing, intended use and polish level, a concrete but plain scene.
- Never add anything not implied by the request or the product: extra people, pets or props; brand names, logos, slogans, taglines, prices, shop names, watermarks; color palettes or story beats; any text the user did not give.
- Do not choose left or right placement on your own. Use a side only when the spec fixes it (P004, embroidery-rules, the user).

## 4. Text that must appear in the image
Wrong text is the most expensive failure in this shop, so treat every string carefully.
- List each string on its own `画面文字（逐字）` line, in double quotes, with exact capitals, apostrophes and punctuation: "Max's", "MOM".
- Spell every customer name, and any word that is not a common English word, letter by letter right after it: "Aaliyah"（逐字母：A-a-l-i-y-a-h，只出现这一个词）. The spelling is for checking, so say once that only the quoted word appears.
- Describe how the text looks: font style (script, varsity block, serif), size relative to something on the product, color, and whether it is embroidered or printed.
- Close the list with "除以上文字外，画面里没有其他文字、标志或水印". For lifestyle photos this still applies to the product and the scene.
- Infographics: give every label and every number explicitly (sizes, inches, color names); the model invents them otherwise. Use few, short labels in large type; long sentences and tiny captions render badly.

## 5. Reference images: order and role
- The model receives references in `references` order. Put the most important one first; some models keep the first image in the most detail.
- In the `参考图` line, name each image by position and say what to take from it: "图1=客户成品图，照它画刺绣的字体、位置和方向；图2=卫衣颜色参考，只取颜色". Typical roles: product identity (shape, proportions), embroidery or text detail, color only, pose or composition only, scene mood only.
- When a reference carries content you do not want, say so: "图2只取颜色，不照搬背景和文字".
- If you would only borrow data or layout ideas from an image (another shop's size chart, a long color card), do not pass it; write the data into the prompt (see etsy-suite).
- At most 4 references, all from input.references or product.referenceIDs.
- Give every image that shows the main design the same references, in the same order, with the same role wording. A different set or order lets the model borrow different details for each image.

## 6. One design, drawn the same in every image
The model draws each image separately and copies reference images more strongly than it follows text. A design that is only loosely described drifts: one image adds an accessory from the reference, the next leaves it out.
- **Locked is the single source of truth.** Describe the main design once in locked as a closed list: one sentence "图案只包含：…" naming the subject, action, facing, every accessory and its colors, and one sentence "图案里没有：…".
- **Name what the references show but the design no longer has.** When the user changed the design and a reference still shows the old one, list each removed feature by name in "图案里没有" (e.g. "鹅不戴圣诞帽，身上没有彩灯串，嘴里没有冬青枝"). "不照搬原有图案" or "没有其他装饰" is not enough; the model reads the reference, not the vague phrase.
- **Features the user did not mention:** decide once, write the decision into locked, and apply it to every image. By default a feature that only appears in the old design is removed.
- **Use one fixed short name in every prompt.** When a prompt mentions the design, copy the same short name word for word, e.g. "胸前刺绣（鹅喝珍珠奶茶，按统一锁定项）". Never paraphrase it, describe only part of it, or add details in one image that the others lack.
- **Old-design references:** avoid them for close-ups of the design when another reference gives the same information. If you must pass one, say in the `参考图` line exactly which features not to copy, and repeat them in `避免`.

## 7. Photos that look real
- Write "真实摄影" when the goal is a photo, and name concrete real texture instead of "高品质": fabric weave, rib knit cuffs, raised stitches, thread sheen, natural skin texture.
- Use plain camera language for framing: eye level, three-quarter view, waist-up, full body, overhead flat lay, natural perspective. Under P004, never write depth of field, bokeh, blur or soft focus, not even as something to avoid; write "整张画面清晰" instead.
- Describe people concretely when they appear: rough age, pose, where the hands are, gaze, how the garment sits. Hands, hair and props must not cover a locked element that this image is meant to show.
- Prefer one person per image unless the plan needs more; each extra person is another garment the model has to draw correctly.

## 8. Revisions: change one thing
When the user asks to change part of an existing plan, change only the prompts and lines the request touches. Keep the other prompts, locked, editable and references as they were, and restate the relevant invariants in each changed prompt. Do not rewrite the whole suite to fix one image.

## 9. Example (format only; replace the content with this run's product)
```
用途：Etsy 第1张主图，真实摄影
场景：明亮的客厅，浅色沙发前
主体：一位欧美女性穿森林绿卫衣，正面朝向镜头，腰部以上；胸前刺绣和画面右侧袖口的名字都可见
风格：真实摄影，自然色彩
构图：平视，人物居中，衣服占画面约六成
光线：窗边柔和自然光
材质：加绒卫衣的罗纹袖口，刺绣线迹凸起、有丝线光泽
参考图：图1=客户成品图，照它画胸前图案和袖口名字；图2=卫衣颜色参考，只取颜色
画面文字（逐字）："Max's"（胸前，白色手写体）；"MOM"（胸前，蜂蜜金 varsity 大写）；"Amelia"（逐字母：A-m-e-l-i-a，画面右侧袖口，白色手写体）
本张变化：衣服颜色：森林绿
约束：除以上文字外，画面里没有其他文字、标志或水印；整张画面清晰
避免：额外人物、宠物、品牌标志
```

## 10. Check every prompt before save_suite_plan
- It has a `用途` line with one use case.
- Every string in the image appears verbatim, names spelled letter by letter, matching locked and the user's instruction exactly.
- The `参考图` roles follow the order of `references`, at most 4.
- No added brands, logos, extra text, people or props.
- Nothing contradicts locked or the project rules; under P004 none of the banned blur words appear.
- It says which locked elements are visible and which are out of frame.
- Across the suite: locked has "图案只包含" and "图案里没有" (naming every feature dropped from the references); every prompt uses the same short name for the design; every image showing it has the same references in the same order.
