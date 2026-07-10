package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"slices"

	"github.com/abhinavxd/libredesk/internal/attachment"
	amodels "github.com/abhinavxd/libredesk/internal/auth/models"
	"github.com/abhinavxd/libredesk/internal/envelope"
	"github.com/abhinavxd/libredesk/internal/image"
	mmodels "github.com/abhinavxd/libredesk/internal/media/models"
	"github.com/abhinavxd/libredesk/internal/stringutil"
	"github.com/google/uuid"
	"github.com/valyala/fasthttp"
	"github.com/volatiletech/null/v9"
	"github.com/zerodha/fastglue"
)

const (
	maxUnlinkedUploadsPerUser = 50
	maxUnlinkedUploadBytes    = int64(100 << 20)
	mediaUploadWindow         = 10 * time.Minute
	maxMediaUploadsPerWindow  = 30
)

// handleMediaUpload handles media uploads.
func handleMediaUpload(r *fastglue.Request) error {
	var (
		app         = r.Context.(*App)
		cleanUp     = false
		ownerUserID null.Int
	)

	if user, ok := r.RequestCtx.UserValue("user").(amodels.User); ok && user.ID > 0 {
		ownerUserID = null.IntFrom(user.ID)
	}
	if !ownerUserID.Valid {
		return r.SendErrorEnvelope(fasthttp.StatusUnauthorized, app.i18n.T("status.deniedPermission"), nil, envelope.UnauthorizedError)
	}

	limit, err := app.rateLimit.CheckWindow(r.RequestCtx, fmt.Sprintf("media_upload:user:%d", ownerUserID.Int), mediaUploadWindow, maxMediaUploadsPerWindow)
	if err != nil {
		app.lo.Error("media upload rate limit unavailable", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusServiceUnavailable, app.i18n.T("globals.messages.somethingWentWrong"), nil, envelope.GeneralError)
	}
	if !limit.Allowed {
		r.RequestCtx.Response.Header.Set("Retry-After", strconv.Itoa(int(limit.RetryAfter.Seconds())))
		return r.SendErrorEnvelope(fasthttp.StatusTooManyRequests, "Too many uploads", nil, envelope.InputError)
	}

	form, err := r.RequestCtx.MultipartForm()
	if err != nil {
		app.lo.Error("error parsing form data.", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, app.i18n.T("errors.parsingRequest"), nil, envelope.GeneralError)
	}

	files, ok := form.File["files"]
	if !ok || len(files) == 0 {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, app.i18n.T("validation.notFoundFile"), nil, envelope.InputError)
	}

	fileHeader := files[0]
	file, err := fileHeader.Open()
	if err != nil {
		app.lo.Error("error reading uploaded file", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, app.i18n.T("globals.messages.somethingWentWrong"), nil, envelope.GeneralError)
	}
	defer file.Close()

	// Inline?
	var disposition = null.StringFrom(attachment.DispositionAttachment)
	inline, ok := form.Value["inline"]
	if ok && len(inline) > 0 && inline[0] == "true" {
		disposition = null.StringFrom(attachment.DispositionInline)
	}

	// Sanitize filename.
	srcFileName := stringutil.SanitizeFilename(fileHeader.Filename)
	srcContentType := fileHeader.Header.Get("Content-Type")
	srcFileSize := fileHeader.Size
	srcExt := strings.TrimPrefix(strings.ToLower(filepath.Ext(srcFileName)), ".")

	// Check if file is empty
	if srcFileSize == 0 {
		app.lo.Error("error: uploaded file is empty (0 bytes)")
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, app.i18n.T("media.fileEmpty"), nil, envelope.InputError)
	}

	// Check file size
	consts := app.consts.Load().(*constants)
	if bytesToMegabytes(srcFileSize) > float64(consts.MaxFileUploadSizeMB) {
		app.lo.Error("error: uploaded file size is larger than max allowed", "size", bytesToMegabytes(srcFileSize), "max_allowed", consts.MaxFileUploadSizeMB)
		return r.SendErrorEnvelope(
			fasthttp.StatusRequestEntityTooLarge,
			app.i18n.Ts("media.fileSizeTooLarge", "size", fmt.Sprintf("%dMB", consts.MaxFileUploadSizeMB)),
			nil,
			envelope.GeneralError,
		)
	}

	unlinkedCount, unlinkedBytes, err := app.media.GetUnlinkedUsage(ownerUserID.Int)
	if err != nil {
		return sendErrorEnvelope(r, err)
	}
	if !withinUnlinkedUploadQuota(unlinkedCount, unlinkedBytes, srcFileSize) {
		return r.SendErrorEnvelope(fasthttp.StatusRequestEntityTooLarge, "Unattached upload quota exceeded", nil, envelope.InputError)
	}
	canStore, err := app.media.CanStoreForOwner(ownerUserID.Int, srcFileSize)
	if err != nil {
		return sendErrorEnvelope(r, err)
	}
	if !canStore {
		return r.SendErrorEnvelope(fasthttp.StatusRequestEntityTooLarge, "Persistent upload quota exceeded", nil, envelope.InputError)
	}

	if !slices.Contains(consts.AllowedUploadFileExtensions, "*") && !slices.Contains(consts.AllowedUploadFileExtensions, srcExt) {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, app.i18n.T("media.fileTypeNotAllowed"), nil, envelope.InputError)
	}

	// Delete files on any error.
	var uuid = uuid.New()
	thumbName := image.ThumbPrefix + uuid.String()
	defer func() {
		if cleanUp {
			if err := app.media.Delete(uuid.String()); err != nil {
				app.lo.Error("error cleaning up uploaded media", "uuid", uuid.String(), "error", err)
			}
			if err := app.media.Delete(thumbName); err != nil {
				app.lo.Error("error cleaning up uploaded thumbnail", "uuid", thumbName, "error", err)
			}
		}
	}()

	// Generate and upload thumbnail and store image dimensions in the media meta.
	var (
		meta          = []byte("{}")
		thumbnailSize int64
	)
	if image.IsImageByContent(file) {
		file.Seek(0, 0)
		thumbFile, width, height, err := image.CreateThumbWithDimensions(image.DefThumbSize, file)
		if err != nil && !errors.Is(err, image.ErrThumbnailBusy) {
			app.lo.Error("error creating thumb image", "error", err)
			return r.SendErrorEnvelope(fasthttp.StatusBadRequest, app.i18n.T("media.fileTypeNotAllowed"), nil, envelope.InputError)
		}
		if thumbFile != nil {
			thumbnailSize = thumbFile.Size()
			thumbName, _, err = app.media.Upload(thumbName, srcContentType, thumbFile)
			if err != nil {
				return sendErrorEnvelope(r, err)
			}
		} else {
			app.lo.Warn("thumbnail capacity reached; storing original without thumbnail")
		}

		meta, _ = json.Marshal(map[string]interface{}{
			"width":          width,
			"height":         height,
			"thumbnail_size": thumbnailSize,
		})
	}

	// Reset ptr.
	file.Seek(0, 0)

	// Override content type after upload (in case it was detected incorrectly).
	_, srcContentType, err = app.media.Upload(uuid.String(), srcContentType, file)
	if err != nil {
		cleanUp = true
		app.lo.Error("error uploading file", "error", err)
		return sendErrorEnvelope(r, err)
	}

	// Insert in DB.
	media, err := app.media.Insert(
		disposition,
		srcFileName,
		srcContentType,
		"", /**content_id**/
		null.StringFrom(mmodels.ModelMessages),
		uuid.String(),
		null.Int{}, /**model_id**/
		ownerUserID,
		int(srcFileSize),
		srcFileSize+thumbnailSize,
		meta,
	)
	if err != nil {
		cleanUp = true
		app.lo.Error("error inserting metadata into database", "error", err)
		return sendErrorEnvelope(r, err)
	}
	return r.SendEnvelope(media)
}

