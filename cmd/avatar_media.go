package main

import (
	"fmt"
	"strings"

	mmodels "github.com/abhinavxd/libredesk/internal/media/models"
	"github.com/google/uuid"
	"github.com/volatiletech/null/v9"
)

const userAvatarUploadPrefix = "/uploads/"

// userAvatarMediaUUID only accepts canonical, server-issued local upload URLs.
// External and malformed historical avatar URLs may be cleared, but must never
// be interpreted as storage object names.
func userAvatarMediaUUID(raw string) (string, bool) {
	if !strings.HasPrefix(raw, userAvatarUploadPrefix) {
		return "", false
	}
	name := strings.TrimPrefix(raw, userAvatarUploadPrefix)
	if name == "" || strings.ContainsAny(name, "/\\?#") {
		return "", false
	}
	parsed, err := uuid.Parse(name)
	if err != nil || parsed.String() != name {
		return "", false
	}
	return name, true
}

func requestedContactAvatarAllowed(current null.String, requested string) bool {
	return requested == "" || (current.Valid && requested == current.String)
}

func isOwnedUserAvatarMedia(media mmodels.Media, userID int, mediaUUID string) bool {
	return userID > 0 && media.UUID == mediaUUID &&
		media.Model.Valid && media.Model.String == mmodels.ModelUser &&
		media.ModelID.Valid && media.ModelID.Int == userID &&
		media.OwnerUserID.Valid && media.OwnerUserID.Int == userID
}

// deleteOwnedUserAvatarMedia deletes an avatar only after proving that the
// media row is a user-owned object for the target user. Invalid, external, and
// unowned URLs are intentionally left in storage and may still be cleared from
// the user record by the caller.
func deleteOwnedUserAvatarMedia(app *App, userID int, avatarURL string) error {
	mediaUUID, ok := userAvatarMediaUUID(avatarURL)
	if !ok {
		return nil
	}
	if app == nil || app.media == nil || userID <= 0 {
		return fmt.Errorf("invalid user avatar deletion state")
	}

	media, err := app.media.Get(0, mediaUUID)
	if err != nil {
		app.lo.Warn("skipping avatar object deletion without media ownership proof", "user_id", userID, "media_uuid", mediaUUID, "error", err)
		return nil
	}
	if !isOwnedUserAvatarMedia(media, userID, mediaUUID) {
		app.lo.Warn("skipping unowned avatar object deletion", "user_id", userID, "media_uuid", mediaUUID)
		return nil
	}
	return app.media.Delete(mediaUUID)
}
