package core

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

// A trimmed Walmart product page: icons and thumbnail copies in <img>, the real
// data in __NEXT_DATA__.
func walmartFixture(t *testing.T, withData bool) string {
	data := map[string]any{"props": map[string]any{"pageProps": map[string]any{"initialData": map[string]any{"data": map[string]any{
		"product": map[string]any{
			"name":             "Brand Women's Embroidered Sweatshirt, Sizes XS-4X",
			"brand":            "Brand",
			"shortDescription": "Ultra-soft terry with <b>embroidered flowers</b>.",
			"imageInfo": map[string]any{"allImages": []map[string]any{
				{"id": "A", "url": "https://i5.walmartimages.com/seo/Main_abc.jpeg"},
				{"id": "B", "url": "https://i5.walmartimages.com/asr/side.jpeg?odnHeight=117&odnWidth=117&odnBg=FFFFFF"},
				{"id": "C", "url": "https://i5.walmartimages.com/asr/back.jpeg"},
			}},
		},
		"idml": map[string]any{
			"longDescription": "<ul><li><b>Material:</b> 70% Cotton/30% Polyester</li><li><b>Care:</b> Machine washable</li></ul><p><b>Fit:</b> Relaxed</p>",
			"specifications": []map[string]any{
				{"name": "Brand", "value": "Brand"},
				{"name": "Color category", "value": "Green"},
				{"name": "Clothing size", "value": "XS"},
			},
			"productHighlights": []map[string]any{{"name": "Only in highlights", "value": "x"}},
		},
	}}}}}
	script := ""
	if withData {
		b, e := json.Marshal(data)
		if e != nil {
			t.Fatal(e)
		}
		script = `<script id="__NEXT_DATA__" type="application/json">` + string(b) + `</script>`
	}
	return `<!doctype html><html><head><title>Brand Embroidered Sweatshirt, Green, XS - Walmart.com</title>
<meta name="description" content="One sentence summary.">
<script type="application/ld+json">{"@context":"https://schema.org","@type":"WebPage"}</script></head><body>
<img src="https://i5.walmartimages.com/dfw/63fd9f59/v1/delivery-truck.svg">
<img src="https://i5.walmartimages.com/dfw/4ff9c6c9/k2-_icon.v1.png">
<img src="https://i5.walmartimages.com/seo/Main_abc.jpeg?odnHeight=117&odnWidth=117&odnBg=FFFFFF">
<img src="https://i5.walmartimages.com/asr/back.jpeg?odnHeight=573&odnWidth=573&odnBg=FFFFFF">
` + script + `</body></html>`
}

func TestWalmartListingUsesPageData(t *testing.T) {
	info, images, e := parseProductPage([]byte(walmartFixture(t, true)), "https://www.walmart.com/ip/Brand-Sweatshirt/19990250082?classType=VARIANT")
	if e != nil {
		t.Fatal(e)
	}
	want := []string{
		"https://i5.walmartimages.com/seo/Main_abc.jpeg",
		"https://i5.walmartimages.com/asr/side.jpeg",
		"https://i5.walmartimages.com/asr/back.jpeg",
	}
	if strings.Join(images, "\n") != strings.Join(want, "\n") {
		t.Fatalf("expected the full-size photos only, got %q", images)
	}
	if info.Title != "Brand Women's Embroidered Sweatshirt, Sizes XS-4X" {
		t.Fatalf("title %q", info.Title)
	}
	for _, s := range []string{"Ultra-soft terry with embroidered flowers.", "Material: 70% Cotton/30% Polyester; Care: Machine washable; Fit: Relaxed", "Specifications: Brand: Brand; Color category: Green; Clothing size: XS"} {
		if !strings.Contains(info.Description, s) {
			t.Errorf("description misses %q:\n%s", s, info.Description)
		}
	}
	for _, s := range []string{"<b>", "<li>", "One sentence summary.", "Only in highlights"} {
		if strings.Contains(info.Description, s) {
			t.Errorf("description keeps %q:\n%s", s, info.Description)
		}
	}
	if strings.Count(info.Description, "Brand: Brand") != 1 {
		t.Errorf("brand listed twice:\n%s", info.Description)
	}
	if info.Error != "" {
		t.Fatal("readable listing reported an error:", info.Error)
	}
}

func TestWalmartWithoutPageDataDropsIconsAndThumbnailSizes(t *testing.T) {
	_, images, e := parseProductPage([]byte(walmartFixture(t, false)), "https://www.walmart.com/ip/x/1")
	if e != nil {
		t.Fatal(e)
	}
	want := []string{"https://i5.walmartimages.com/seo/Main_abc.jpeg", "https://i5.walmartimages.com/asr/back.jpeg"}
	if strings.Join(images, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got %q", images)
	}
}

func TestWalmartHostDetection(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://www.walmart.com/ip/x/1":      true,
		"https://walmart.com/ip/x/1":          true,
		"https://www.walmart.ca/en/ip/x/1":    true,
		"https://walmart.example.com/ip/x":    false,
		"https://www.walmart.com.evil.io/ip":  false,
		"https://www.amazon.com/dp/B000":      false,
		"https://i5.walmartimages.com/a.jpeg": false,
	} {
		u, _ := url.Parse(raw)
		if isWalmartHost(u) != want {
			t.Errorf("%s: want %v", raw, want)
		}
	}
}

func TestWalmartRobotPageUsesBrowser(t *testing.T) {
	page := []byte(`<html><head><title>Robot or human?</title></head><body>Activate and hold the button</body></html>`)
	if !looksLikeChallenge(page) {
		t.Fatal("Walmart robot check not recognised")
	}
}
