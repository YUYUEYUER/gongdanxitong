// Package media provides functionality for managing files backed by fs or S3.
package media

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"strings"
	"time"

	"github.com/abhinavxd/libredesk/internal/attachment"
	"github.com/abhinavxd/libredesk/internal/dbutil"
	"github.com/abhinavxd/libredesk/internal/envelope"
	"github.com/abhinavxd/libredesk/internal/image"
	"github.com/abhinavxd/libredesk/internal/media/models"
	"github.com/gabriel-vasile/mimetype"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/knadh/go-i18n"
	"github.com/lib/pq"
	"github.com/volatiletech/null/v9"
	"github.com/zerodha/logf"
)

var (
	//go:embed queries.sql
	efs                   embed.FS
	ErrOwnerQuotaExceeded = errors.New("persistent media owner quota exceeded")
)

const (
	MaxOwnedMediaFiles           = 500
	MaxOwnedMediaBytes           = int64(1 << 30)
	MaxUnlinkedMediaFiles        = int64(50)
	MaxUnlinkedMediaBytes        = int64(100 << 20)
	DefaultMaxInstanceMediaFiles = int64(100_000)
	DefaultMaxInstanceMediaBytes = int64(50 << 30)
	DefaultMinFreeStorageBytes   = int64(1 << 30)
)

// Store defines the interface for media storage operations.
type Store interface {
	Put(name, contentType string, content io.ReadSeeker) (string, error)
	Delete(name string) error
	GetURL(name, disposition, fileName string) string
	GetBlob(name string) ([]byte, error)
	Name() string
	// SignedURLValidator returns a validator function if the store supports signed URLs.
	// Returns nil if the store doesn't use signed URLs (e.g., S3 handles validation itself).
	SignedURLValidator() func(name, sig string, exp int64) bool
}

// CapacityStore is implemented by stores that can report the physical free
// space available to this application. Remote object stores generally enforce
// capacity outside LibreDesk and rely on the database hard quota below.
type CapacityStore interface {
	AvailableBytes() (uint64, error)
}

// ReservedCapacityStore atomically checks a physical free-space reserve and
// writes an object. Local filesystem storage implements this to avoid a
// check-then-write race between concurrent uploads.
type ReservedCapacityStore interface {
	PutWithReserve(name, contentType string, content io.ReadSeeker, minFreeBytes uint64) (string, error)
}

// SignedURLStore defines the interface for stores that support signed URLs.
// This is optional and only implemented by stores that need signed URL functionality (like fs).
type SignedURLStore interface {
	Store
	GetSignedURL(name string) string
}

type Manager struct {
	store            Store
	lo               *logf.Logger
	i18n             *i18n.I18n
	db               *sqlx.DB
	maxInstanceFiles int64
	maxInstanceBytes int64
	minFreeBytes     int64
	queries          queries
}

// Opts provides options for configuring the Manager.
type Opts struct {
	Store             Store
	Lo                *logf.Logger
	DB                *sqlx.DB
	I18n              *i18n.I18n
	MaxInstanceFiles  int64
	MaxInstanceBytes  int64
	MinFreeStoreBytes int64
}

// New initializes and returns a new Manager instance for handling media operations.
func New(opt Opts) (*Manager, error) {
	var q queries
	if err := dbutil.ScanSQLFile("queries.sql", &q, opt.DB, efs); err != nil {
		return nil, err
	}
	if opt.MaxInstanceFiles <= 0 {
		opt.MaxInstanceFiles = DefaultMaxInstanceMediaFiles
	}
	if opt.MaxInstanceBytes <= 0 {
		opt.MaxInstanceBytes = DefaultMaxInstanceMediaBytes
	}
	if opt.MinFreeStoreBytes <= 0 {
		opt.MinFreeStoreBytes = DefaultMinFreeStorageBytes
	}
	return &Manager{
		store:            opt.Store,
		lo:               opt.Lo,
		i18n:             opt.I18n,
		db:               opt.DB,
		maxInstanceFiles: opt.MaxInstanceFiles,
		maxInstanceBytes: opt.MaxInstanceBytes,
		minFreeBytes:     opt.MinFreeStoreBytes,
		queries:          q,
	}, nil
}

