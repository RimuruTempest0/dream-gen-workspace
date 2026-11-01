package core

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Walmart product pages carry no Product JSON-LD. Their <img> tags mix the
// photos with 117 px thumbnail copies and interface icons (SVG truck, logo,
// returns), and the meta description is one sentence. The page's Next.js data
// (<script id="__NEXT_DATA__">) has the selected variant's full-size photos,
// the marketing copy, the detail list and the specifications.

var walmartHostPattern = regexp.MustCompile(`^(www\.)?walmart\.(com|ca)$`)

func isWalmartHost(u *url.URL) bool {
	return u != nil && walmartHostPattern.MatchString(strings.ToLower(u.Hostname()))
}

type walmartNameValue struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type walmartPage struct {
	Props struct {
		PageProps struct {
			InitialData struct {
				Data struct {
					Product struct {
						Name             string `json:"name"`
						Brand            string `json:"brand"`
						ShortDescription string `json:"shortDescription"`
						ImageInfo        struct {
							AllImages []struct {
								URL string `json:"url"`
							} `json:"allImages"`
						} `json:"imageInfo"`
					} `json:"product"`
					Idml struct {
						ShortDescription  string             `json:"shortDescription"`
						LongDescription   string             `json:"longDescription"`
						ProductHighlights []walmartNameValue `json:"productHighlights"`
						Specifications    []walmartNameValue `json:"specifications"`
					} `json:"idml"`
				} `json:"data"`
			} `json:"initialData"`
		} `json:"pageProps"`
	} `json:"props"`
}

// walmartProduct returns the title, the full-size photos and the detail text
// from the page data, or ok=false when the page has no product data.
func walmartProduct(doc *html.Node) (title string, images []string, details string, ok bool) {
	var data string
	var find func(*html.Node) bool
	find = func(n *html.Node) bool {
		if n.Type == html.ElementNode && n.Data == "script" && htmlAttr(n, "id") == "__NEXT_DATA__" && n.FirstChild != nil {
			data = n.FirstChild.Data
			return true
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if find(c) {
				return true
			}
		}
		return false
	}
	if !find(doc) {
		return "", nil, "", false
	}
	var page walmartPage
	if json.Unmarshal([]byte(data), &page) != nil {
		return "", nil, "", false
	}
	product := page.Props.PageProps.InitialData.Data.Product
	idml := page.Props.PageProps.InitialData.Data.Idml
	if product.Name == "" && len(product.ImageInfo.AllImages) == 0 {
		return "", nil, "", false
	}
	for _, i := range product.ImageInfo.AllImages {
		if i.URL != "" {
			images = append(images, walmartFullSize(i.URL))
		}
	}
	parts := []string{}
	short := product.ShortDescription
	if short == "" {
		short = idml.ShortDescription
	}
	for _, block := range []string{short, idml.LongDescription} {
		if text := htmlFragmentText(block); text != "" {
			parts = append(parts, cutProductText(text, 8000))
		}
	}
	// Specifications include everything in the highlights; use highlights only without them.
	facts := idml.Specifications
	if len(facts) == 0 {
		facts = idml.ProductHighlights
	}
	if product.Brand != "" {
		facts = append([]walmartNameValue{{"Brand", product.Brand}}, facts...)
	}
	lines := []string{}
	seen := map[string]bool{}
	for _, f := range facts {
		name, value := strings.TrimSpace(f.Name), strings.TrimSpace(f.Value)
		if name == "" || value == "" || seen[name] {
			continue
		}
		seen[name] = true
		lines = append(lines, name+": "+value)
	}
	if len(lines) > 0 {
		parts = append(parts, cutProductText("Specifications: "+strings.Join(lines, "; "), 8000))
	}
	return strings.TrimSpace(product.Name), images, strings.Join(parts, "\n\n"), true
}

// walmartFullSize drops the resize parameters (odnHeight/odnWidth/odnBg) that
// turn a photo link into a thumbnail.
func walmartFullSize(raw string) string {
	u, e := url.Parse(raw)
	if e != nil || !strings.HasSuffix(strings.ToLower(u.Hostname()), "walmartimages.com") {
		return raw
	}
	q := u.Query()
	for key := range q {
		if strings.HasPrefix(strings.ToLower(key), "odn") {
			q.Del(key)
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// walmartUIImage reports Walmart interface graphics: SVG icons and the shared
// design assets under /dfw/, which are not product photos.
func walmartUIImage(raw string) bool {
	u, e := url.Parse(raw)
	if e != nil {
		return false
	}
	p := strings.ToLower(u.Path)
	return strings.HasSuffix(p, ".svg") || strings.HasPrefix(p, "/dfw/")
}

// htmlFragmentText turns seller HTML (lists, bold labels) into plain text.
func htmlFragmentText(fragment string) string {
	if strings.TrimSpace(fragment) == "" {
		return ""
	}
	nodes, e := html.ParseFragment(strings.NewReader(fragment), &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div})
	if e != nil {
		return strings.TrimSpace(fragment)
	}
	var b strings.Builder
	var visit func(*html.Node)
	visit = func(n *html.Node) {
		if n.Type == html.ElementNode && (n.Data == "script" || n.Data == "style") {
			return
		}
		if n.Type == html.ElementNode && (n.Data == "li" || n.Data == "p" || n.Data == "br") && b.Len() > 0 {
			b.WriteString("; ")
		}
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			visit(c)
		}
	}
	for _, n := range nodes {
		visit(n)
	}
	text := strings.TrimSpace(amazonSpace.ReplaceAllString(b.String(), " "))
	text = strings.ReplaceAll(text, " ;", ";")
	for strings.Contains(text, ";;") {
		text = strings.ReplaceAll(text, ";;", ";")
	}
	return strings.Trim(text, "; ")
}
