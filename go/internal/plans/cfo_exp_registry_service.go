package plans

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

type CfoExpenseRegistryCards interface {
	Card(context.Context, int64) (Card, error)
}
type CfoExpenseRegistryService struct {
	store CfoExpenseRegistryStore
	cards CfoExpenseRegistryCards
	scope ScopeStore
	fx    CfoExpenseFXResolver
	rates CfoExpenseRateResolver
}

func NewCfoExpenseRegistryService(store CfoExpenseRegistryStore, cards CfoExpenseRegistryCards, scope ScopeStore) *CfoExpenseRegistryService {
	return &CfoExpenseRegistryService{store: store, cards: cards, scope: scope}
}

// WithResolvers supplies optional dated FX and parameter directories.
func (s *CfoExpenseRegistryService) WithResolvers(fx CfoExpenseFXResolver, rates CfoExpenseRateResolver) *CfoExpenseRegistryService {
	s.fx, s.rates = fx, rates
	return s
}
func cfoRegistryCardCode(c Card) (CfoExpFormCode, string, error) {
	if code, country, ok := ParseCfoExpScope(c.ScopeKey); ok {
		return code, country, nil
	}
	s := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(c.FormCode)), "cfo:")
	n, e := strconv.Atoi(s)
	if e != nil {
		return 0, "", fmt.Errorf("card %d is not a CFO expense card", c.ID)
	}
	code := CfoExpFormCode(n)
	spec, ok := LookupCfoExpSpec(code)
	if !ok {
		return 0, "", fmt.Errorf("unknown CFO form %d", code)
	}
	country := c.Country
	if country == "" {
		country = spec.Country
	}
	return code, strings.ToUpper(country), nil
}
func (s *CfoExpenseRegistryService) access(ctx context.Context, p Principal, cardID int64) (Card, CfoExpFormCode, string, error) {
	c, e := s.cards.Card(ctx, cardID)
	if e != nil {
		return c, 0, "", e
	}
	code, country, e := cfoRegistryCardCode(c)
	if e != nil {
		return c, 0, "", e
	}
	spec, _ := LookupCfoExpSpec(code)
	if !strings.EqualFold(country, spec.Country) {
		return c, 0, "", fmt.Errorf("CFO country mismatch")
	}
	if s.scope != nil {
		f, e := scopeFilterFor(ctx, s.scope, p)
		if e != nil {
			return c, 0, "", e
		}
		if !f.Allows(int(code), country, c.LegalEntity) {
			return c, 0, "", fmt.Errorf("карточка вне вашего среза доступа")
		}
	} else if !p.PlansAdmin {
		return c, 0, "", fmt.Errorf("срез доступа не настроен")
	}
	return c, code, country, nil
}
func (s *CfoExpenseRegistryService) Get(ctx context.Context, p Principal, cardID int64, sheetName string) (CfoExpenseRegistry, error) {
	c, code, country, e := s.access(ctx, p, cardID)
	if e != nil {
		return CfoExpenseRegistry{}, e
	}
	if sheetName == "catalog" || sheetName == "" {
		catalog := CfoExpenseRegistryCatalog(code)
		if len(catalog) == 0 {
			return CfoExpenseRegistry{}, fmt.Errorf("для формы %d нет листов", code)
		}
		sheetName = catalog[0].Name
	}
	sh, ok := CfoExpenseRegistrySheetFor(code, sheetName)
	if !ok {
		return CfoExpenseRegistry{}, fmt.Errorf("лист %q не разрешён для формы %d", sheetName, code)
	}
	scope := cfoRegistryScope(code, country)
	rows, e := s.store.Rows(ctx, cardID, scope, sheetName)
	if e != nil {
		return CfoExpenseRegistry{}, e
	}
	var total float64
	for i := range rows {
		n, issues := CfoExpenseRegistryCalc(sheetName, rows[i], s.fx, s.rates)
		rows[i].Total = n
		rows[i].Issues = issues
		total += n
	}
	return CfoExpenseRegistry{CardID: cardID, Code: code, Country: country, BookScope: scope, Sheets: CfoExpenseRegistryCatalog(code), Sheet: sheetName, Rows: rows, ReadOnly: !cardEditable(c), Total: total, ConfigurationPending: sh.ConfigurationPending}, nil
}
func (s *CfoExpenseRegistryService) Save(ctx context.Context, p Principal, cardID int64, sheetName string, r CfoExpenseRegistryRow) (int64, error) {
	c, code, country, e := s.access(ctx, p, cardID)
	if e != nil {
		return 0, e
	}
	if !cardEditable(c) {
		return 0, fmt.Errorf("реестр закрыт на ввод")
	}
	if _, ok := CfoExpenseRegistrySheetFor(code, sheetName); !ok {
		return 0, fmt.Errorf("лист %q не разрешён", sheetName)
	}
	for month := range r.Months {
		if month < 1 || month > 12 {
			return 0, fmt.Errorf("месяц вне диапазона: %d", month)
		}
	}
	r.Sheet = sheetName
	return s.store.Save(ctx, cardID, cfoRegistryScope(code, country), sheetName, r, p.UserID)
}
func (s *CfoExpenseRegistryService) Delete(ctx context.Context, p Principal, cardID int64, sheetName string, rowID int64) error {
	c, code, country, e := s.access(ctx, p, cardID)
	if e != nil {
		return e
	}
	if !cardEditable(c) {
		return fmt.Errorf("реестр закрыт на ввод")
	}
	if _, ok := CfoExpenseRegistrySheetFor(code, sheetName); !ok {
		return fmt.Errorf("лист %q не разрешён", sheetName)
	}
	return s.store.Delete(ctx, cardID, cfoRegistryScope(code, country), sheetName, rowID, p.UserID)
}
func (s *CfoExpenseRegistryService) Copy(ctx context.Context, p Principal, cardID int64, sheetName string, rowID int64) (int64, error) {
	c, code, country, e := s.access(ctx, p, cardID)
	if e != nil {
		return 0, e
	}
	if !cardEditable(c) {
		return 0, fmt.Errorf("реестр закрыт на ввод")
	}
	if _, ok := CfoExpenseRegistrySheetFor(code, sheetName); !ok {
		return 0, fmt.Errorf("лист %q не разрешён", sheetName)
	}
	return s.store.Copy(ctx, cardID, cfoRegistryScope(code, country), sheetName, rowID, p.UserID)
}
