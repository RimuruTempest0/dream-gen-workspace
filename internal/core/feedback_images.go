package core

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	stdDraw "image/draw"
	"image/jpeg"
	"os"
	"sort"
	"strings"
	"time"

	"golang.org/x/image/draw"
)

// The images of a 反馈包 (#19): every generated image of the suite (all
// versions, samples included) and every reference (the customer's uploads, the
// product page's images, crops), as JPEGs with a long side of at most 1600
// pixels under images/. run.json lists them under "images" by file name, and
// 反馈.md names the same files.

const (
	feedbackImageSide    = 1600
	feedbackImageQuality = 85
	// A packed image is estimated at this many pixels per byte for the preview,
	// which does not encode anything.
	feedbackPixelsPerByte = 5
)

// feedbackPackDelay slows packing down per image; only the desktop QA build
// sets it, so the progress can be seen.
var feedbackPackDelay time.Duration

type feedbackPackedImage struct {
	// File is the entry under images/; empty when the image is Missing.
	File    string `json:"file,omitempty"`
	AssetID string `json:"assetID"`
	// Kind is generated or reference.
	Kind string `json:"kind"`
	// Source of a reference: upload (the customer's own product photo), product
	// (the product page), crop (cut from a reference) or other.
	Source     string `json:"source,omitempty"`
	TaskID     string `json:"taskID,omitempty"`
	PlanItemID string `json:"planItemID,omitempty"`
	// Slot is the image's number in the plan (序号), Version its attempt in that slot.
	Slot       int    `json:"slot,omitempty"`
	Version    int    `json:"version,omitempty"`
	Sample     bool   `json:"sample,omitempty"`
	SampleArea string `json:"sampleArea,omitempty"`
	// Width and Height are the packed image's.
	Width  int `json:"width,omitempty"`
	Height int `json:"height,omitempty"`
	// Missing: the image's file is no longer on this computer.
	Missing bool `json:"missing,omitempty"`
	path    string
}

// FeedbackPackageProgress reports packing: image Image of Images is being
// packed; Done once the package is saved.
type FeedbackPackageProgress struct {
	RunID  string `json:"runID"`
	Image  int    `json:"image"`
	Images int    `json:"images"`
	Done   bool   `json:"done"`
}

func packedSize(w, h int) (int, int) {
	if long := max(w, h); long > feedbackImageSide {
		if w >= h {
			return feedbackImageSide, max(1, (h*feedbackImageSide+long/2)/long)
		}
		return max(1, (w*feedbackImageSide+long/2)/long), feedbackImageSide
	}
	return w, h
}

// feedbackImagesLocked lists the suite's images: generated ones by slot and
// version (numbered as for 逐张核验), then references. Each image appears once.
func (s *Service) feedbackImagesLocked(run AgentRun, tasks []Task) []feedbackPackedImage {
	out := []feedbackPackedImage{}
	seen := map[string]bool{}
	add := func(id string, v feedbackPackedImage) {
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		v.AssetID = id
		var a Asset
		if s.store.Get("asset", id, &a) != nil {
			v.Missing = true
		} else if p, e := owned(s.store.Root, a.Path); e != nil {
			v.Missing = true
		} else if _, e = os.Stat(p); e != nil {
			v.Missing = true
		} else {
			v.path = p
			v.Width, v.Height = packedSize(a.Width, a.Height)
		}
		out = append(out, v)
	}
	cards := feedbackImagesOf(run, tasks)
	sort.SliceStable(cards, func(i, j int) bool {
		return cards[i].Slot < cards[j].Slot || cards[i].Slot == cards[j].Slot && cards[i].Version < cards[j].Version
	})
	for _, c := range cards {
		for _, id := range c.AssetIDs {
			add(id, feedbackPackedImage{Kind: "generated", TaskID: c.TaskID, PlanItemID: c.PlanItemID, Slot: c.Slot, Version: c.Version, Sample: c.Sample, SampleArea: c.SampleArea})
		}
	}
	for _, id := range run.Input.References {
		add(id, feedbackPackedImage{Kind: "reference", Source: "upload"})
	}
	for _, id := range run.Product.ReferenceIDs {
		add(id, feedbackPackedImage{Kind: "reference", Source: "product"})
	}
	for _, id := range run.Crops {
		add(id, feedbackPackedImage{Kind: "reference", Source: "crop"})
	}
	for _, plan := range run.Plans {
		for _, item := range plan.Items {
			for _, id := range item.References {
				add(id, feedbackPackedImage{Kind: "reference", Source: "other"})
			}
		}
	}
	for _, t := range tasks {
		for _, id := range t.References {
			add(id, feedbackPackedImage{Kind: "reference", Source: "other"})
		}
	}
	generated, references := 0, 0
	for i := range out {
		if out[i].Missing {
			continue
		}
		if out[i].Kind == "generated" {
			generated++
			out[i].File = fmt.Sprintf("images/image-%02d.jpg", generated)
		} else {
			references++
			out[i].File = fmt.Sprintf("images/reference-%02d.jpg", references)
		}
	}
	return out
}

