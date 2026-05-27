package bugtracker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"mime/multipart"
	"strings"
	"time"

	"github.com/company/finance-api/internal/notifications"
)

const dedupWindow = 24 * time.Hour

type Service struct {
	repo    *Repo
	storage *Storage
	notif   *notifications.Service
}

func NewService(repo *Repo, storage *Storage, notif *notifications.Service) *Service {
	return &Service{repo: repo, storage: storage, notif: notif}
}

// computeSignature — SHA256(section|url|title|description). url берётся из route.url
// (фронт его всегда передаёт).
func computeSignature(in CreateInput) string {
	url, _ := in.Route["url"].(string)
	h := sha256.New()
	h.Write([]byte(strings.ToLower(in.Section)))
	h.Write([]byte("|"))
	h.Write([]byte(strings.ToLower(url)))
	h.Write([]byte("|"))
	h.Write([]byte(strings.ToLower(strings.TrimSpace(in.Title))))
	h.Write([]byte("|"))
	h.Write([]byte(strings.ToLower(strings.TrimSpace(in.Description))))
	return hex.EncodeToString(h.Sum(nil))
}

// Create — основной поток создания репорта.
// Возвращает либо новый Report, либо найденный недавний дубль (deduplicated=true).
func (s *Service) Create(
	ctx context.Context, userID *int64, in CreateInput, files []*multipart.FileHeader,
) (rep *Report, deduplicated bool, err error) {
	if !IsValidSection(in.Section) {
		in.Section = "other"
	}
	if !IsValidType(in.Type) {
		in.Type = TypeBug
	}
	if strings.TrimSpace(in.Title) == "" {
		return nil, false, errors.New("title required")
	}

	sig := computeSignature(in)

	if dupID, err := s.repo.FindRecentDuplicate(ctx, userID, sig, dedupWindow); err != nil {
		return nil, false, err
	} else if dupID != 0 {
		rep, err = s.repo.Get(ctx, dupID)
		return rep, true, err
	}

	id, err := s.repo.Create(ctx, userID, in, sig)
	if err != nil {
		return nil, false, err
	}

	if len(files) > 0 {
		names, err := s.storage.SaveAll(id, files)
		if err != nil {
			// откатываем — пустой репорт уже не полезен.
			_ = s.repo.Delete(ctx, id)
			return nil, false, err
		}
		if err := s.repo.UpdateScreenshots(ctx, id, names); err != nil {
			return nil, false, err
		}
	}

	rep, err = s.repo.Get(ctx, id)
	if err != nil {
		return nil, false, err
	}

	// Нотификация админам — best-effort.
	if s.notif != nil {
		title := "Новый баг-репорт: " + rep.Title
		msg := "Раздел: " + rep.Section
		if rep.Description != "" {
			msg += "\n\n" + truncate(rep.Description, 400)
		}
		go func() {
			bgCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			_, _ = s.notif.CreateForAllAdmins(bgCtx, notifications.Input{
				Title:      title,
				Message:    msg,
				Type:       notifications.TypeWarning,
				ObjectType: notifications.ObjBugReportNew,
				Data: map[string]any{
					"report_id": rep.ID,
					"section":   rep.Section,
					"url":       "/admin/bugtracker",
				},
			})
		}()
	}

	return rep, false, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
