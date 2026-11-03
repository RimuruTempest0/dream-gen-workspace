Use this skill to write or revise per-image prompts for save_suite_plan. Product identity comes from the run's effective requirements and fixed design references; this skill controls phrasing, not a new design.

Read enabled product-analysis, embroidery-rules and listing-suite as relevant. Enabled p004-rules overrides this skill's defaults. Resolve incompatible requirements before saving. Keep prompts and user communication in Chinese; preserve in-image text in its original language.

## What the image model actually receives
It receives the item's prompt and reference images in references order. For product images the backend appends locked, editable, ratio and applicable fixed project rules. It does not receive purpose, scene, notes, the product page, other items or the user's chat. Images with noProduct: true do not receive the product spec.

Each image is a separate request. Shared words and references reduce ambiguity but do not enforce exact artwork identity. Planning tools cannot inspect generated results, crop new reference assets, certify a sample, composite artwork or perform visual QA. Never invent these operations, fields or asset IDs, and never report file generation as a visual consistency check.

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
Add style or lighting only when it changes the outcome. Use one use case: product photo, lifestyle, detail close-up or infographic. The purpose line is needed because purpose is not passed to the model.

Completeness comes before length: do not target 250 characters at the expense of exact text, numbers, reference roles, positioning or grouped relationships. Remove repeated prose and irrelevant decoration first. locked is appended, so do not recopy it in full. Use append: true batches of at most six items to manage output size, then finalize without append.

## 2. One design, drawn the same in every image
- Treat chest lettering, its main motif and accessories as one fixed design unit; treat cuff text and its icon as another. Do not independently rearrange the pieces in each image.
- locked defines each unit once: exact text and letterforms, line order, arch, spacing, motif outline and facing, accessories, fill and outline colors, unit proportions, garment anchor, scale and count.
- Use the same short name whenever mentioning the unit; do not add a new accessory, font interpretation or partial redesign in one item.
- Write 图案只包含 for the actual complete design and 图案里没有 for explicitly removed or forbidden features. A local edit preserves unrequested features. A full replacement follows the user's replacement scope.
- Locate units on garment landmarks, not canvas coordinates: garment centerline, zipper end, neckline, cuff seam and sleeve center. Keep scale relative to the garment and internal unit proportions stable; normal perspective can alter apparent screen size.
- Reuse the approved-by-user or otherwise clearly sourced design appearance when available. Generic terms such as “手写体” cannot substitute for visible letterforms. Do not label an unreviewed generated sample as an approved master.
- New design decisions without visual evidence must be made once and fixed in locked, only within the user's authorized design freedom. Exact replication of an unreadable design needs better source material, not invented typography.

## 3. Reference images: fixed identity, explicit roles
Keep the identity/design references for each visible unit in the same order with the same role wording across items that show it. Scene-only or color-only references may vary; do not force irrelevant references onto charts or unrelated crops.
- Prioritize the product and the visible design over optional scene, pose or color cues.
- Label each input by its actual index and role. Say whether it supplies garment shape, chest design, cuff design, color only or scene only. An existing image being modified is the edit target; supporting images are not replacement compositions.
- An old-design reference contributes only the unchanged parts; name the removed or changed features and exclude them. Do not call the old design the exact reference for the new one.
- references contains at most four existing IDs from input.references or product.referenceIDs. An item with useSample: true has at most three manual references; the backend adds the sample last with its own index and role line. Do not number that future sample in your manual reference line.
- If the only clear design source is a whole-garment photo, retain it and say which local design to copy and which composition to ignore. Do not discard the only letterform or icon evidence merely to simplify framing.
- Data-only sources such as size tables supply verified values written in the prompt, not a second product for the model to copy.

## 4. Exact text, letterforms and relationships
Quote each visible string verbatim, including punctuation and case; spell names letter by letter. Describe or reference letterforms, line order, color and location. State the title's arch and its relationship to the motif when relevant.
- Do not use a different script style, tracking, icon or title gap for variety. Scene changes cannot change the product design.
- Use only sourced font names, dimensions, sizes, colors and option counts. Design decisions and measured product facts must stay distinguishable.
- A unit that is visible remains complete. An out-of-frame unit is not moved into another body region.
- No extra wording, symbols, logos or watermarks. This does not remove authorized design text or labels.
- Infographics need every requested label, number and unit explicitly; keep optional copy short. Never truncate verified values to meet a length suggestion.

