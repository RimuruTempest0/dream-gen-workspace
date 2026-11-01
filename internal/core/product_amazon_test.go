package core

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

// A trimmed page with the structure of an Amazon listing (desktop and mobile).
const amazonFixture = `<!doctype html><html><head>
<title>Amazon.com: Brand Embroidered Sweatshirt : Clothing, Shoes &amp; Jewelry</title>
<meta name="description" content="Short summary only.">
</head><body>
<img src="https://m.media-amazon.com/images/G/01/gno/sprites/nav-sprite-global-1x._CB1_.png">
<img src="https://m.media-amazon.com/images/I/41AAAA._AC_SR38,50_.jpg">
<img src="https://m.media-amazon.com/images/I/41BBBB._SS64_.jpg">
<div id="imgTagWrapperId"><img id="landingImage" src="https://m.media-amazon.com/images/I/71MAIN._AC_SX342_.jpg"
 data-old-hires="https://m.media-amazon.com/images/I/71MAIN._AC_SL1500_.jpg"
 data-a-dynamic-image="{&quot;https://m.media-amazon.com/images/I/71MAIN._AC_SX342_.jpg&quot;:[342,342]}"></div>
<span id="productTitle">  Brand Custom Embroidered Floral Sweatshirt  </span>
<div id="productFactsDesktopExpander">Fabric type Cotton &amp; Polyester <h3>About this item</h3>
<ul><li>CUSTOMIZATION: name on the sleeve.</li><li>FEATURE: soft ribbed cuffs.</li></ul> See more About this item</div>
<div id="detailBullets_feature_div"><ul><li>Department &#8207; : &#8206; womens</li><li>ASIN &#8207; : &#8206; B000TEST</li>
<li>Best Sellers Rank: #1 in Sweatshirts</li></ul></div>
<script type="text/javascript">
P.when('A').register("ImageBlockATF", function(A){
  var data = {
    'colorImages': { 'initial': A.$.parseJSON('[{"hiRes":"https://m.media-amazon.com/images/I/71MAIN._AC_SL1500_.jpg","thumb":"https://m.media-amazon.com/images/I/41AAAA._AC_SR38,50_.jpg","large":"https://m.media-amazon.com/images/I/41AAAA._AC_.jpg","variant":"MAIN"},{"hiRes":"https://m.media-amazon.com/images/I/81SIDE._AC_SL1500_.jpg","variant":"PT01"},{"hiRes":null,"large":"https://m.media-amazon.com/images/I/41BACK._AC_.jpg","variant":"PT02","note":"Mom\'s pick"}]')},
    'colorToAsin': {'initial': '{}'}
  };
});
</script>
<script>var other = {"Mimi":[{"hiRes":"https://m.media-amazon.com/images/I/71OTHER._AC_SL1500_.jpg"}]};</script>
</body></html>`

func TestAmazonListingUsesGalleryAndDetailSections(t *testing.T) {
	info, images, e := parseProductPage([]byte(amazonFixture), "https://www.amazon.com/gp/aw/d/B000TEST?th=1")
	if e != nil {
		t.Fatal(e)
	}
	want := []string{
		"https://m.media-amazon.com/images/I/71MAIN._AC_SL1500_.jpg",
		"https://m.media-amazon.com/images/I/81SIDE._AC_SL1500_.jpg",
		"https://m.media-amazon.com/images/I/41BACK._AC_.jpg",
	}
	if strings.Join(images, "\n") != strings.Join(want, "\n") {
		t.Fatalf("expected the shown variant's full-size gallery only, got %q", images)
	}
	if info.Title != "Brand Custom Embroidered Floral Sweatshirt" {
		t.Fatalf("title %q", info.Title)
	}
	for _, s := range []string{"Fabric type Cotton & Polyester", "About this item", "CUSTOMIZATION: name on the sleeve.", "FEATURE: soft ribbed cuffs.", "Department: womens", "ASIN: B000TEST"} {
		if !strings.Contains(info.Description, s) {
			t.Errorf("description misses %q:\n%s", s, info.Description)
		}
	}
	for _, s := range []string{"See more", "Best Sellers Rank", "\u200e", "\u200f", "Short summary only."} {
		if strings.Contains(info.Description, s) {
			t.Errorf("description keeps %q:\n%s", s, info.Description)
		}
	}
	if info.Error != "" {
		t.Fatal("readable listing reported an error:", info.Error)
	}
}

