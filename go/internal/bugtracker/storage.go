package bugtracker

import (
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

const (
	maxScreenshots = 8
	maxFileBytes   = 5 * 1024 * 1024 // 5 МБ
)

// allowedMime: значение → расширение для сохранения на диск.
var allowedMime = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/webp": ".webp",
	"image/gif":  ".gif",
}

// Storage — сохраняет скриншоты репортов в каталог UploadsDir/{report_id}/.
// Возвращает относительные пути (без UploadsDir), которые потом отдаются
// HTTP-handler'ом /uploads/bugtracker/{id}/{file}.
type Storage struct {
	UploadsDir string // абсолютный путь, например /var/lib/finance/uploads/bugtracker
}

func NewStorage(dir string) *Storage {
	if dir == "" {
		dir = "/var/lib/finance/uploads/bugtracker"
	}
	return &Storage{UploadsDir: dir}
}

// SaveAll — пишет загруженные файлы в UploadsDir/{reportID}/{uuid}.{ext}.
// Возвращает список имён файлов (без префикса каталога) в порядке загрузки.
// Не сохраняет ничего, если есть нарушение лимитов/типа — возвращает ошибку,
// чтобы вызывающий код мог откатить транзакцию репорта.
func (s *Storage) SaveAll(reportID int64, files []*multipart.FileHeader) ([]string, error) {
	if len(files) > maxScreenshots {
		return nil, fmt.Errorf("too many files (max %d)", maxScreenshots)
	}
	for _, fh := range files {
		if fh.Size > maxFileBytes {
			return nil, fmt.Errorf("file %q is too large (max %d bytes)", fh.Filename, maxFileBytes)
		}
	}

	dir := filepath.Join(s.UploadsDir, fmt.Sprintf("%d", reportID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	names := make([]string, 0, len(files))
	for _, fh := range files {
		ext, err := s.detectAndValidate(fh)
		if err != nil {
			return nil, err
		}
		name := uuid.NewString() + ext
		if err := s.saveOne(filepath.Join(dir, name), fh); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, nil
}

func (s *Storage) detectAndValidate(fh *multipart.FileHeader) (string, error) {
	// Сначала смотрим Content-Type, заявленный клиентом.
	ct := fh.Header.Get("Content-Type")
	if ext, ok := allowedMime[strings.ToLower(ct)]; ok {
		return ext, nil
	}
	// Если клиент соврал — sniff'ом по первым 512 байтам.
	f, err := fh.Open()
	if err != nil {
		return "", err
	}
	defer f.Close()
	head := make([]byte, 512)
	n, _ := io.ReadFull(f, head)
	sniffed := http.DetectContentType(head[:n])
	if ext, ok := allowedMime[strings.ToLower(sniffed)]; ok {
		return ext, nil
	}
	return "", errors.New("unsupported file type: " + ct + "/" + sniffed)
}

func (s *Storage) saveOne(fullPath string, fh *multipart.FileHeader) error {
	src, err := fh.Open()
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenFile(fullPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer dst.Close()
	if _, err := io.Copy(dst, src); err != nil {
		_ = os.Remove(fullPath)
		return err
	}
	return nil
}

// Open — отдать файл по имени для GET /uploads/bugtracker/{id}/{file}.
// Защита от path traversal: пути склеиваем по filepath.Clean + проверяем префикс.
func (s *Storage) Open(reportID int64, name string) (*os.File, error) {
	dir := filepath.Join(s.UploadsDir, fmt.Sprintf("%d", reportID))
	full := filepath.Clean(filepath.Join(dir, name))
	if !strings.HasPrefix(full, filepath.Clean(dir)+string(os.PathSeparator)) {
		return nil, errors.New("forbidden")
	}
	return os.Open(full)
}