// packFeedbackImage reads the image and returns it as a JPEG of the packed size.
func packFeedbackImage(v feedbackPackedImage) ([]byte, error) {
	data, e := os.ReadFile(v.path)
	if e != nil {
		return nil, e
	}
	src, _, e := image.Decode(bytes.NewReader(data))
	if e != nil {
		return nil, e
	}
	canvas := image.NewRGBA(image.Rect(0, 0, v.Width, v.Height))
	stdDraw.Draw(canvas, canvas.Bounds(), &image.Uniform{C: color.White}, image.Point{}, stdDraw.Src)
	if b := src.Bounds(); b.Dx() == v.Width && b.Dy() == v.Height {
		stdDraw.Draw(canvas, canvas.Bounds(), src, b.Min, stdDraw.Over)
	} else {
		draw.CatmullRom.Scale(canvas, canvas.Bounds(), src, b, stdDraw.Over, nil)
	}
	var out bytes.Buffer
	if e = jpeg.Encode(&out, canvas, &jpeg.Options{Quality: feedbackImageQuality}); e != nil {
		return nil, e
	}
	return out.Bytes(), nil
}

// packFeedbackImages encodes the images outside the service lock, so the image
// queue keeps running, and reports each one before it is packed.
func (s *Service) packFeedbackImages(runID string, images []feedbackPackedImage, progress func(FeedbackPackageProgress)) ([]zipEntry, error) {
	todo := []feedbackPackedImage{}
	for _, v := range images {
		if !v.Missing {
			todo = append(todo, v)
		}
	}
	out := []zipEntry{}
	for i, v := range todo {
		progress(FeedbackPackageProgress{RunID: runID, Image: i + 1, Images: len(todo)})
		if feedbackPackDelay > 0 {
			time.Sleep(feedbackPackDelay)
		}
		data, e := packFeedbackImage(v)
		if e != nil {
			return nil, s.fail("feedback.imageUnreadable", "file", v.File)
		}
		out = append(out, zipEntry{v.File, data})
	}
	return out, nil
}

// feedbackImagesMarkdown names each packed image in 反馈.md, as run.json does.
func feedbackImagesMarkdown(b *strings.Builder, p feedbackPackage) {
	if len(p.Images) == 0 {
		return
	}
	refine := map[string]string{}
	for _, t := range p.Tasks {
		refine[t.ID] = t.RefineInstruction
	}
	sources := map[string]string{"upload": "顾客上传的商品原图", "product": "商品页图片", "crop": "截出的局部", "other": "其他参考图"}
	fmt.Fprintf(b, "\n## 图片\n\n")
	for _, v := range p.Images {
		label := ""
		if v.Kind == "generated" {
			parts := []string{"生成图"}
			if v.Slot > 0 {
				parts = append(parts, fmt.Sprintf("第 %d 张", v.Slot))
			}
			if v.Sample {
				if v.SampleArea != "" {
					parts = append(parts, "样板（"+v.SampleArea+"）")
				} else {
					parts = append(parts, "样板")
				}
			}
			if v.Version > 0 {
				parts = append(parts, fmt.Sprintf("第 %d 版", v.Version))
			}
			label = strings.Join(parts, " ")
			if r := strings.TrimSpace(refine[v.TaskID]); r != "" {
				label += "（修改要求：" + oneLine(r) + "）"
			}
		} else {
			label = "参考图：" + sources[v.Source]
		}
		if v.Missing {
			fmt.Fprintf(b, "- （图片文件已不在本机，%s）%s\n", v.AssetID, label)
		} else {
			fmt.Fprintf(b, "- %s：%s\n", v.File, label)
		}
	}
}
