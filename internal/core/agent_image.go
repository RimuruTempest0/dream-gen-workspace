package core

import (
	"bytes"
	"encoding/base64"
	"errors"
	"golang.org/x/image/draw"
	"image"
	"image/color"
	stdDraw "image/draw"
	"image/jpeg"
	"net/http"
)

// Keep generation references untouched; only the copy sent to the vision model is bounded.
func visionImageBlock(data []byte) (map[string]any, error) {
	config, _, e := image.DecodeConfig(bytes.NewReader(data))
	if e != nil || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 64_000_000 {
		return nil, errors.New("Skill 或参考图片不可读取或像素过大")
	}
	if len(data) > 4<<20 || config.Width > 2048 || config.Height > 2048 {
		img, _, e := image.Decode(bytes.NewReader(data))
		if e != nil {
			return nil, e
		}
		w, h := config.Width, config.Height
		if w > 1568 || h > 1568 {
			if w >= h {
				h = h * 1568 / w
				w = 1568
			} else {
				w = w * 1568 / h
				h = 1568
			}
		}
		if w < 1 {
			w = 1
		}
		if h < 1 {
			h = 1
		}
		canvas := image.NewRGBA(image.Rect(0, 0, w, h))
		stdDraw.Draw(canvas, canvas.Bounds(), &image.Uniform{C: color.White}, image.Point{}, stdDraw.Src)
		draw.CatmullRom.Scale(canvas, canvas.Bounds(), img, img.Bounds(), stdDraw.Over, nil)
		var out bytes.Buffer
		if e = jpeg.Encode(&out, canvas, &jpeg.Options{Quality: 90}); e != nil {
			return nil, e
		}
		data = out.Bytes()
	}
	return map[string]any{"type": "image", "mimeType": http.DetectContentType(data), "data": base64.StdEncoding.EncodeToString(data)}, nil
}