func TestAmazonWithoutGalleryFallsBackToMainImage(t *testing.T) {
	page := strings.Replace(amazonFixture, "'colorImages'", "'otherImages'", 1)
	_, images, e := parseProductPage([]byte(page), "https://www.amazon.de/dp/B000TEST")
	if e != nil {
		t.Fatal(e)
	}
	if len(images) != 1 || images[0] != "https://m.media-amazon.com/images/I/71MAIN._AC_SL1500_.jpg" {
		t.Fatalf("expected the main product image, got %q", images)
	}
}

func TestAmazonHostDetection(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://www.amazon.com/dp/X":       true,
		"https://amazon.co.uk/dp/X":         true,
		"https://www.amazon.com.au/dp/X":    true,
		"https://m.amazon.de/dp/X":          true,
		"https://amazon.example.com/dp/X":   false,
		"https://www.notamazon.com/dp/X":    false,
		"https://www.etsy.com/listing/1/x":  false,
		"https://www.amazon.com.evil.io/dp": false,
	} {
		u, _ := url.Parse(raw)
		if isAmazonHost(u) != want {
			t.Errorf("%s: want %v", raw, want)
		}
	}
}

func TestPartialImageNoteIsNotAPageError(t *testing.T) {
	// Runs saved before 0.6.7 kept the note in Error, which showed "cannot read the page".
	var p ProductInfo
	if e := json.Unmarshal([]byte(`{"sourceURL":"https://www.amazon.com/dp/X","error":"部分商品图片不可读取，可上传参考图"}`), &p); e != nil {
		t.Fatal(e)
	}
	if p.Error != "" || p.ImageNote != "部分商品图片不可读取，可上传参考图" {
		t.Fatalf("legacy note not migrated: %+v", p)
	}
	if e := json.Unmarshal([]byte(`{"error":"商品页无法读取（直连HTTP 403）"}`), &p); e != nil || p.Error == "" {
		t.Fatal("a real page error must stay an error", p, e)
	}
}

func TestProductLinkStopsAtFullWidthBracket(t *testing.T) {
	links := productLinks("【商品链接：https://www.example.com/item/42?color=red】请做套图")
	if len(links) != 1 || links[0] != "https://www.example.com/item/42?color=red" {
		t.Fatalf("got %q", links)
	}
}

func TestCleanProductURL(t *testing.T) {
	cases := map[string]string{
		// The link from the 0.6.13 run: mobile share path, search parameters and a trailing bracket.
		"https://www.amazon.com/gp/aw/d/B0HFYRYR76?pd_rd_plhdr=t&qid=1792204568&th=1】": "https://www.amazon.com/dp/B0HFYRYR76",
		"https://www.amazon.com/Brand-Sweatshirt/dp/b0hfyryr76/ref=sr_1_2?th=1":        "https://www.amazon.com/dp/B0HFYRYR76",
		"https://m.amazon.co.uk/gp/product/B0HFYRYR76":                                 "https://www.amazon.co.uk/dp/B0HFYRYR76",
		"https://smile.amazon.de/dp/B0HFYRYR76#reviews":                                "https://www.amazon.de/dp/B0HFYRYR76",
		// Amazon pages that are not listings, and other shops, keep their link.
		"https://www.amazon.com/s?k=sweatshirt":                     "https://www.amazon.com/s?k=sweatshirt",
		"https://www.walmart.com/ip/Sweatshirt/123456?classType=x】": "https://www.walmart.com/ip/Sweatshirt/123456?classType=x",
		" https://shop.example/item，":                               "https://shop.example/item",
	}
	for in, want := range cases {
		if got := cleanProductURL(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

func TestSavedBlockPageIsReadAgain(t *testing.T) {
	// 0.6.13 saved Amazon's "Continue shopping" page as the product.
	var p ProductInfo
	saved := `{"sourceURL":"https://www.amazon.com/dp/B0HFYRYR76","title":"Amazon.com","description":"Amazon.com Click the button below to continue shopping Continue shopping Conditions of Use","error":""}`
	if e := json.Unmarshal([]byte(saved), &p); e != nil {
		t.Fatal(e)
	}
	if p.Error == "" || p.Description != "" || p.Title != "" {
		t.Fatalf("block page not flagged: %+v", p)
	}
}