// queries holds the prepared SQL statements.
type queries struct {
	Insert                *sqlx.Stmt `query:"insert-media"`
	Get                   *sqlx.Stmt `query:"get-media"`
	GetByUUID             *sqlx.Stmt `query:"get-media-by-uuid"`
	Delete                *sqlx.Stmt `query:"delete-media"`
	Attach                *sqlx.Stmt `query:"attach-to-model"`
	GetByModel            *sqlx.Stmt `query:"get-model-media"`
	GetUnlinkedMedia      *sqlx.Stmt `query:"get-unlinked-media"`
	GetUnlinkedMediaUsage *sqlx.Stmt `query:"get-unlinked-media-usage"`
	GetOwnedMediaUsage    *sqlx.Stmt `query:"get-owned-media-usage"`
	GetGlobalMediaUsage   *sqlx.Stmt `query:"get-global-media-usage"`
	LockMediaOwner        *sqlx.Stmt `query:"lock-media-owner"`
	LockGlobalMediaQuota  *sqlx.Stmt `query:"lock-global-media-quota"`
	ContentIDExists       *sqlx.Stmt `query:"content-id-exists"`
	GetByContentIDs       *sqlx.Stmt `query:"get-media-by-content-ids"`
	SetContentID          *sqlx.Stmt `query:"set-media-content-id"`
	SetThumbnailSize      *sqlx.Stmt `query:"set-media-thumbnail-size"`
}

// UploadAndInsert uploads file on storage and inserts an entry in db.
func (m *Manager) UploadAndInsert(srcFilename, contentType, contentID string, modelType null.String, modelID, ownerUserID null.Int, content io.ReadSeeker, fileSize int, disposition null.String, meta []byte) (models.Media, error) {
	var (
		uuid = uuid.New()
		err  error
	)

	// Override content type after upload (in case it was detected incorrectly).
	_, contentType, err = m.Upload(uuid.String(), contentType, content)
	if err != nil {
		return models.Media{}, err
	}
	thumbnailSize, hasThumbnailSize := thumbnailSizeFromMeta(meta)
	if strings.HasPrefix(contentType, "image/") {
		if !hasThumbnailSize {
			meta = withThumbnailSizeMeta(meta, 0)
			thumbnailSize = 0
		}
	} else {
		thumbnailSize = 0
	}

	media, err := m.Insert(disposition, srcFilename, contentType, contentID, modelType, uuid.String(), modelID, ownerUserID, fileSize, int64(fileSize)+thumbnailSize, meta)
	if err != nil {
		m.store.Delete(uuid.String())
		return models.Media{}, err
	}
	return media, nil
}

func thumbnailSizeFromMeta(meta []byte) (int64, bool) {
	var values struct {
		ThumbnailSize *int64 `json:"thumbnail_size"`
	}
	if len(meta) == 0 || json.Unmarshal(meta, &values) != nil || values.ThumbnailSize == nil ||
		*values.ThumbnailSize < 0 || *values.ThumbnailSize > image.ThumbnailStorageReserveBytes {
		return 0, false
	}
	return *values.ThumbnailSize, true
}

func withThumbnailSizeMeta(meta []byte, thumbnailSize int64) []byte {
	values := make(map[string]any)
	if len(meta) > 0 {
		_ = json.Unmarshal(meta, &values)
	}
	values["thumbnail_size"] = thumbnailSize
	encoded, err := json.Marshal(values)
	if err != nil {
		return []byte(`{"thumbnail_size":0}`)
	}
	return encoded
}

