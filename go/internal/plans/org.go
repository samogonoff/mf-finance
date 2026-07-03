package plans

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Структура компании (ответственность по направлениям). Дерево узлов выводится
// из dir_cfo (направление group_cfo1 × страна); ЦФО разносятся по этим разрезам
// автоматически. Хранится только «кто ответственный за узел» (plans_org_responsible).
// Сверху — учредители (финальное утверждение, этап 4). Источник истины по
// ответственным за формы ввода; маршрут читает его. «Не разнесено» = ЦФО в узлах
// без ответственного.

// OrgUnit — узел структуры (направление × страна) с числом ЦФО и ответственным.
type OrgUnit struct {
	Group           string `json:"group"`
	Country         string `json:"country"`
	FormCode        string `json:"form_code"`
	CfoCount        int    `json:"cfo_count"`
	ResponsibleID   *int64 `json:"responsible_user_id"`
	ResponsibleName string `json:"responsible_name"`
	DeputyName      string `json:"deputy_name"`
	Assigned        bool   `json:"assigned"`
}

// OrgDirection — направление (group_cfo1) со своими узлами по странам.
type OrgDirection struct {
	Group    string    `json:"group"`
	Units    []OrgUnit `json:"units"`
	CfoCount int       `json:"cfo_count"`
	Assigned int       `json:"assigned_cfo"`
}

// OrgFounder — учредитель (финальное утверждение всего).
type OrgFounder struct {
	Slot string `json:"slot"`
	ID   *int64 `json:"responsible_user_id"`
	Name string `json:"responsible_name"`
}

// OrgTree — вся структура + покрытие ЦФО.
type OrgTree struct {
	Founders      []OrgFounder   `json:"founders"`
	Directions    []OrgDirection `json:"directions"`
	TotalCfo      int            `json:"total_cfo"`
	AssignedCfo   int            `json:"assigned_cfo"`
	UnassignedCfo int            `json:"unassigned_cfo"`
}

// OrgStore — доступ к структуре компании.
type OrgStore struct{ pool *pgxpool.Pool }

// NewOrgStore — конструктор.
func NewOrgStore(pool *pgxpool.Pool) *OrgStore { return &OrgStore{pool: pool} }

// Tree собирает дерево направлений + учредителей + покрытие.
func (s *OrgStore) Tree(ctx context.Context) (OrgTree, error) {
	var t OrgTree

	// Глобальные замы (stage='') ответственных — для подсказки подмены.
	deputyByPrincipal := map[int64]string{}
	if drows, err := s.pool.Query(ctx, `
		SELECT d.principal_user_id, TRIM(COALESCE(u.last_name,'')||' '||COALESCE(u.name,''))
		FROM plans_deputy d JOIN users u ON u.id=d.deputy_user_id WHERE d.stage_code=''`); err == nil {
		for drows.Next() {
			var pid int64
			var name string
			if drows.Scan(&pid, &name) == nil {
				deputyByPrincipal[pid] = name
			}
		}
		drows.Close()
	}

	// Узлы направлений из dir_cfo + ответственный.
	rows, err := s.pool.Query(ctx, `
		SELECT u.grp, u.cnt, u.cfo_count,
		       r.responsible_user_id, TRIM(COALESCE(usr.last_name,'')||' '||COALESCE(usr.name,'')), COALESCE(r.form_code,'')
		FROM (
			SELECT payload_json->>'group_cfo1' grp, COALESCE(payload_json->>'country','') cnt, count(*) cfo_count
			FROM plans_directory_row pr JOIN plans_directory d ON d.id=pr.directory_id
			WHERE d.code='dir_cfo' AND COALESCE(payload_json->>'group_cfo1','')<>''
			GROUP BY 1,2
		) u
		LEFT JOIN plans_org_responsible r ON r.group_cfo1=u.grp AND r.country=u.cnt
		LEFT JOIN users usr ON usr.id=r.responsible_user_id
		ORDER BY u.grp, u.cnt`)
	if err != nil {
		return t, err
	}
	defer rows.Close()

	dirIdx := map[string]int{}
	for rows.Next() {
		var u OrgUnit
		var respID *int64
		if err := rows.Scan(&u.Group, &u.Country, &u.CfoCount, &respID, &u.ResponsibleName, &u.FormCode); err != nil {
			return t, err
		}
		u.ResponsibleID = respID
		u.Assigned = respID != nil
		if respID != nil {
			u.DeputyName = deputyByPrincipal[*respID]
		}
		t.TotalCfo += u.CfoCount
		if u.Assigned {
			t.AssignedCfo += u.CfoCount
		}
		i, ok := dirIdx[u.Group]
		if !ok {
			i = len(t.Directions)
			dirIdx[u.Group] = i
			t.Directions = append(t.Directions, OrgDirection{Group: u.Group})
		}
		d := &t.Directions[i]
		d.Units = append(d.Units, u)
		d.CfoCount += u.CfoCount
		if u.Assigned {
			d.Assigned += u.CfoCount
		}
	}
	t.UnassignedCfo = t.TotalCfo - t.AssignedCfo

	// Учредители (финальное утверждение).
	frows, err := s.pool.Query(ctx, `
		SELECT r.country, r.responsible_user_id, TRIM(COALESCE(u.last_name,'')||' '||COALESCE(u.name,''))
		FROM plans_org_responsible r LEFT JOIN users u ON u.id=r.responsible_user_id
		WHERE r.group_cfo1='Учредители' ORDER BY r.country`)
	if err == nil {
		for frows.Next() {
			var f OrgFounder
			var id *int64
			if frows.Scan(&f.Slot, &id, &f.Name) == nil {
				f.ID = id
				t.Founders = append(t.Founders, f)
			}
		}
		frows.Close()
	}
	return t, rows.Err()
}

// SetResponsible назначает ответственного за узел (или учредителя). userID=0 снимает.
func (s *OrgStore) SetResponsible(ctx context.Context, group, country string, userID int64, formCode string) error {
	var uid *int64
	if userID != 0 {
		uid = &userID
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO plans_org_responsible (group_cfo1, country, responsible_user_id, form_code, updated_at)
		VALUES ($1,$2,$3,COALESCE(NULLIF($4,''), (SELECT form_code FROM plans_org_responsible WHERE group_cfo1=$1 AND country=$2), ''), NOW())
		ON CONFLICT (group_cfo1, country)
		DO UPDATE SET responsible_user_id=$3,
		    form_code=COALESCE(NULLIF($4,''), plans_org_responsible.form_code),
		    updated_at=NOW()`,
		group, country, uid, formCode)
	return err
}

// ResponsiblesForDirection — ответственные узлов направления (для маршрута: кто
// заполняет формы этого направления). Возвращает имена с разрезом страны.
func (s *OrgStore) ResponsiblesForDirection(ctx context.Context, group string) ([]OrgUnit, error) {
	t, err := s.Tree(ctx)
	if err != nil {
		return nil, err
	}
	for _, d := range t.Directions {
		if d.Group == group {
			return d.Units, nil
		}
	}
	return nil, nil
}
