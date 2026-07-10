package main

import (
	"testing"

	mmodels "github.com/abhinavxd/libredesk/internal/media/models"
	"github.com/stretchr/testify/require"
	"github.com/volatiletech/null/v9"
)

const testAvatarMediaUUID = "8e0da074-86e0-407a-b52e-b6c56e82975d"

func TestUserAvatarMediaUUIDRequiresCanonicalLocalUpload(t *testing.T) {
	t.Parallel()

	got, ok := userAvatarMediaUUID("/uploads/" + testAvatarMediaUUID)
	require.True(t, ok)
	require.Equal(t, testAvatarMediaUUID, got)

	for _, raw := range []string{
		"https://example.com/uploads/" + testAvatarMediaUUID,
		"/uploads/thumb_" + testAvatarMediaUUID,
		"/uploads/" + testAvatarMediaUUID + "?download=1",
		"/uploads/../" + testAvatarMediaUUID,
		"/uploads/8E0DA074-86E0-407A-B52E-B6C56E82975D",
		"/uploads/not-a-uuid",
	} {
		_, ok := userAvatarMediaUUID(raw)
		require.Falsef(t, ok, "must reject %q", raw)
	}
}

func TestRequestedContactAvatarCannotPointAtAnotherObject(t *testing.T) {
	t.Parallel()

	current := null.StringFrom("/uploads/" + testAvatarMediaUUID)
	require.True(t, requestedContactAvatarAllowed(current, current.String))
	require.True(t, requestedContactAvatarAllowed(current, ""))
	require.False(t, requestedContactAvatarAllowed(current, "/uploads/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"))
	require.False(t, requestedContactAvatarAllowed(null.String{}, current.String))
}

func TestOwnedUserAvatarMediaRequiresModelAndBothOwnerBindings(t *testing.T) {
	t.Parallel()

	media := mmodels.Media{
		UUID:        testAvatarMediaUUID,
		Model:       null.StringFrom(mmodels.ModelUser),
		ModelID:     null.IntFrom(42),
		OwnerUserID: null.IntFrom(42),
	}
	require.True(t, isOwnedUserAvatarMedia(media, 42, testAvatarMediaUUID))

	wrongModel := media
	wrongModel.Model = null.StringFrom(mmodels.ModelMessages)
	require.False(t, isOwnedUserAvatarMedia(wrongModel, 42, testAvatarMediaUUID))

	wrongModelID := media
	wrongModelID.ModelID = null.IntFrom(7)
	require.False(t, isOwnedUserAvatarMedia(wrongModelID, 42, testAvatarMediaUUID))

	wrongOwner := media
	wrongOwner.OwnerUserID = null.IntFrom(7)
	require.False(t, isOwnedUserAvatarMedia(wrongOwner, 42, testAvatarMediaUUID))

	missingOwner := media
	missingOwner.OwnerUserID = null.Int{}
	require.False(t, isOwnedUserAvatarMedia(missingOwner, 42, testAvatarMediaUUID))
}