// handleServeMedia serves uploaded media.
// Supports both authenticated access (with permission checks) and signed URL access (no permission checks).
func handleServeMedia(r *fastglue.Request) error {
	var (
		app        = r.Context.(*App)
		uuid       = r.RequestCtx.UserValue("uuid").(string)
		authMethod = r.RequestCtx.UserValue("auth_method")
	)

	// If accessed via signed URL, skip permission checks and serve file directly.
	if authMethod == "signed_url" {
		return serveMediaFile(r, app, uuid, nil)
	}

	// Session/API key authenticated - perform full permission check.
	auser := r.RequestCtx.UserValue("user").(amodels.User)

	user, err := app.user.GetAgentCachedOrLoad(auser.ID)
	if err != nil {
		return sendErrorEnvelope(r, err)
	}

	// Fetch media from DB.
	media, err := getMediaByUUID(app, uuid)
	if err != nil {
		return sendErrorEnvelope(r, err)
	}

	// Check if the user has permission to access the linked model.
	allowed, err := app.authz.EnforceMediaAccess(user, media.Model.String)
	if err != nil {
		return sendErrorEnvelope(r, err)
	}

	// For messages, check access to the conversation this message is part of.
	// Skip if model_id is not set (media uploaded but not yet attached to a message).
	if media.Model.String == mmodels.ModelMessages && media.ModelID.Int > 0 {
		conversation, err := app.conversation.GetConversationByMessageID(media.ModelID.Int)
		if err != nil {
			return sendErrorEnvelope(r, err)
		}
		allowed, err = app.authz.EnforceConversationAccess(user, conversation)
		if err != nil {
			return sendErrorEnvelope(r, err)
		}
	}

	if !allowed {
		return r.SendErrorEnvelope(http.StatusUnauthorized, app.i18n.T("status.deniedPermission"), nil, envelope.UnauthorizedError)
	}

	return serveMediaFile(r, app, uuid, &media)
}

