package product

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestExtractProduct(t *testing.T) {
	page := `<html><script type="application/ld+json">{"@type":"BreadcrumbList"}</script>
	<script type="application/ld+json">{"@type":"Product","sku":"123","name":"Other product"}</script>
	<script type="application/ld+json">{"@type":"BuyAction","object":{"@type":"Product","sku":"2087300","name":"Pokémon Box","offers":{"price":59.99,"availability":"https://schema.org/OutOfStock"}}}</script></html>`
	data, err := extractProduct(strings.NewReader(page), "2087300")
	if err != nil {
		t.Fatal(err)
	}
	var product struct {
		SKU    string `json:"sku"`
		Name   string `json:"name"`
		Offers struct {
			Price        float64 `json:"price"`
			Availability string  `json:"availability"`
		} `json:"offers"`
	}
	if err := json.Unmarshal(data, &product); err != nil {
		t.Fatal(err)
	}
	if product.SKU != "2087300" || product.Name != "Pokémon Box" || product.Offers.Price != 59.99 || product.Offers.Availability != "https://schema.org/OutOfStock" {
		t.Fatalf("unexpected product: %+v", product)
	}
}

func TestExtractProductDirect(t *testing.T) {
	data, err := extractProduct(strings.NewReader(`<script type="application/ld+json">{"@type":"Product","sku":"2087300"}</script>`), "2087300")
	if err != nil || !json.Valid(data) {
		t.Fatalf("data=%s, err=%v", data, err)
	}
}

func TestExtractProductRejectsMissingOrInvalidData(t *testing.T) {
	for _, page := range []string{
		`<script type="application/ld+json">{"@type":"Product","sku":"123"}</script>`,
		`<script type="application/ld+json">{"@type":"BuyAction","object":null}</script>`,
		`<script type="application/ld+json">not JSON</script>`,
		`<script type="text/javascript">{"@type":"Product","sku":"2087300"}</script>`,
		`<html><h1>Captcha</h1></html>`,
	} {
		if _, err := extractProduct(strings.NewReader(page), "2087300"); err == nil {
			t.Errorf("expected missing-product error for %s", page)
		}
	}
}