// Upload saves the media file to the storage backend - returns the generated filename and content type (after detection).
func (m *Manager) Upload(fileName, contentType string, content io.ReadSeeker) (string, string, error) {
	// On store file is named by UUID to avoid collisions and the actual filename is stored in DB.
	m.lo.Debug("detecting content type for file before upload", "uuid", fileName, "source_content_type", contentType)

	// Detect content type and override if needed.
	contentType, err := m.detectContentType(contentType, content)
	if err != nil {
		m.lo.Error("error detecting content type", "error", err, "file_name", fileName, "content_type", contentType, "store", m.store.Name())
		return "", "", err
	}
	contentSize, err := content.Seek(0, io.SeekEnd)
	if err != nil {
		return "", "", envelope.NewError(envelope.GeneralError, m.i18n.T("globals.messages.errorUploadingFile"), nil)
	}
	if _, err := content.Seek(0, io.SeekStart); err != nil {
		return "", "", envelope.NewError(envelope.GeneralError, m.i18n.T("globals.messages.errorUploadingFile"), nil)
	}
	if capacityStore, ok := m.store.(CapacityStore); ok {
		available, err := capacityStore.AvailableBytes()
		if err != nil {
			m.lo.Error("error checking media store capacity", "error", err, "store", m.store.Name())
			return "", "", envelope.NewError(envelope.GeneralError, m.i18n.T("globals.messages.errorUploadingFile"), nil)
		}
		reserve := uint64(m.minFreeBytes)
		required := uint64(contentSize)
		if available <= reserve || required > available-reserve {
			return "", "", envelope.NewErrorWithCode(envelope.GeneralError, 507, "Media storage capacity reached", nil)
		}
	}

	var fName string
	if reservedStore, ok := m.store.(ReservedCapacityStore); ok {
		fName, err = reservedStore.PutWithReserve(fileName, contentType, content, uint64(m.minFreeBytes))
	} else {
		fName, err = m.store.Put(fileName, contentType, content)
	}
	if err != nil {
		m.lo.Error("error uploading media to store", "error", err, "file_name", fileName, "content_type", contentType, "store", m.store.Name())
		return "", "", envelope.NewError(envelope.GeneralError, m.i18n.T("globals.messages.errorUploadingFile"), nil)
	}
	return fName, contentType, nil
}

// Insert inserts media details into the database and returns the inserted media record.
func (m *Manager) Insert(disposition null.String, fileName, contentType, contentID string, modelType null.String, uuid string, modelID, ownerUserID null.Int, fileSize int, storageSize int64, meta []byte) (models.Media, error) {
	if fileSize < 0 || storageSize < int64(fileSize) || !ownerUserID.Valid || ownerUserID.Int <= 0 {
		return models.Media{}, envelope.NewError(envelope.InputError, m.i18n.T("globals.messages.badRequest"), nil)
	}
	return m.insertWithQuotas(disposition, fileName, contentType, contentID, modelType, uuid, modelID, ownerUserID, fileSize, storageSize, meta)
}