// serveMediaFile serves the actual file content based on the storage provider.
// If media is nil, it will be fetched from DB.
func serveMediaFile(r *fastglue.Request, app *App, uuid string, media *mmodels.Media) error {
	// Fetch media metadata from DB if not provided.
	if media == nil {
		m, err := getMediaByUUID(app, uuid)
		if err != nil {
			return sendErrorEnvelope(r, err)
		}
		media = &m
	}

	forceDownload := string(r.RequestCtx.QueryArgs().Peek("download")) == "1"

	consts := app.consts.Load().(*constants)
	switch consts.UploadProvider {
	case "fs":
		disposition := "attachment"

		if !forceDownload && safeInlineMediaType(media.ContentType) {
			disposition = "inline"
		}

		r.RequestCtx.Response.Header.Set("Content-Type", media.ContentType)
		r.RequestCtx.Response.Header.Set("Content-Disposition", fmt.Sprintf(`%s; filename="%s"`, disposition, media.Filename))
		r.RequestCtx.Response.Header.Set("X-Content-Type-Options", "nosniff")

		fasthttp.ServeFile(r.RequestCtx, filepath.Join(ko.String("upload.fs.upload_path"), uuid))
	case "s3":
		url := app.media.GetURL(uuid, media.ContentType, media.Filename)
		if forceDownload {
			url = app.media.GetURLForDownload(uuid, media.Filename)
		}
		r.RequestCtx.Redirect(url, http.StatusFound)
	}
	return nil
}

func safeInlineMediaType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	switch strings.ToLower(mediaType) {
	case "image/png", "image/jpeg", "image/gif",
		"video/mp4", "video/webm", "video/ogg":
		return true
	default:
		return false
	}
}

// bytesToMegabytes converts bytes to megabytes.
func bytesToMegabytes(bytes int64) float64 {
	return float64(bytes) / 1024 / 1024
}

func withinUnlinkedUploadQuota(count, usedBytes, nextBytes int64) bool {
	if count < 0 || usedBytes < 0 || nextBytes <= 0 {
		return false
	}
	if count >= maxUnlinkedUploadsPerUser || usedBytes > maxUnlinkedUploadBytes {
		return false
	}
	return nextBytes <= maxUnlinkedUploadBytes-usedBytes
}

// getUnassociatedMedia fetches media by IDs, skipping any already associated with a model.
// Unattached uploads are scoped to the uploading user to prevent cross-account attachment reuse.
func getUnassociatedMedia(app *App, ids []int, ownerUserID int) ([]mmodels.Media, error) {
	all, err := app.media.GetMany(ids)
	if err != nil {
		return nil, err
	}
	out := make([]mmodels.Media, 0, len(all))
	for _, m := range all {
		if m.ModelID.Int > 0 {
			app.lo.Warn("attachment already associated with another model, skipping", "media_id", m.ID, "model", m.Model.String, "model_id", m.ModelID.Int)
			continue
		}
		if ownerUserID > 0 && (!m.OwnerUserID.Valid || m.OwnerUserID.Int != ownerUserID) {
			app.lo.Warn("attachment owner mismatch", "media_id", m.ID, "expected_owner_user_id", ownerUserID, "owner_user_id", m.OwnerUserID.Int)
			return nil, envelope.NewError(envelope.PermissionError, app.i18n.T("status.deniedPermission"), nil)
		}
		out = append(out, m)
	}
	return out, nil
}

// getMediaByUUID fetches media metadata from DB, handling thumbnail prefix.
func getMediaByUUID(app *App, uuid string) (mmodels.Media, error) {
	return app.media.Get(0, strings.TrimPrefix(uuid, image.ThumbPrefix))
}
