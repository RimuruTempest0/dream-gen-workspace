package core

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
	"image"
	_ "image/jpeg"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const MaxImageBytes = 32 << 20

func owned(root, path string) (string, error) {
	if filepath.IsAbs(path) {
		return "", errors.New("非法文件路径")
	}
	clean := filepath.Clean(filepath.FromSlash(path))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("非法文件路径")
	}
	return filepath.Join(root, clean), nil
}
func readOwned(root, path string) ([]byte, error) {
	p, e := owned(root, path)
	if e != nil {
		return nil, e
	}
	f, e := os.Open(p)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, MaxImageBytes+1))
	if len(b) > MaxImageBytes {
		return nil, errors.New("图片超过 32 MB")
	}
	return b, e
}
func atomicWrite(path string, data []byte) error {
	f, e := os.CreateTemp(filepath.Dir(path), ".partial-*")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	if _, e = f.Write(data); e != nil {
		f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return os.Rename(name, path)
}
func (s *Service) saveImage(data []byte, name, kind, taskID, project string, id string) (Asset, error) {
	if len(data) > MaxImageBytes {
		return Asset{}, errors.New("图片超过 32 MB")
	}
	config, format, e := image.DecodeConfig(bytes.NewReader(data))
	if e != nil {
		return Asset{}, errors.New("请选择 PNG、JPEG 或 WebP 图片")
	}
	if config.Width*config.Height > 64_000_000 {
		return Asset{}, errors.New("图片像素过大，限制为 6400 万像素")
	}
	img, _, e := image.Decode(bytes.NewReader(data))
	if e != nil {
		return Asset{}, errors.New("图片文件损坏")
	}
	ext := format
	if ext == "jpeg" {
		ext = "jpg"
	}
	dir := "images"
	if kind == "reference" {
		dir = "references"
	}
	if id == "" {
		id = NewID()
	}
	if kind == "generated" {
		name = id + "." + ext
	}
	a := Asset{ID: id, Name: filepath.Base(name), Path: dir + "/" + id + "." + ext, Thumbnail: "thumbnails/" + id + ".png", Kind: kind, TaskID: taskID, ProjectID: project, CreatedAt: Now(), Width: config.Width, Height: config.Height}
	width := 320
	height := int(float64(config.Height) * float64(width) / float64(config.Width))
	if height < 1 {
		height = 1
	}
	if height > 640 {
		height = 640
		width = int(float64(config.Width) * float64(height) / float64(config.Height))
		if width < 1 {
			width = 1
		}
	}
	thumb := image.NewNRGBA(image.Rect(0, 0, width, height))
	draw.CatmullRom.Scale(thumb, thumb.Bounds(), img, img.Bounds(), draw.Over, nil)
	var b bytes.Buffer
	if e = png.Encode(&b, thumb); e != nil {
		return Asset{}, e
	}
	p, _ := owned(s.store.Root, a.Path)
	tp, _ := owned(s.store.Root, a.Thumbnail)
	if e = atomicWrite(p, data); e != nil {
		return Asset{}, e
	}
	if e = atomicWrite(tp, b.Bytes()); e != nil {
		return Asset{}, e
	}
	if e = s.store.Put("asset", id, a); e != nil {
		return Asset{}, e
	}
	return a, nil
}
func (s *Service) ImportImage(name, data string) (Asset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(data) > MaxImageBytes*4/3+16 {
		return Asset{}, errors.New("图片超过 32 MB")
	}
	b, e := base64.StdEncoding.DecodeString(data)
	if e != nil {
		return Asset{}, errors.New("图片内容无法读取")
	}
	return s.saveImage(b, name, "reference", "", "", "")
}
func (s *Service) ReadAsset(id string, thumbnail bool) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var a Asset
	if e := s.store.Get("asset", id, &a); e != nil {
		return "", e
	}
	path := a.Path
	if thumbnail {
		path = a.Thumbnail
	}
	b, e := readOwned(s.store.Root, path)
	if e != nil {
		return "", e
	}
	return "data:" + http.DetectContentType(b) + ";base64," + base64.StdEncoding.EncodeToString(b), nil
}
func (s *Service) ExportAsset(id, path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var a Asset
	if e := s.store.Get("asset", id, &a); e != nil {
		return e
	}
	b, e := readOwned(s.store.Root, a.Path)
	if e != nil {
		return e
	}
	return atomicWrite(path, b)
}
func (s *Service) ListAssets(project string, offset, limit int) (AssetPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, e := List[Asset](s.store, "asset")
	if e != nil {
		return AssetPage{}, e
	}
	filtered := []Asset{}
	for _, a := range all {
		if !a.Hidden && (project == "" || a.ProjectID == project) {
			filtered = append(filtered, a)
		}
	}
	start, end := bounds(offset, limit, len(filtered))
	return AssetPage{filtered[start:end], len(filtered)}, nil
}
func (s *Service) UpdateAsset(id, project string, hidden bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var a Asset
	if e := s.store.Get("asset", id, &a); e != nil {
		return e
	}
	if project != "" {
		var p Project
		if e := s.store.Get("project", project, &p); e != nil {
			return e
		}
	}
	a.ProjectID = project
	a.Hidden = hidden
	return s.store.Put("asset", id, a)
}

