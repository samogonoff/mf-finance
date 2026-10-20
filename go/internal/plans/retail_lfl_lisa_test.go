package plans

import (
	"strings"
	"testing"
)

// LFL-статус берётся из карточки клиента Лисы (DWH.dim.s_klient по KLIENT_ID = CodeFOX): справочник ЦФО хранит
// устаревший, у большинства магазинов там «новый».
func TestRetailStoresQuery_LisaJoin(t *testing.T) {
	with := retailStoresQuery("FinDWH.dbo.[001 CodeCFO]", "DWH.dim.s_klient")
	for _, want := range []string{"LEFT JOIN DWH.dim.s_klient K ON K.[ITEM_ID] = TRY_CAST(S.[CodeFOX] AS int)", "K.[LFL_STATUS]", "FROM FinDWH.dbo.[001 CodeCFO] S", "S.[GroupCFO1] = @p1"} {
		if !strings.Contains(with, want) {
			t.Errorf("в запросе нет %q:\n%s", want, with)
		}
	}
	without := retailStoresQuery("FinDWH.dbo.[001 CodeCFO]", " ")
	if strings.Contains(without, "JOIN") || strings.Contains(without, "K.[") {
		t.Errorf("без таблицы Лисы присоединения быть не должно:\n%s", without)
	}
}

func TestRetailLFLSource(t *testing.T) {
	cases := []struct {
		name         string
		cfo, lisa    string
		found        bool
		want, wantNm string
	}{
		{"Лиса главнее справочника ЦФО", "новый", " LFL ", true, "LFL", LFLYes},
		{"статус до года из Лисы", "новый", "до года", true, "до года", LFLUnder1},
		{"в Лисе статус не присвоен — не подставляем устаревший", "новый", "", true, "", ""},
		{"карточки в Лисе нет — справочник ЦФО", "закрыт", "", false, "закрыт", LFLClosed},
	}
	for _, c := range cases {
		got := retailLFLSource(c.cfo, c.lisa, c.found)
		if got != c.want {
			t.Errorf("%s: %q, ждали %q", c.name, got, c.want)
		}
		if n := retailLFLStatus(got); n != c.wantNm {
			t.Errorf("%s: канонический %q, ждали %q", c.name, n, c.wantNm)
		}
	}
}