## 5. One fixed sentence for small grouped details
Copy one cuff group sentence into every cuff-visible prompt. Name the screen side for the chosen view, garment anchor, same-line versus stacked layout, reading order or vertical relationship, letterform reference, spacing and relative scale.
- Same-line and stacked are mutually exclusive. For a stacked icon, describe centering above the name; do not also demand that the icon precede the name on the same baseline.
- Interpret object-relative text direction separately from camera angle. Under P004 choose a front view with the cuff at screen right, forearm vertically down, seam level, text and icon level. Do not rotate text separately from the fabric or switch the physical sleeve to meet a screen-side phrase.
- Keep the embroidered forearm outside the torso, not crossing the chest or resting on the lap. For seated subjects place that hand beside the hip on the cushion or floor; assign mugs and gift opening to the other hand. Flat lays keep that sleeve straight beside the garment.
- Do not hide what the frame shows: a visible cuff cannot be excused as “no embroidery shown”. Whole front garments count as cuff-visible; only real crops or unavailable viewpoints omit the cuff. Do not obscure a detail the item is required to demonstrate.
- Only use the cuff instructions when cuffEmbroidery is true. Plain cuffs must not acquire a sample, icon or name.
- Sample support is limited to one cuff detail item (sample: true) and its dependents (useSample: true); see listing-suite. The backend's sample framing is cuff-specific, so it is not a second chest or whole-product master. Preserve original design references even on dependent items.
- The sample is generated before dependents, but is not visually reviewed by the current backend. A missing or failed sample may not stop other items. Do not promise strict gating, successful copying or automated inspection.

## 6. Scenes and themes
Turn requested settings into visible evidence. For a party with unspecified count, at least 3 people interacting plus a food table, drinks or decorations makes the group readable; honor a specified count instead. Only the principal subject wears the product unless the user requests otherwise.
Theme props belong in the scene, not the garment design. Name relevant holiday objects rather than just “festive”; requested plain backgrounds stay plain. Avoid generic “no extra people” text that cancels an explicitly requested party.
For photos specify realistic fabric, stitch texture, natural skin and a feasible pose. Keep required chest and cuff details clear of hands, hair and props. Under P004 write 整张画面清晰 and use composition or light to emphasize the subject.

## 7. Crop and information scope
- Chest detail: a real close-up of the fixed chest unit and surrounding fabric, without the entire garment outline, collar, hem or cuffs. Keep the chest title if it is part of that unit. A fold edge can show folding without turning the item into another full-garment view.
- Cuff detail: show the correct sleeve group and seam with enough space to judge placement; do not invent a new local arrangement.
- Size and color graphics: minimize redundant full garments. Any garment shown preserves its design and placement; chart space does not justify replacing or omitting details.
- Completely product-free swatch, thread or font graphics use noProduct: true and include all necessary data in their own prompt. A visible garment fragment is still a product image.
- For local crops, explicitly exclude the other garment regions because the backend still appends the full locked spec.

## 8. Revisions: change only the affected scope
For a local user change, preserve untouched prompts, reference order, text and data. Restate relevant invariants in affected prompts; do not rewrite the entire suite for variety.
If the user changes a shared design unit, update locked and every item that shows that unit, preserving unrelated units. Do not mix old and new design versions within the plan.
A correction to one image uses its existing image as the edit target when the application supplies it, with the correct design reference as support. Do not claim masks or exact asset compositing are available unless the tools actually provide them.

## 9. Final planning check
Compare every item against its requested slot, locked, editable, effective project rules and actual reference indices:
- All visible strings and design units are complete and unchanged except explicitly allowed changes.
- The garment anchor, internal layout, scale and current view agree; the selected color is a value, not an open list.
- References for visible identities remain fixed; optional references have bounded roles; old designs cannot silently override changes.
- Cuff group sentences, pose, sample flags, noProduct and crop scope are consistent.
- There are no internal codes, ambiguous sides, invented facts or unsupported workflow claims.
This checks the plan. Do not say generated images passed visual validation or that a sample guarantees identical output.
