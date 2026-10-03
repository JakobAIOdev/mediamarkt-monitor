package product

import (
	"encoding/json"
	"testing"
)

func TestProductAvailability(t *testing.T) {
	for _, tc := range []struct {
		name    string
		product string
		status  string
		inStock bool
		wantErr bool
	}{
		{"single", `{"offers":{"availability":"https://schema.org/InStock"}}`, "InStock", true, false},
		{"http", `{"offers":{"availability":"http://schema.org/OutOfStock"}}`, "OutOfStock", false, false},
		{"preorder", `{"offers":{"availability":"PreOrder"}}`, "PreOrder", false, false},
		{"multiple", `{"offers":[{"availability":"OutOfStock"},{"availability":"InStock"}]}`, "OutOfStock,InStock", true, false},
		{"missing", `{"offers":{}}`, "", false, true},
		{"null", `{"offers":null}`, "", false, true},
		{"empty list", `{"offers":[]}`, "", false, true},
		{"malformed", `{"offers":"unavailable"}`, "", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, inStock, err := Availability(json.RawMessage(tc.product))
			if status != tc.status || inStock != tc.inStock || (err != nil) != tc.wantErr {
				t.Fatalf("status=%q, inStock=%v, err=%v", status, inStock, err)
			}
		})
	}
}
