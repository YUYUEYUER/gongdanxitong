import { sanitizeHttpUrl } from './url.js'

const SAFE_PREVIEW_IMAGE_TYPES = new Set(['image/png', 'image/jpeg', 'image/gif'])

export function isSafePreviewImage (item) {
    const contentType = String(item?.content_type || item?.type || '').split(';', 1)[0].trim().toLowerCase()
    if (!SAFE_PREVIEW_IMAGE_TYPES.has(contentType)) return false

    const isStoredAttachment = ['uuid', 'url', 'download_url', 'thumbnail_url']
        .some((field) => Object.prototype.hasOwnProperty.call(item || {}, field))
    return !isStoredAttachment || Boolean(sanitizeHttpUrl(item?.thumbnail_url))
}

export function formatBytes (bytes) {
    if (bytes < 1024 * 1024) {
        return (bytes / 1024).toFixed(2) + ' KB'
    } else {
        return (bytes / (1024 * 1024)).toFixed(2) + ' MB'
    }
}

export function getThumbFilepath (url) {
    if (!url) return url
    return sanitizeHttpUrl(url)
}

export function downloadUrl (url) {
    if (!url) return url
    return sanitizeHttpUrl(url)
}