func (m *Manager) insertWithQuotas(disposition null.String, fileName, contentType, contentID string, modelType null.String, uuid string, modelID, ownerUserID null.Int, fileSize int, storageSize int64, meta []byte) (models.Media, error) {
	tx, err := m.db.Beginx()
	if err != nil {
		m.lo.Error("error beginning media quota transaction", "error", err)
		return models.Media{}, envelope.NewError(envelope.GeneralError, m.i18n.T("globals.messages.somethingWentWrong"), nil)
	}
	defer tx.Rollback()

	if _, err := tx.Stmtx(m.queries.LockGlobalMediaQuota).Exec(); err != nil {
		m.lo.Error("error locking global media quota", "error", err)
		return models.Media{}, envelope.NewError(envelope.GeneralError, m.i18n.T("globals.messages.somethingWentWrong"), nil)
	}
	var globalCount, globalBytes int64
	if err := tx.Stmtx(m.queries.GetGlobalMediaUsage).QueryRow().Scan(&globalCount, &globalBytes); err != nil {
		m.lo.Error("error fetching global media usage", "error", err)
		return models.Media{}, envelope.NewError(envelope.GeneralError, m.i18n.T("globals.messages.somethingWentWrong"), nil)
	}
	storageObjects := int64(1)
	if storageSize > int64(fileSize) {
		storageObjects++
	}
	if !withinMediaQuota(globalCount, globalBytes, storageObjects, storageSize, m.maxInstanceFiles, m.maxInstanceBytes) {
		return models.Media{}, envelope.NewErrorWithCode(envelope.InputError, 413, "Instance media storage quota exceeded", nil)
	}

	if ownerUserID.Valid && ownerUserID.Int > 0 {
		var lockedOwnerID int
		if err := tx.Stmtx(m.queries.LockMediaOwner).Get(&lockedOwnerID, ownerUserID.Int); err != nil {
			m.lo.Warn("invalid media owner", "owner_user_id", ownerUserID.Int, "error", err)
			return models.Media{}, envelope.NewError(envelope.PermissionError, m.i18n.T("status.deniedPermission"), nil)
		}
		var count, usedBytes int64
		if err := tx.Stmtx(m.queries.GetOwnedMediaUsage).QueryRow(ownerUserID.Int).Scan(&count, &usedBytes); err != nil {
			m.lo.Error("error fetching persistent media usage", "owner_user_id", ownerUserID.Int, "error", err)
			return models.Media{}, envelope.NewError(envelope.GeneralError, m.i18n.T("globals.messages.somethingWentWrong"), nil)
		}
		if !withinOwnedMediaQuota(count, usedBytes, storageSize) {
			return models.Media{}, envelope.NewErrorWithCode(envelope.InputError, 413, "Persistent upload quota exceeded", nil)
		}
		if !modelID.Valid || modelID.Int <= 0 {
			var unlinkedCount, unlinkedBytes int64
			if err := tx.Stmtx(m.queries.GetUnlinkedMediaUsage).QueryRow(ownerUserID.Int).Scan(&unlinkedCount, &unlinkedBytes); err != nil {
				m.lo.Error("error fetching unlinked media usage", "owner_user_id", ownerUserID.Int, "error", err)
				return models.Media{}, envelope.NewError(envelope.GeneralError, m.i18n.T("globals.messages.somethingWentWrong"), nil)
			}
			if !withinMediaQuota(unlinkedCount, unlinkedBytes, 1, storageSize, MaxUnlinkedMediaFiles, MaxUnlinkedMediaBytes) {
				return models.Media{}, envelope.NewErrorWithCode(envelope.InputError, 413, "Unattached upload quota exceeded", nil)
			}
		}
	}

	var id int
	if err := tx.Stmtx(m.queries.Insert).QueryRow(m.store.Name(), fileName, contentType, fileSize, meta, modelID, modelType, disposition, contentID, uuid, ownerUserID).Scan(&id); err != nil {
		m.lo.Error("error inserting media", "error", err, "file_name", fileName, "owner_user_id", ownerUserID.Int)
		return models.Media{}, envelope.NewError(envelope.GeneralError, m.i18n.T("globals.messages.somethingWentWrong"), nil)
	}
	if err := tx.Commit(); err != nil {
		m.lo.Error("error committing media quota transaction", "error", err)
		return models.Media{}, envelope.NewError(envelope.GeneralError, m.i18n.T("globals.messages.somethingWentWrong"), nil)
	}
	return m.Get(id, "")
}

// GetMany fetches multiple media records by their IDs.
func (m *Manager) GetMany(ids []int) ([]models.Media, error) {
	out := make([]models.Media, 0, len(ids))
	for _, id := range ids {
		med, err := m.Get(id, "")
		if err != nil {
			return nil, err
		}
		out = append(out, med)
	}
	return out, nil
}

// GetUnlinkedUsage returns the number and total bytes of uploads owned by a
// user that have not yet been attached to a model.
func (m *Manager) GetUnlinkedUsage(ownerUserID int) (int64, int64, error) {
	var count, bytes int64
	if ownerUserID <= 0 {
		return 0, 0, fmt.Errorf("invalid media owner")
	}
	if err := m.queries.GetUnlinkedMediaUsage.QueryRow(ownerUserID).Scan(&count, &bytes); err != nil {
		m.lo.Error("error fetching unlinked media usage", "owner_user_id", ownerUserID, "error", err)
		return 0, 0, envelope.NewError(envelope.GeneralError, m.i18n.T("globals.messages.somethingWentWrong"), nil)
	}
	return count, bytes, nil
}

// GetOwnedUsage includes linked and unlinked media so attaching a file never
// resets the owner's persistent storage budget.
func (m *Manager) GetOwnedUsage(ownerUserID int) (int64, int64, error) {
	var count, bytes int64
	if ownerUserID <= 0 {
		return 0, 0, fmt.Errorf("invalid media owner")
	}
	if err := m.queries.GetOwnedMediaUsage.QueryRow(ownerUserID).Scan(&count, &bytes); err != nil {
		m.lo.Error("error fetching persistent media usage", "owner_user_id", ownerUserID, "error", err)
		return 0, 0, envelope.NewError(envelope.GeneralError, m.i18n.T("globals.messages.somethingWentWrong"), nil)
	}
	return count, bytes, nil
}

