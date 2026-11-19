package core

import (
	"imageworkbench/internal/i18n"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/net/html"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// Full-width brackets and punctuation end a link pasted in Chinese text ("…th=1】").
var productURLPattern = regexp.MustCompile(`https?://[^\s\]\)\\<>【】「」『』（）《》，。；！、]+`)

// errProductTarget marks link problems a browser cannot fix: invalid, private or unresolvable targets.
var errProductTarget = errors.New("product target")
var errProductTooLarge = i18n.Fail("product.tooLarge")

type productTargetError struct{ message i18n.Message }

func (e productTargetError) Error() string           { return e.message.In(i18n.Default) }
func (e productTargetError) Is(target error) bool { return target == errProductTarget }
func (e productTargetError) In(lang string) string { return e.message.In(lang) }

// cleanProductURL cuts a pasted link at the first full-width bracket or
// punctuation and shortens Amazon listing links; other links are kept as typed.
func cleanProductURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if found := productURLPattern.FindString(raw); found != "" && strings.HasPrefix(raw, found) {
		raw = found
	}
	raw = strings.TrimRight(raw, "，。；,;！!\"'")
	if u, e := url.Parse(raw); e == nil {
		if short, ok := amazonProductURL(u); ok {
			return short
		}
	}
	return raw
}

func validateProductURL(raw string) error {
	u, e := url.Parse(raw)
	if e != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return productTargetError{i18n.Msg("product.url.invalid")}
	}
	return nil
}
func publicProductURL(ctx context.Context, raw string) error {
	if e := validateProductURL(raw); e != nil {
		return e
	}
	u, _ := url.Parse(raw)
	host := strings.ToLower(u.Hostname())
	publicHosts.Lock()
	verified := time.Now().Before(publicHosts.until[host])
	publicHosts.Unlock()
	if verified {
		return nil
	}
	// Local DNS behind a system proxy can fail intermittently ("no such host"),
	// so a failed lookup is retried before the link is treated as unreachable.
	var ips []net.IPAddr
	var e error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(productLookupRetryDelay):
			}
		}
		if ips, e = lookupProductHost(ctx, host); e == nil && len(ips) > 0 {
			break
		}
	}
	if e != nil {
		return productTargetError{i18n.Msg("product.url.lookup", "error", e.Error())}
	}
	if len(ips) == 0 {
		return productTargetError{i18n.Msg("product.url.unresolved")}
	}
	for _, a := range ips {
		if a.IP.IsLoopback() || a.IP.IsPrivate() || a.IP.IsUnspecified() || a.IP.IsLinkLocalUnicast() || a.IP.IsLinkLocalMulticast() || a.IP.IsMulticast() {
			return productTargetError{i18n.Msg("product.url.private")}
		}
	}
	// Only public results are remembered; a private answer is checked again every time.
	publicHosts.Lock()
	publicHosts.until[host] = time.Now().Add(5 * time.Minute)
	publicHosts.Unlock()
	return nil
}

var (
	lookupProductHost       = net.DefaultResolver.LookupIPAddr
	productLookupRetryDelay = 400 * time.Millisecond
	publicHosts             = struct {
		sync.Mutex
		until map[string]time.Time
	}{until: map[string]time.Time{}}
)

