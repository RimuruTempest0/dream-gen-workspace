package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	stdDraw "image/draw"
	"image/png"
	"strings"
	"unicode/utf8"

	"golang.org/x/image/draw"
)

// ---- cutting a clean design reference out of a larger image (0.7.8) ------------
//
// A product page often shows several styles in one picture (a comparison of two
// headpieces, five stockings with five names). Given whole, the image model mixes
// them. The Agent cuts out the one style the set needs and uses that as the design
// reference, above all for the detail sample.

const (
	maxCropsPerRun = 10
	minCropPixels  = 48
	// A small cut is enlarged so the image model gets enough pixels to work from.
	cropTargetSide = 1024
)

// runReferenceIDs are the images a run's plan may use: the user's uploads, the
// product page's images and the Agent's own crops of them.
func runReferenceIDs(r AgentRun) []string {
	ids := append(append([]string{}, r.Input.References...), r.Product.ReferenceIDs...)
	return append(ids, r.Crops...)
}

func cropReferenceTool() map[string]any {
	box := func(d string) map[string]any {
		return map[string]any{"type": "number", "minimum": 0, "maximum": 1, "description": d}
	}
	return map[string]any{"name": "crop_reference", "description": "从本次的参考图里截取一块局部，保存为新的参考图并返回它的 ID 和截图。用来把对比图、多款陈列图里本次要的那一款单独截出来作为干净的设计依据（尤其是细节样板的唯一参考）。坐标是占整张图宽高的比例。", "inputSchema": toolSchema(map[string]any{
		"assetID": textProperty("要截取的参考图 ID"),
		"left":    box("左边界，0–1"),
		"top":     box("上边界，0–1"),
		"width":   box("宽度，0–1"),
		"height":  box("高度，0–1"),
		"label":   textProperty("这块截图是什么，例如“迷你圣诞袜头饰款的袜口特写”"),
	}, "assetID", "left", "top", "width", "height", "label")}
}

func (b *agentBridge) cropReference(r AgentRun, args json.RawMessage) ([]any, error) {
	var v struct {
		AssetID string `json:"assetID"`
		Label   string `json:"label"`
	}
	var box struct {
		Left   float64 `json:"left"`
		Top    float64 `json:"top"`
		Width  float64 `json:"width"`
		Height float64 `json:"height"`
	}
	if e := json.Unmarshal(args, &v); e != nil {
		return nil, e
	}
	json.Unmarshal(args, &box)
	allowed := false
	for _, id := range runReferenceIDs(r) {
		allowed = allowed || id == v.AssetID
	}
	if !allowed {
		return nil, errors.New("只能截取本次需求的参考图")
	}
	if len(r.Crops) >= maxCropsPerRun {
		return nil, fmt.Errorf("每次制作最多截取 %d 张", maxCropsPerRun)
	}
	if box.Left < 0 || box.Top < 0 || box.Width <= 0 || box.Height <= 0 || box.Left+box.Width > 1.0001 || box.Top+box.Height > 1.0001 {
		return nil, errors.New("截取范围无效：left、top、width、height 都是 0–1 的比例，且不能超出图片")
	}
	label := strings.TrimSpace(v.Label)
	if utf8.RuneCountInString(label) > 60 {
		label = string([]rune(label)[:60])
	}
	s := b.service
	s.mu.Lock()
	var a Asset
	e := s.store.Get("asset", v.AssetID, &a)
	var data []byte
	if e == nil {
		data, e = readOwned(s.store.Root, a.Path)
	}
	s.mu.Unlock()
	if e != nil {
		return nil, errors.New("参考图文件不可用")
	}
	cut, e := cropImage(data, box.Left, box.Top, box.Width, box.Height)
	if e != nil {
		return nil, e
	}
	s.mu.Lock()
	saved, e := s.saveImage(cut, "截图-"+label+".png", "reference", "", r.Input.ProjectID, "")
	s.mu.Unlock()
	if e != nil {
		return nil, e
	}
	if e = s.updateAgent(b.runID, "截取参考图局部", func(run *AgentRun) error {
		if len(run.Crops) >= maxCropsPerRun {
			return fmt.Errorf("每次制作最多截取 %d 张", maxCropsPerRun)
		}
		run.Crops = append(run.Crops, saved.ID)
		return nil
	}); e != nil {
		return nil, e
	}
	block, e := visionImageBlock(cut)
	if e != nil {
		return nil, e
	}
	note := mcpText(map[string]any{"assetID": saved.ID, "width": saved.Width, "height": saved.Height, "note": "已保存为本次的参考图，可以放进 references；请看下面的截图确认截对了"})
	return append(note, block), nil
}

// cropImage cuts the box (fractions of the image) out of data and returns a PNG,
// enlarged when the cut is small.
func cropImage(data []byte, left, top, width, height float64) ([]byte, error) {
	src, _, e := image.Decode(bytes.NewReader(data))
	if e != nil {
		return nil, errors.New("参考图无法读取")
	}
	bounds := src.Bounds()
	w, h := float64(bounds.Dx()), float64(bounds.Dy())
	rect := image.Rect(
		bounds.Min.X+int(left*w), bounds.Min.Y+int(top*h),
		bounds.Min.X+int((left+width)*w), bounds.Min.Y+int((top+height)*h),
	).Intersect(bounds)
	if rect.Dx() < minCropPixels || rect.Dy() < minCropPixels {
		return nil, fmt.Errorf("截取范围太小（至少 %d×%d 像素）", minCropPixels, minCropPixels)
	}
	outW, outH := rect.Dx(), rect.Dy()
	if longest := max(outW, outH); longest < cropTargetSide {
		outW, outH = outW*cropTargetSide/longest, outH*cropTargetSide/longest
	}
	canvas := image.NewRGBA(image.Rect(0, 0, outW, outH))
	if outW == rect.Dx() {
		stdDraw.Draw(canvas, canvas.Bounds(), src, rect.Min, stdDraw.Src)
	} else {
		draw.CatmullRom.Scale(canvas, canvas.Bounds(), src, rect, stdDraw.Src, nil)
	}
	var out bytes.Buffer
	if e = png.Encode(&out, canvas); e != nil {
		return nil, e
	}
	return out.Bytes(), nil
}