func (m *Manager) CanStoreForOwner(ownerUserID int, nextBytes int64) (bool, error) {
	globalCount, globalBytes, err := m.GetGlobalUsage()
	if err != nil {
		return false, err
	}
	if !withinMediaQuota(globalCount, globalBytes, 1, nextBytes, m.maxInstanceFiles, m.maxInstanceBytes) {
		return false, nil
	}
	count, usedBytes, err := m.GetOwnedUsage(ownerUserID)
	if err != nil {
		return false, err
	}
	return withinOwnedMediaQuota(count, usedBytes, nextBytes), nil
}

// GetGlobalUsage includes rows with no owner so e-mail and rotating anonymous
// identities cannot bypass the instance storage boundary.
func (m *Manager) GetGlobalUsage() (int64, int64, error) {
	var count, bytes int64
	if err := m.queries.GetGlobalMediaUsage.QueryRow().Scan(&count, &bytes); err != nil {
		m.lo.Error("error fetching global media usage", "error", err)
		return 0, 0, envelope.NewError(envelope.GeneralError, m.i18n.T("globals.messages.somethingWentWrong"), nil)
	}
	return count, bytes, nil
}

func withinMediaQuota(count, usedBytes, nextFiles, nextBytes, maxFiles, maxBytes int64) bool {
	if count < 0 || usedBytes < 0 || nextFiles <= 0 || nextBytes < 0 || maxFiles <= 0 || maxBytes <= 0 || count > maxFiles || usedBytes > maxBytes {
		return false
	}
	return nextFiles <= maxFiles-count && nextBytes <= maxBytes-usedBytes
}

func withinOwnedMediaQuota(count, usedBytes, nextBytes int64) bool {
	if count < 0 || usedBytes < 0 || nextBytes < 0 || count >= MaxOwnedMediaFiles || usedBytes > MaxOwnedMediaBytes {
		return false
	}
	return nextBytes <= MaxOwnedMediaBytes-usedBytes
}

// OwnedMediaUsageWithinLimits is used when ownership is transferred without
// creating a new file, such as visitor-to-contact merges.
func OwnedMediaUsageWithinLimits(count, usedBytes int64) bool {
	return count >= 0 && usedBytes >= 0 && count <= MaxOwnedMediaFiles && usedBytes <= MaxOwnedMediaBytes
}

// Get retrieves the media record by its ID and returns the media.
func (m *Manager) Get(id int, uuid string) (models.Media, error) {
	var media models.Media
	if err := m.queries.Get.Get(&media, id, uuid); err != nil {
		if err == sql.ErrNoRows {
			return media, envelope.NewError(envelope.NotFoundError, m.i18n.T("validation.notFoundMedia"), nil)
		}
		m.lo.Error("error fetching media", "error", err)
		return media, envelope.NewError(envelope.GeneralError, m.i18n.T("globals.messages.somethingWentWrong"), nil)
	}
	media.URL, media.DownloadURL, media.ThumbnailURL = m.GetAttachmentURLs(media.UUID, media.ContentType, media.Filename)
	if !ThumbnailAvailable(media.Meta, media.ContentType) {
		media.ThumbnailURL = ""
	}
	return media, nil
}

// GetAttachmentURLs returns separately signed preview, download and thumbnail
// URLs. Callers must not derive one storage URL from another because both FS
// and S3 signatures bind the object name and/or response parameters.
func (m *Manager) GetAttachmentURLs(uuid, contentType, fileName string) (string, string, string) {
	previewURL := m.GetURL(uuid, contentType, fileName)
	downloadURL := m.GetURLForDownload(uuid, fileName)
	thumbnailURL := ""
	if isThumbnailMediaContentType(contentType) {
		thumbnailURL = m.store.GetURL(image.ThumbPrefix+uuid, "inline", "thumbnail-"+fileName)
	}
	return previewURL, downloadURL, thumbnailURL
}

func isThumbnailMediaContentType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	switch strings.ToLower(mediaType) {
	case "image/png", "image/jpeg", "image/gif":
		return true
	default:
		return false
	}
}

func (m *Manager) DecorateAttachment(item *attachment.Attachment) {
	if item == nil || item.UUID == "" {
		return
	}
	item.URL, item.DownloadURL, item.ThumbnailURL = m.GetAttachmentURLs(item.UUID, item.ContentType, item.Name)
	if !item.ThumbnailAvailable {
		item.ThumbnailURL = ""
	}
}

