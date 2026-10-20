package plans

import "strings"

// Шаг 1.4 (Сипарова) утверждает поток розницы целиком: «все четыре страны» — это условие РЕШЕНИЯ
// на шаге 1.4, а не прохода директора. Руководитель и директор (1.2, 1.3) принимают свою страну
// независимо: ждать другие страны им незачем, а Сипарова увидит розницу, когда страны соберутся.

// missingRetailCountries — страны, которые ещё не дошли до шага Сипаровой. currentReady считает
// указанную карточку готовой (для проверки внутри транзакции, где её состояние уже известно).
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
		if card.StepCode == "1.4" && card.Status == CardOnApproval ||
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

// retailFinalDecision — решение шага 1.4 (утвердить или вернуть) по карточке розницы.
func retailFinalDecision(c Card, action string) bool {
	return c.FormCode == TemplateRetail && c.StepCode == "1.4" && c.Status == CardOnApproval &&
		(action == "approve" || action == "return")
}

// retailFinalWaitError — текст отказа, пока не все страны дошли до шага 1.4.
func retailFinalWaitError(missing []string) string {
	return "Сипарова утверждает розницу, когда все четыре страны пройдены директором; ждём: " + strings.Join(missing, ", ")
}