func productClient(ctx context.Context, allowLocal bool) *http.Client {
	return &http.Client{Transport: newNetworkTransport(), Timeout: 30 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) > 5 {
			return failCtx(r.Context(), "product.redirects")
		}
		r.Header.Del("Authorization")
		r.Header.Del("x-api-key")
		if !allowLocal {
			return publicProductURL(ctx, r.URL.String())
		}
		return validateProductURL(r.URL.String())
	}}
}
func getPublicBytes(ctx context.Context, client *http.Client, raw string, limit int64, allowLocal bool) ([]byte, error) {
	if !allowLocal {
		if e := publicProductURL(ctx, raw); e != nil {
			return nil, e
		}
	}
	r, e := http.NewRequestWithContext(ctx, "GET", raw, nil)
	if e != nil {
		return nil, e
	}
	r.Header.Set("User-Agent", "Mozilla/5.0 (compatible; ImageWorkbench/0.2; product-reference)")
	resp, e := client.Do(r)
	if e != nil {
		return nil, e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, productHTTPError{resp.StatusCode}
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if int64(len(b)) > limit {
		return nil, errProductTooLarge
	}
	return b, e
}
func cutProductText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && (s[n]&0xc0) == 0x80 {
		n--
	}
	return s[:n]
}
func parseProductPage(raw []byte, base, lang string) (ProductInfo, []string, error) {
	info := ProductInfo{SourceURL: base, ReferenceIDs: []string{}}
	images := []string{}
	seen := map[string]bool{}
	baseURL, _ := url.Parse(base)
	addImage := func(value string) {
		u, e := url.Parse(value)
		if e != nil {
			return
		}
		resolved := baseURL.ResolveReference(u)
		if (resolved.Scheme == "http" || resolved.Scheme == "https") && resolved.Host != "" && resolved.User == nil && !seen[resolved.String()] && len(images) < 20 {
			seen[resolved.String()] = true
			images = append(images, resolved.String())
		}
	}
	// Schema.org ImageObject carries the link in contentUrl (Etsy spells it contentURL).
	addImageObject := func(v map[string]any) {
		for _, key := range []string{"contentURL", "contentUrl", "url"} {
			if u, ok := v[key].(string); ok && u != "" {
				addImage(u)
				return
			}
		}
	}
	var structured func(any)
	structured = func(v any) {
		switch x := v.(type) {
		case []any:
			for _, child := range x {
				structured(child)
			}
		case map[string]any:
			typeText := fmt.Sprint(x["@type"])
			if strings.Contains(typeText, "Product") {
				if value, ok := x["name"].(string); ok && info.Title == "" {
					info.Title = value
				}
				if value, ok := x["description"].(string); ok {
					info.Description = value
				}
				switch imageValue := x["image"].(type) {
				case string:
					addImage(imageValue)
				case []any:
					for _, i := range imageValue {
						switch v := i.(type) {
						case string:
							addImage(v)
						case map[string]any:
							addImageObject(v)
						}
					}
				case map[string]any:
					addImageObject(imageValue)
				}
				facts := map[string]any{}
				for _, key := range []string{"brand", "material", "color", "size", "sku", "additionalProperty", "offers"} {
					if v, ok := x[key]; ok {
						facts[key] = v
					}
				}
				b, _ := json.Marshal(facts)
				info.Facts = cutProductText(string(b), 16000)
			}
			for _, child := range x {
				structured(child)
			}
		}
	}
	doc, e := html.Parse(strings.NewReader(string(raw)))
	if e != nil {
		return info, images, e
	}
	var collectStructured func(*html.Node)
	collectStructured = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "script" {
			for _, a := range n.Attr {
				if a.Key == "type" && strings.Contains(strings.ToLower(a.Val), "ld+json") && n.FirstChild != nil {
					var v any
					if json.Unmarshal([]byte(n.FirstChild.Data), &v) == nil {
						structured(v)
					}
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			collectStructured(child)
		}
	}
	collectStructured(doc)
	// Product JSON-LD images are the listing's own photos; page <img> tags also carry
	// recommendations and resized duplicates, so they are only a fallback.
	structuredImages := len(images) > 0
	var text strings.Builder
	var visit func(*html.Node)
	visit = func(n *html.Node) {
		if n.Type == html.ElementNode {
			attrs := map[string]string{}
			for _, a := range n.Attr {
				attrs[a.Key] = a.Val
			}
			if n.Data == "script" {
				if strings.Contains(strings.ToLower(attrs["type"]), "ld+json") && n.FirstChild != nil {
					var v any
					if json.Unmarshal([]byte(n.FirstChild.Data), &v) == nil {
						structured(v)
					}
				}
				return
			}
			if n.Data == "style" || n.Data == "noscript" || n.Data == "nav" || n.Data == "footer" {
				return
			}
			if n.Data == "meta" {
				key := attrs["property"]
				if key == "" {
					key = attrs["name"]
				}
				switch key {
				case "og:title":
					if info.Title == "" {
						info.Title = attrs["content"]
					}
				case "og:description", "description":
					if info.Description == "" {
						info.Description = attrs["content"]
					}
				case "og:image":
					if !structuredImages {
						addImage(attrs["content"])
					}
				}
			}
			if n.Data == "title" && info.Title == "" && n.FirstChild != nil {
				info.Title = n.FirstChild.Data
			}
			if n.Data == "img" && !structuredImages {
				source := attrs["data-src"]
				if source == "" {
					source = attrs["src"]
				}
				addImage(source)
			}
		}
		if n.Type == html.TextNode && text.Len() < 48000 {
			v := strings.TrimSpace(n.Data)
			if v != "" {
				text.WriteString(v)
				text.WriteString(" ")
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(doc)
	if isAmazonHost(baseURL) {
		if gallery := amazonGallery(raw, doc); len(gallery) > 0 {
			images, seen = []string{}, map[string]bool{}
			for _, g := range gallery {
				addImage(g)
			}
		} else {
			kept := []string{}
			for _, i := range images {
				if !amazonUIImage(i) {
					kept = append(kept, i)
				}
			}
			images = kept
		}
		title, details := amazonDetails(doc)
		if title != "" {
			info.Title = title
		}
		if len(details) > len(info.Description) {
			info.Description = details
		}
	}
	if isWalmartHost(baseURL) {
		title, gallery, details, ok := walmartProduct(doc)
		if len(gallery) > 0 {
			images, seen = []string{}, map[string]bool{}
			for _, g := range gallery {
				addImage(g)
			}
		} else {
			kept := []string{}
			for _, i := range images {
				if !walmartUIImage(i) {
					kept = append(kept, walmartFullSize(i))
				}
			}
			images, seen = []string{}, map[string]bool{}
			for _, i := range kept {
				addImage(i)
			}
		}
		if ok && title != "" {
			info.Title = title
		}
		if len(details) > len(info.Description) {
			info.Description = details
		}
	}
	if info.Description == "" {
		info.Description = cutProductText(text.String(), 32000)
	}
	info.Title = cutProductText(info.Title, 1000)
	info.Description = cutProductText(info.Description, 32000)
	if len(strings.TrimSpace(info.Description)) < 40 && len(images) == 0 {
		info.Error = i18n.T(lang, "product.noData")
	}
	return info, images, nil
}
func (s *Service) fetchProduct(ctx context.Context, id string) (ProductInfo, error) {
	r, e := s.GetAgentRun(id)
	if e != nil {
		return ProductInfo{}, e
	}
	// A run with images still missing reads the page again on continue, keeping saved images.
	if r.Product.SourceURL != "" && r.Product.Error == "" && r.Product.ImageNote == "" {
		s.setProgress(id, func(p *RunProgress) {
			p.visit(stepProduct)
			p.visit(stepImages)
			p.ImagesFound, p.ImagesSaved = len(r.Product.ReferenceIDs), len(r.Product.ReferenceIDs)
			p.ProductDone = true
		})
		return r.Product, nil
	}
	if r.Input.ProductURL == "" {
		return ProductInfo{Description: r.Input.ProductText}, nil
	}
	// Runs saved before 0.6.14 may hold the link with a trailing "】" or tracking parameters.
	target := cleanProductURL(r.Input.ProductURL)
	ctx = s.langCtx(ctx)
	s.updateAgent(id, s.msg("agent.stage.productLink"), func(run *AgentRun) error {
		p := progressOf(run)
		p.enter(stepProduct)
		p.ProductVia = "direct"
		return nil
	})
	client := productClient(ctx, s.allowLocalProduct)
	data, e := getPublicBytes(ctx, client, target, 4<<20, s.allowLocalProduct)
	info := ProductInfo{SourceURL: target, ReferenceIDs: r.Product.ReferenceIDs, ImageSources: r.Product.ImageSources}
	var images []string
	if e == nil {
		info, images, e = parseProductPage(data, target, s.Language())
	}
	var browser *browserSession
	if reason := browserFallbackReason(ctx, data, info, e); reason != "" {
		parsed, found, session, be := s.productFromBrowser(ctx, id, target, reason)
		if be == nil {
			browser = session
			defer browser.Close()
			info, images, e = parsed, found, nil
		} else if ctx.Err() != nil {
			e = ctx.Err()
		} else if e != nil || reason == msgCtx(ctx, "product.reason.challenge") {
			// The direct page is unusable; the permanent fallback is uploaded images or pasted text.
			e = s.fail("product.unreadable", "reason", directLabel(s.Language(), reason), "browser", be)
		} else {
			info.Error = cutProductText(info.Error+s.msg("product.browserFailedNote", "error", be), 1500)
		}
	}
	if e == nil {
		info.SourceURL = target
		if cleanProductURL(r.Product.SourceURL) == target {
			info.ReferenceIDs = append([]string{}, r.Product.ReferenceIDs...)
			info.ImageSources = r.Product.ImageSources
		}
		if info.ImageSources == nil {
			info.ImageSources = map[string]string{}
		}
	}
	if e != nil {
		// Discard anything parsed from a blocked or challenge page.
		info = ProductInfo{SourceURL: target, ReferenceIDs: r.Product.ReferenceIDs, ImageSources: r.Product.ImageSources}
		info.Error = cutProductText(s.errText(e), 1500)
		// A page that read fine in an earlier run but not now (usually a passing
		// network or proxy problem) is not asked for again: that run's product
		// data and saved images are used.
		if cached, at, ok := s.recentProduct(id, target); ok && ctx.Err() == nil {
			for _, ref := range r.Product.ReferenceIDs {
				if !slices.Contains(cached.ReferenceIDs, ref) {
					cached.ReferenceIDs = append(cached.ReferenceIDs, ref)
				}
			}
			info = cached
			_ = s.store.appendLog("warn", "agent", id, s.msg("product.log.cached", "error", cutProductText(s.errText(e), 300), "time", localTime(at)))
			s.addActivity(id, "warn", s.msg("product.cached", "time", localTime(at), "n", len(info.ReferenceIDs)))
			s.setProgress(id, func(p *RunProgress) {
				p.visit(stepImages)
				p.ImagesFound, p.ImagesSaved = len(info.ReferenceIDs), len(info.ReferenceIDs)
			})
		}
	}
	if e == nil {
		found := min(len(images), 20)
		s.setProgress(id, func(p *RunProgress) {
			if found > 0 {
				p.enter(stepImages)
			}
			p.ImagesFound, p.ImagesSaved = found, min(len(info.ReferenceIDs), found)
		})
		failed, firstFailure := 0, ""
		defer func() {
			if failed > 0 {
				_ = s.store.appendLog("warn", "agent", id, s.msg("product.log.imagesFailed", "n", failed, "total", len(images), "error", firstFailure))
			}
		}()
		fail := func(err error) {
			failed++
			if firstFailure == "" {
				firstFailure = cutProductText(s.errText(err), 300)
			}
		}
		for _, rawURL := range images {
			if len(info.ReferenceIDs) >= 20 {
				break
			}
			if info.ImageSources[rawURL] != "" {
				continue
			}
			if ctx.Err() != nil {
				break
			}
			b, err := getPublicBytes(ctx, client, rawURL, MaxImageBytes, s.allowLocalProduct)
			if err != nil && browser != nil && ctx.Err() == nil && (s.allowLocalProduct || publicProductURL(ctx, rawURL) == nil) {
				b, err = browser.Image(ctx, rawURL)
			}
			if err != nil {
				fail(err)
				info.ImageNote = s.msg("product.imagesNotDownloaded")
				continue
			}
			u, _ := url.Parse(rawURL)
			s.mu.Lock()
			asset, err := s.saveImage(b, path.Base(u.Path), "reference", "", r.Input.ProjectID, "")
			s.mu.Unlock()
			if err != nil {
				fail(err)
				info.ImageNote = s.msg("product.imagesUnreadable")
				continue
			}
			info.ReferenceIDs = append(info.ReferenceIDs, asset.ID)
			info.ImageSources[rawURL] = asset.ID
			// Persist after every download so cancellation does not discard usable images.
			s.updateAgent(id, s.msg("agent.stage.productImages"), func(run *AgentRun) error {
				run.Product = info
				progressOf(run).ImagesSaved = len(info.ReferenceIDs)
				return nil
			})
		}
	}
	if len(info.ReferenceIDs) == 0 && len(r.Product.ReferenceIDs) > 0 {
		info.ReferenceIDs = r.Product.ReferenceIDs
	}
	s.updateAgent(id, s.msg("agent.stage.productDone"), func(run *AgentRun) error {
		run.Product = info
		progressOf(run).ProductDone = true
		return nil
	})
	// Extraction failures are information for the Agent, not a reason to invent product facts.
	return info, nil
}

// directLabel names why the direct request did not give a usable page.
func directLabel(lang, reason string) string {
	if strings.HasPrefix(reason, "HTTP ") {
		return i18n.T(lang, "product.direct.status", "reason", reason)
	}
	return i18n.T(lang, "product.direct.reason", "reason", reason)
}

// productCacheAge is how old another run's reading of the same link may be to
// stand in for a page that cannot be opened now.
const productCacheAge = 7 * 24 * time.Hour

// recentProduct returns the newest successful reading of the same product link
// by another run, keeping only images that are still saved, and when it was read.
func (s *Service) recentProduct(id, target string) (ProductInfo, string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	runs, e := List[AgentRun](s.store, "agent_run")
	if e != nil {
		return ProductInfo{}, "", false
	}
	for _, r := range runs { // newest first
		p := r.Product
		at, err := time.Parse(time.RFC3339Nano, r.CreatedAt)
		if r.ID == id || err != nil || time.Since(at) > productCacheAge || p.Error != "" || challengeInfo(p) ||
			cleanProductURL(p.SourceURL) != target || strings.TrimSpace(p.Title+p.Description) == "" {
			continue
		}
		refs, sources := []string{}, map[string]string{}
		for _, ref := range p.ReferenceIDs {
			var a Asset
			if s.store.Get("asset", ref, &a) == nil {
				refs = append(refs, ref)
			}
		}
		for src, ref := range p.ImageSources {
			if slices.Contains(refs, ref) {
				sources[src] = ref
			}
		}
		p.ReferenceIDs, p.ImageSources, p.ImageNote, p.SourceURL = refs, sources, "", target
		return p, r.CreatedAt, true
	}
	return ProductInfo{}, "", false
}