func ThumbnailAvailable(meta json.RawMessage, contentType string) bool {
	var values struct {
		ThumbnailSize *int64 `json:"thumbnail_size"`
	}
	if json.Unmarshal(meta, &values) == nil && values.ThumbnailSize != nil {
		return *values.ThumbnailSize > 0 && *values.ThumbnailSize <= image.ThumbnailStorageReserveBytes
	}
	return isThumbnailMediaContentType(contentType)
}

func (m *Manager) SetThumbnailSize(id int, thumbnailSize int64) error {
	if id <= 0 || thumbnailSize < 0 || thumbnailSize > image.ThumbnailStorageReserveBytes {
		return fmt.Errorf("invalid thumbnail accounting update")
	}
	if _, err := m.queries.SetThumbnailSize.Exec(id, thumbnailSize); err != nil {
		return fmt.Errorf("updating thumbnail accounting: %w", err)
	}
	return nil
}

// SetContentID stamps a content_id onto a media row if one isn't already set.
func (m *Manager) SetContentID(id int, contentID string) error {
	if _, err := m.queries.SetContentID.Exec(id, contentID); err != nil {
		m.lo.Error("error setting media content_id", "id", id, "content_id", contentID, "error", err)
		return fmt.Errorf("setting media content_id: %w", err)
	}
	return nil
}

// ContentIDExists reports whether a media row with the given content_id is linked to a message in the given conversation. Scoped this way so an orphan media row (e.g., from a partial failure) doesn't short-circuit a retry into skipping the upload.
func (m *Manager) ContentIDExists(contentID, conversationUUID string) (bool, string, error) {
	if contentID == "" || conversationUUID == "" {
		return false, "", nil
	}
	var uuid string
	if err := m.queries.ContentIDExists.Get(&uuid, contentID, conversationUUID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, "", nil
		}
		m.lo.Error("error checking if content_id exists", "error", err)
		return false, "", fmt.Errorf("checking if content_id exists: %w", err)
	}
	return true, uuid, nil
}

// GetByContentIDs returns media rows matching any of the given content_ids, scoped to the given conversation to prevent cross-conversation lookup.
func (m *Manager) GetByContentIDs(contentIDs []string, conversationUUID string) ([]models.Media, error) {
	out := []models.Media{}
	if len(contentIDs) == 0 || conversationUUID == "" {
		return out, nil
	}
	if err := m.queries.GetByContentIDs.Select(&out, pq.Array(contentIDs), conversationUUID); err != nil {
		m.lo.Error("error fetching media by content_ids", "error", err)
		return nil, fmt.Errorf("fetching media by content_ids: %w", err)
	}
	return out, nil
}

// GetBlob retrieves the raw binary content of a media file by its name.
func (m *Manager) GetBlob(name string) ([]byte, error) {
	return m.store.GetBlob(name)
}

// GetURL returns the URL for accessing a media file by its name.
func (m *Manager) GetURL(uuid, contentType, fileName string) string {
	disposition := "attachment"
	if safeInlineMediaContentType(contentType) {
		disposition = "inline"
	}
	return m.store.GetURL(uuid, disposition, fileName)
}

func safeInlineMediaContentType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	switch strings.ToLower(mediaType) {
	case "image/png", "image/jpeg", "image/gif",
		"video/mp4", "video/webm", "video/ogg", "application/pdf":
		return true
	default:
		return false
	}
}

func (m *Manager) GetURLForDownload(uuid, fileName string) string {
	return m.store.GetURL(uuid, "attachment", fileName)
}

// GetSignedURL generates a signed URL for secure media access if the store supports it.
// Returns a regular URL if the store doesn't support signed URLs.
func (m *Manager) GetSignedURL(name string) string {
	if signedStore, ok := m.store.(SignedURLStore); ok {
		return signedStore.GetSignedURL(name)
	}
	// Fallback to regular URL if signed URLs not supported
	return m.GetURL(name, "", "")
}

// SignedURLValidator returns the store's signature validator if available.
// Returns nil if the store doesn't support signed URL validation.
func (m *Manager) SignedURLValidator() func(name, sig string, exp int64) bool {
	return m.store.SignedURLValidator()
}