const maxBatchAssets = 500

// selectedAssets loads every selected asset before anything changes, so a stale
// or unknown id rejects the whole batch instead of applying it halfway.
func (s *Service) selectedAssets(ids []string) ([]Asset, error) {
	if len(ids) == 0 {
		return nil, errors.New("请先选择素材")
	}
	if len(ids) > maxBatchAssets {
		return nil, fmt.Errorf("一次最多处理 %d 张素材", maxBatchAssets)
	}
	seen := map[string]bool{}
	out := []Asset{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		var a Asset
		if e := s.store.Get("asset", id, &a); e != nil {
			return nil, missing(e, "部分素材已不存在，请刷新后重新选择")
		}
		out = append(out, a)
	}
	return out, nil
}

// UpdateAssets assigns the selected assets to a project ("" = 不归类) or removes
// them from the library. Removing only hides them; files and task links remain.
func (s *Service) UpdateAssets(ids []string, project string, hidden bool) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	assets, e := s.selectedAssets(ids)
	if e != nil {
		return 0, e
	}
	if project != "" {
		var p Project
		if e := s.store.Get("project", project, &p); e != nil {
			return 0, missing(e, "所选项目已不存在")
		}
	}
	for _, a := range assets {
		if !hidden {
			a.ProjectID = project
		}
		a.Hidden = hidden
		if e := s.store.Put("asset", a.ID, a); e != nil {
			return 0, e
		}
	}
	return len(assets), nil
}

// ExportAssets writes the selected originals into one ZIP in selection order.
func (s *Service) ExportAssets(ids []string, path string) (int, error) {
	s.mu.Lock()
	root := s.store.Root
	assets, e := s.selectedAssets(ids)
	s.mu.Unlock()
	if e != nil {
		return 0, e
	}
	names := make([]string, len(assets))
	for i, a := range assets {
		base := strings.TrimSuffix(filepath.Base(a.Name), filepath.Ext(a.Name))
		if base == "" || base == "." {
			base = a.ID
		}
		names[i] = fmt.Sprintf("%03d-%s%s", i+1, base, filepath.Ext(a.Path))
	}
	return len(assets), writeAssetZip(root, assets, names, path)
}

func (s *Service) outputBytes(ctx context.Context, out Output) ([]byte, error) {
	if out.Base64 != "" {
		if len(out.Base64) > MaxImageBytes*4/3+16 {
			return nil, errors.New("结果图片超过 32 MB")
		}
		return base64.StdEncoding.DecodeString(out.Base64)
	}
	return downloadImage(ctx, out.URL)
}
