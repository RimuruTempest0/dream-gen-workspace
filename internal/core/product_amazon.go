package core

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// Amazon has no Product JSON-LD. Its page <img> tags are mostly 38–64 px
// thumbnails, navigation sprites and recommendations, and the meta description
// is a one-line summary. The listing's own photos are in the image-block script
// ('colorImages': {'initial': ...}) and the details are in a few known sections.

// amazon.com, amazon.de, amazon.co.uk, amazon.com.au …, with or without www./m./smile.
var amazonHostPattern = regexp.MustCompile(`^(www\.|m\.|smile\.)?amazon\.(com|[a-z]{2}|co\.[a-z]{2}|com\.[a-z]{2})$`)

func isAmazonHost(u *url.URL) bool {
	return u != nil && amazonHostPattern.MatchString(strings.ToLower(u.Hostname()))
}

// /dp/ASIN, /gp/product/ASIN, /gp/aw/d/ASIN (mobile share links) and older forms.
var amazonASINPattern = regexp.MustCompile(`(?i)/(?:dp|gp/product|gp/aw/d|exec/obidos/asin|o/asin)/([A-Z0-9]{10})(?:[/?#]|$)`)

// amazonProductURL shortens a listing link to https://www.<site>/dp/ASIN. Search
// and tracking parameters and the mobile path can get a bot-check page instead
// of the listing; the short form opens the same product on the desktop page.
func amazonProductURL(u *url.URL) (string, bool) {
	if !isAmazonHost(u) {
		return "", false
	}
	m := amazonASINPattern.FindStringSubmatch(u.EscapedPath())
	if m == nil {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	host = "www." + strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(host, "www."), "m."), "smile.")
	return "https://" + host + "/dp/" + strings.ToUpper(m[1]), true
}

var amazonGalleryPattern = regexp.MustCompile(`(?s)'colorImages'\s*:\s*\{\s*'initial'\s*:\s*[\w.$]*parseJSON\('(.*?)'\)`)

// amazonGallery returns the photos of the variant shown on the page, largest
// available size first per photo, in gallery order.
func amazonGallery(raw []byte, doc *html.Node) []string {
	images := []string{}
	if m := amazonGalleryPattern.FindSubmatch(raw); m != nil {
		var entries []struct {
			HiRes string `json:"hiRes"`
			Large string `json:"large"`
		}
		text := string(m[1])
		// The JSON sits inside a single-quoted JavaScript string.
		for _, candidate := range []string{text, strings.ReplaceAll(text, `\'`, `'`), strings.ReplaceAll(strings.ReplaceAll(text, `\'`, `'`), `\\`, `\`)} {
			if json.Unmarshal([]byte(candidate), &entries) == nil {
				break
			}
			entries = nil
		}
		for _, e := range entries {
			if e.HiRes != "" {
				images = append(images, e.HiRes)
			} else if e.Large != "" {
				images = append(images, e.Large)
			}
		}
	}
	if len(images) > 0 {
		return images
	}
	// Fallback: the main product image element.
	var landing func(*html.Node) string
	landing = func(n *html.Node) string {
		if n.Type == html.ElementNode && n.Data == "img" && htmlAttr(n, "id") == "landingImage" {
			if v := htmlAttr(n, "data-old-hires"); v != "" {
				return v
			}
			best, width := "", 0
			var sizes map[string][]json.Number
			if json.Unmarshal([]byte(htmlAttr(n, "data-a-dynamic-image")), &sizes) == nil {
				for u, wh := range sizes {
					if len(wh) > 0 {
						if w, _ := strconv.Atoi(wh[0].String()); w > width || w == width && u < best {
							best, width = u, w
						}
					}
				}
			}
			if best != "" {
				return best
			}
			return htmlAttr(n, "src")
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if v := landing(c); v != "" {
				return v
			}
		}
		return ""
	}
	if v := landing(doc); v != "" {
		images = append(images, v)
	}
	return images
}

// amazonUIImage reports Amazon interface graphics (sprites, icons, badges),
// which live under /images/G/ rather than the product image path /images/I/.
func amazonUIImage(raw string) bool {
	u, e := url.Parse(raw)
	return e == nil && strings.Contains(u.Path, "/images/G/")
}

// amazonSections lists the detail sections in reading order: bullet points
// ("About this item"), product facts, the overview table, the seller's
// description and the detail bullets (size, department, dates).
var amazonSections = []string{"feature-bullets", "productFactsDesktopExpander", "productOverview_feature_div", "productDescription", "detailBullets_feature_div"}

// amazonDetails returns the product title and the text of the detail sections.
func amazonDetails(doc *html.Node) (string, string) {
	found := map[string]string{}
	title := ""
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			id := htmlAttr(n, "id")
			if id == "productTitle" && title == "" {
				title = amazonText(n)
			}
			for _, section := range amazonSections {
				if id == section && found[id] == "" {
					found[id] = amazonText(n)
					return
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	parts := []string{}
	for _, section := range amazonSections {
		text := found[section]
		// Warranty links, sales rank and review counts are not product facts.
		for _, stop := range []string{"Product Warranty", "Best Sellers Rank", "Customer Reviews"} {
			if i := strings.Index(text, stop); i >= 0 {
				text = strings.TrimSpace(text[:i])
			}
		}
		if text != "" {
			parts = append(parts, cutProductText(text, 8000))
		}
	}
	return title, strings.Join(parts, "\n\n")
}

var amazonSpace = regexp.MustCompile(`\s+`)

func amazonText(n *html.Node) string {
	var b strings.Builder
	var visit func(*html.Node)
	visit = func(n *html.Node) {
		if n.Type == html.ElementNode && (n.Data == "script" || n.Data == "style" || n.Data == "noscript") {
			return
		}
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
			b.WriteString(" ")
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			visit(c)
		}
	}
	visit(n)
	// Detail rows contain invisible direction marks around the colon.
	text := strings.NewReplacer("‎", "", "‏", "").Replace(b.String())
	text = amazonSpace.ReplaceAllString(text, " ")
	for _, noise := range []string{"See more About this item", "See more", "See less"} {
		text = strings.ReplaceAll(text, noise, "")
	}
	text = strings.ReplaceAll(text, " : ", ": ")
	return strings.TrimSpace(amazonSpace.ReplaceAllString(text, " "))
}

func htmlAttr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}