// Attach associates a media file with a specific model by its ID and model name.
func (m *Manager) Attach(id int, model string, modelID int) error {
	if _, err := m.queries.Attach.Exec(id, model, modelID); err != nil {
		m.lo.Error("error attaching media to model", "model", model, "model_id", modelID, "media_id", id, "error", err)
		return fmt.Errorf("attaching media;%d to model:%s model_id:%d: %w", id, model, modelID, err)
	}
	return nil
}

// GetByModel retrieves all media files attached to a specific model.
func (m *Manager) GetByModel(modelID int, model string) ([]models.Media, error) {
	var media = make([]models.Media, 0)
	if err := m.queries.GetByModel.Select(&media, model, modelID); err != nil {
		m.lo.Error("error getting model media", "model", model, "model_id", modelID, "error", err)
		return nil, fmt.Errorf("fetching media for model:%s model_id:%d: %w", model, modelID, err)
	}
	return media, nil
}

// Delete deletes a media file from both the storage backend and the database.
func (m *Manager) Delete(name string) error {
	if err := m.store.Delete(name); err != nil {
		m.lo.Error("error deleting media from store", "error", err)
		// If the file does not exist, ignore the error.
		if !errors.Is(err, os.ErrNotExist) {
			return envelope.NewError(envelope.GeneralError, m.i18n.T("globals.messages.somethingWentWrong"), nil)
		}
	}

	// Thumbnail files do not exist in the database, only in the storage backend, so return early.
	if strings.HasPrefix(name, image.ThumbPrefix) {
		return nil
	}

	// Delete the media record from the database.
	if _, err := m.queries.Delete.Exec(name); err != nil {
		m.lo.Error("error deleting media from db", "error", err)
		return envelope.NewError(envelope.GeneralError, m.i18n.T("globals.messages.somethingWentWrong"), nil)
	}
	return nil
}

// DeleteUnlinkedMedia periodically deletes stale media not linked to any model.
func (m *Manager) DeleteUnlinkedMedia(ctx context.Context) {
	m.deleteUnlinkedMedia()
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(12 * time.Hour):
			m.lo.Info("starting periodic deletion of unlinked media")
			if err := m.deleteUnlinkedMedia(); err != nil {
				m.lo.Error("error deleting unlinked media", "error", err)
			}
		}
	}
}

// deleteUnlinkedMedia fetches all stale media not linked to any model.
func (m *Manager) deleteUnlinkedMedia() error {
	var media []models.Media
	if err := m.queries.GetUnlinkedMedia.Select(&media); err != nil {
		m.lo.Error("error fetching unlinked media", "error", err)
		return err
	}
	for _, mm := range media {
		m.lo.Debug("deleting media not linked to any message", "media_id", mm.ID)
		if err := m.Delete(mm.UUID); err != nil {
			m.lo.Error("error deleting unlinked media", "error", err)
			continue
		}

		// If it's an image, also delete the `thumb_uuid` image from store.
		if strings.HasPrefix(mm.ContentType, "image/") {
			thumbUUID := image.ThumbPrefix + mm.UUID
			m.lo.Debug("deleting thumbnail for unlinked media", "thumb_uuid", thumbUUID)
			if err := m.Delete(thumbUUID); err != nil {
				m.lo.Error("error deleting thumbnail for unlinked media", "error", err)
			}
		}
	}
	return nil
}

// detectContentType detects the content type from bytes. The source value is
// used only for diagnostics because it is controlled by the uploader.
func (m *Manager) detectContentType(sourceContentType string, content io.ReadSeeker) (string, error) {
	detected, err := sniffContentType(content)
	if err != nil {
		m.lo.Error("error detecting content type", "error", err)
		return "", err
	}
	m.lo.Debug("detected media content type from content", "detected_type", detected, "source_type", sourceContentType)
	return detected, nil
}

func sniffContentType(content io.ReadSeeker) (string, error) {
	if _, err := content.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("seeking media: %w", err)
	}
	defer content.Seek(0, io.SeekStart)
	mtype, err := mimetype.DetectReader(content)
	if err != nil {
		return "", fmt.Errorf("detecting media type: %w", err)
	}
	if detected := mtype.String(); detected != "" {
		return detected, nil
	}
	return "application/octet-stream", nil
}
