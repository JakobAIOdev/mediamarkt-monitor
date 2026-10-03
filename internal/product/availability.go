package product

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Availability reports all published offer statuses and whether any offer is
// explicitly InStock. Missing availability is an error.
func Availability(product json.RawMessage) (string, bool, error) {
	var data struct {
		Offers json.RawMessage `json:"offers"`
	}
	if err := json.Unmarshal(product, &data); err != nil {
		return "", false, fmt.Errorf("decode product availability: %w", err)
	}
	type offer struct {
		Availability string `json:"availability"`
	}
	var offers []offer
	if len(data.Offers) > 0 && data.Offers[0] == '[' {
		if err := json.Unmarshal(data.Offers, &offers); err != nil {
			return "", false, fmt.Errorf("decode product offers: %w", err)
		}
	} else {
		var single offer
		if err := json.Unmarshal(data.Offers, &single); err != nil {
			return "", false, fmt.Errorf("decode product offer: %w", err)
		}
		offers = []offer{single}
	}
	var statuses []string
	inStock := false
	for _, offer := range offers {
		status := NormalizeAvailability(offer.Availability)
		if status != "" {
			statuses = append(statuses, status)
		}
		inStock = inStock || status == "InStock"
	}
	if len(statuses) == 0 {
		return "", false, fmt.Errorf("product offers contain no availability")
	}
	return strings.Join(statuses, ","), inStock, nil
}

// NormalizeAvailability removes schema.org URL prefixes from offer statuses.
func NormalizeAvailability(status string) string {
	status = strings.TrimSpace(status)
	return strings.TrimPrefix(strings.TrimPrefix(status, "https://schema.org/"), "http://schema.org/")
}
