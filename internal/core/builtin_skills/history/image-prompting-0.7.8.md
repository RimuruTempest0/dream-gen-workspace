Use this skill to write or revise per-image prompts for save_suite_plan. Product identity comes from the run's effective requirements and fixed design references; this skill controls phrasing, not a new design. Keep prompts and user communication in Chinese; preserve in-image text in its original language.

## What the image model actually receives
It receives the item's prompt and reference images in references order. For product images the backend appends locked, editable, ratio and applicable fixed project rules. It does not receive purpose, scene, notes, the product page, other items or the user's chat. Images with noProduct: true do not receive the product spec. Each image is a separate request: shared words and references reduce ambiguity but do not enforce exact artwork identity.

## 1. Short labeled lines with complete requirements
Use only helpful lines:
```text
用途：<平台、序号、图片类型>
场景：<本张可见的环境与必要道具>
主体：<商品、机位、可见的固定设计组合与定位>
构图：<景别、裁切、姿势与信息板块>
材质：<已有依据的面料与工艺>
参考图：图1=<身份或设计依据、取用部位>；图2=<其他实际角色>
画面文字（逐字）："<确切文字>"（<位置、字形、颜色>）
本张变化：<editable 中本张选定的具体值>
约束：<关键设计关系、必须保留、画面外的部位>
避免：<本张易误加或明确去掉的内容>
```
Add style or lighting only when it changes the outcome. Use one use case: product photo, lifestyle, detail close-up or infographic. The purpose line is needed because purpose is not passed to the model. No internal codes, ambiguous sides or invented facts.

Completeness comes before length: do not target 250 characters at the expense of exact text, numbers, reference roles, positioning or grouped relationships. Remove repeated prose and irrelevant decoration first. locked is appended, so do not recopy it in full.

## 2. One design, drawn the same in every image
- locked defines each fixed design unit once. Use the same short name whenever mentioning a unit; do not add a new accessory, font interpretation or partial redesign in one item.
- Write 图案只包含 for the actual complete design and 图案里没有 for explicitly removed or forbidden features. A local edit preserves unrequested features. A full replacement follows the user's replacement scope.
- Name a kept element by its actual shape in the reference, with a short visual spec (shape, colors, attachments and their position). Never swap in a similar common object from the user's wording: a mini Christmas-stocking headpiece is not a Santa hat.
- Locate units on garment landmarks, not canvas coordinates. Keep scale relative to the garment; normal perspective can alter apparent screen size.
- Generic terms such as “手写体” cannot substitute for visible letterforms. Do not label an unreviewed generated sample as an approved master.
- New design decisions without visual evidence must be made once and fixed in locked, only within the user's authorized design freedom. Exact replication of an unreadable design needs better source material, not invented typography.

## 3. Reference images: fixed identity, explicit roles
Keep the identity/design references for each visible unit in the same order with the same role wording across items that show it. Scene-only or color-only references may vary; do not force irrelevant references onto charts or unrelated crops.
- Label each input by its actual index and role. Say whether it supplies garment shape, chest design, cuff design, color only or scene only. An existing image being modified is the edit target; supporting images are not replacement compositions.
- An old-design reference contributes only the unchanged parts; name the removed or changed features and exclude them. Do not call the old design the exact reference for the new one.
- Sample mechanics (sample: true, useSample: true) are in listing-suite. The backend adds the sample last with its own index and role line; do not number that future sample in your manual reference line.
- If the only clear design source is a whole-garment photo, retain it and say which local design to copy and which composition to ignore.
- Data-only sources such as size tables supply verified values written in the prompt, not a second product for the model to copy.

## 4. Exact text, letterforms and relationships
Quote each visible string verbatim, including punctuation and case; spell names letter by letter. Describe or reference letterforms, line order, color and location. State the title's arch and its relationship to the motif when relevant.
- Scene changes cannot change the product design: no different script style, tracking, icon or title gap for variety.
- Use only sourced font names, dimensions, sizes, colors and option counts.
- A unit that is visible remains complete. An out-of-frame unit is not moved into another body region.
- No extra wording, symbols, logos or watermarks. This does not remove authorized design text or labels.
- Infographics need every requested label, number and unit explicitly; keep optional copy short. Never truncate verified values to meet a length suggestion.

## 5. One fixed sentence for small grouped details
Copy the one cuff sentence from listing-suite into every cuff-visible prompt, only when cuffEmbroidery is true. Same-line and stacked are mutually exclusive. Do not rotate text separately from the fabric or switch the physical sleeve to meet a screen-side phrase.
- Keep the embroidered forearm outside the torso; keep mugs and gifts in the other hand. Enabled project rules give the exact poses.
- Do not hide what the frame shows: a visible cuff cannot be excused as “no embroidery shown”, and a detail the item must demonstrate cannot be obscured.

## 6. Scenes and themes
Turn requested settings into visible evidence. For a party with unspecified count, at least 3 people interacting plus a food table, drinks or decorations makes the group readable; honor a specified count instead. Only the principal subject wears the product unless the user requests otherwise.
When several identical products share a frame, use at most three (the lower end of a user's range) and frame them close enough for each design to read; state that every one copies the sample exactly. Name relevant holiday objects rather than just “festive”; requested plain backgrounds stay plain. Avoid generic “no extra people” text that cancels an explicitly requested party.
For photos specify realistic fabric, stitch texture, natural skin and a feasible pose. Keep required chest and cuff details clear of hands, hair and props.
The image model's content check refuses some harmless prompts, so prefer neutral wording: no body, skin, bare or tight-fit emphasis; children and babies fully dressed in ordinary family scenes, not beds, baths or changing; no weapon, blood, drug or alcohol words, even as color names (gun gray, blood red); no real people, celebrities, brands or known characters unless the user's design text names them. Keep exact design text verbatim.

## 7. Revisions: change only the affected scope
For a local user change, preserve untouched prompts, reference order, text and data. Restate relevant invariants in affected prompts; do not rewrite the entire suite for variety.
If the user changes a shared design unit, update locked and every item that shows that unit, preserving unrelated units. Do not mix old and new design versions within the plan.
A correction to one image uses its existing image as the edit target when the application supplies it, with the correct design reference as support.
