package plans

// missingRetailCountries evaluates the four independent retail cards in a period.
// The current card is about to leave Degtereva's step and therefore counts as
// ready; every other country must have been submitted through the controller to
// Degtereva or reached a later step. Requiring every other card to be at 1.4
// would deadlock the first country's approval.
func missingRetailCountries(cards []Card, currentID int64, currentReady bool) []string {
	needed := []string{"BY", "RU", "KZ", "UZ"}
	ready := map[string]bool{}
	for _, card := range cards {
		if card.FormCode != TemplateRetail {
			continue
		}
		country := card.Country
		if country == "" {
			country = card.ScopeKey
		}
		if card.ID == currentID && currentReady {
			ready[country] = true
			continue
		}
		if (card.StepCode == "1.3" || card.StepCode == "1.4") && card.Status == CardOnApproval ||
			card.Status == CardApproved || card.Status == CardPublished || card.Status == CardPublishFailed {
			ready[country] = true
		}
	}
	missing := make([]string, 0, len(needed))
	for _, country := range needed {
		if !ready[country] {
			missing = append(missing, country)
		}
	}
	return missing
}
