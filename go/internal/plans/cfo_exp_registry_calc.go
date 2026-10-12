package plans

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

func cfoRound2(v float64) float64 { return math.Round(v*100) / 100 }

// CfoExpenseFXResolver supplies a dated exchange rate. A missing rate blocks
// only the affected row (V-08), leaving unrelated rows calculable.
type CfoExpenseFXResolver interface {
	FXForDate(date, currency string) (float64, bool)
}
type CfoExpenseRateResolver interface {
	RateForDate(name, date string) (float64, bool)
}

const (
	cfoDefaultLevy52        = .25
	cfoDefaultCourierFactor = 1.1
)

func cfoNum(a map[string]any, key string) float64 {
	switch v := a[key].(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case jsonNumber:
		return v.Float()
	case string:
		n, _ := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(v), ",", "."), 64)
		return n
	}
	return 0
}

type jsonNumber string

func (n jsonNumber) Float() float64 { v, _ := strconv.ParseFloat(string(n), 64); return v }
func cfoText(a map[string]any, key string) string {
	if v, ok := a[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}
func cfoDate(a map[string]any) string {
	for _, k := range []string{"date", "month_date", "period"} {
		if s := cfoText(a, k); s != "" {
			return s
		}
	}
	return ""
}

// CfoExpenseRegistryCalc computes a row total from the sheet semantics. It does
// not persist or publish anything. A nil FX resolver permits explicit BYN rows;
// foreign-currency travel rows receive V-08 when no dated rate is available.
func CfoExpenseRegistryCalc(sheet string, r CfoExpenseRegistryRow, fx CfoExpenseFXResolver, rates CfoExpenseRateResolver) (float64, []CfoExpenseRegistryIssue) {
	var issues []CfoExpenseRegistryIssue
	n := func(k string) float64 { return cfoNum(r.Attrs, k) }
	switch sheet {
	case "travel", "travel2024", "travelBY", "travelRU", "travelBY25", "travelRU25":
		currency := strings.ToUpper(cfoText(r.Attrs, "currency"))
		rate := n("fx")
		if currency != "" && currency != "BYN" && rate == 0 && fx != nil {
			rate, _ = fx.FXForDate(cfoDate(r.Attrs), currency)
		}
		if currency != "" && currency != "BYN" && rate == 0 {
			issues = append(issues, CfoExpenseRegistryIssue{Code: "V-08", Message: "курс валюты на дату не найден", Blocking: true})
		}
		if rate == 0 {
			rate = 1
		}
		people := n("people")
		if people == 0 {
			people = 1
		}
		tickets := n("tickets") * rate
		lodging := n("lodging") * n("nights") * people * rate
		perDiem := n("per_diem") * n("days") * people * rate
		return cfoRound2(tickets + lodging + perDiem), issues
	case "route93", "route53", "route50":
		rate := n("rate")
		if rate == 0 {
			rate = 1
		}
		return cfoRound2(n("trips_week") * 4 * 12 * n("price") * rate), issues
	case "delivery52":
		rate := n("rate")
		if rate == 0 {
			rate = 1
		}
		levy := n("levy_rate")
		if levy == 0 && rates != nil {
			levy, _ = rates.RateForDate("levy52", cfoDate(r.Attrs))
		}
		if levy == 0 {
			levy = cfoDefaultLevy52
		}
		return cfoRound2(n("trips_week") * 4 * 12 * n("price") * rate * (1 + levy)), issues
	case "couriers24":
		rate := n("factor")
		if rate == 0 && rates != nil {
			rate, _ = rates.RateForDate("courier_factor", cfoDate(r.Attrs))
		}
		if rate == 0 {
			rate = cfoDefaultCourierFactor
		}
		base := n("fact_month")
		if base == 0 {
			base = n("fact")
		}
		if base == 0 {
			base = n("price")
		}
		return cfoRound2(base * rate), issues
	case "fund":
		return cfoRound2(n("price") * (1 + n("rate"))), issues
	}
	// Generic sheets use nullable monthly inputs. Explicit zero remains zero.
	return cfoRound2(r.SumMonths()), issues
}

func CfoExpenseRegistryRowsTotal(sheet string, rows []CfoExpenseRegistryRow, fx CfoExpenseFXResolver, rates CfoExpenseRateResolver) (float64, []CfoExpenseRegistryIssue) {
	var total float64
	var all []CfoExpenseRegistryIssue
	for i, r := range rows {
		n, issues := CfoExpenseRegistryCalc(sheet, r, fx, rates)
		rows[i].Total = n
		rows[i].Issues = issues
		total += n
		all = append(all, issues...)
	}
	return total, all
}
func cfoRegistryValidateSheet(code CfoExpFormCode, name string) error {
	if _, ok := CfoExpenseRegistrySheetFor(code, name); !ok {
		return fmt.Errorf("лист %q не разрешён для формы %d", name, code)
	}
	return nil
}
