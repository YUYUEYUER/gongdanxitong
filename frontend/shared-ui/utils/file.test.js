import { describe, expect, test } from 'vitest'
import { downloadUrl, getThumbFilepath, isSafePreviewImage } from './file'

describe('signed media URL handling', () => {
    test('keeps backend-issued local signatures intact', () => {
        const signed = 'https://example.com/uploads/9a4f0a03-7b36-4e05-aacd-4eb04947a79d?sig=abc&exp=1700000000'
        expect(downloadUrl(signed)).toBe(signed)
        expect(getThumbFilepath(signed)).toBe(signed)
    })

    test('keeps S3 presigned host, path, and query intact', () => {
        const presigned = 'https://bucket.s3.example.com/9a4f0a03-7b36-4e05-aacd-4eb04947a79d?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Signature=deadbeef'
        expect(downloadUrl(presigned)).toBe(presigned)
        expect(getThumbFilepath(presigned)).toBe(presigned)
    })

    test('keeps relative backend URLs intact', () => {
        const relative = '/uploads/9a4f0a03-7b36-4e05-aacd-4eb04947a79d?download=1'
        expect(downloadUrl(relative)).toBe('http://localhost:3000/uploads/9a4f0a03-7b36-4e05-aacd-4eb04947a79d?download=1')
        expect(getThumbFilepath(relative)).toBe('http://localhost:3000/uploads/9a4f0a03-7b36-4e05-aacd-4eb04947a79d?download=1')
    })

    test('returns falsy input unchanged and rejects executable URLs', () => {
        expect(downloadUrl('')).toBe('')
        expect(downloadUrl(null)).toBe(null)
        expect(getThumbFilepath(undefined)).toBe(undefined)
        expect(downloadUrl('javascript:alert(1)')).toBe('')
        expect(downloadUrl('data:text/html,hello')).toBe('')
        expect(getThumbFilepath('javascript:alert(1)')).toBe('')
    })
})

describe('safe browser image preview types', () => {
    test('allows bounded raster formats only', () => {
        expect(isSafePreviewImage({ content_type: 'image/png' })).toBe(true)
        expect(isSafePreviewImage({ type: 'image/jpeg' })).toBe(true)
        expect(isSafePreviewImage({ name: 'photo.gif' })).toBe(false)
        expect(isSafePreviewImage({ content_type: 'image/webp' })).toBe(false)
        expect(isSafePreviewImage({ content_type: 'image/avif' })).toBe(false)
        expect(isSafePreviewImage({ content_type: 'image/svg+xml' })).toBe(false)
    })

    test('requires a signed thumbnail before automatically previewing stored images', () => {
        expect(isSafePreviewImage({
            uuid: 'image-1',
            name: 'disguised.pdf',
            content_type: 'image/png',
            url: 'https://example.com/original',
            thumbnail_url: 'https://example.com/thumbnail'
        })).toBe(true)
        expect(isSafePreviewImage({
            uuid: 'image-2',
            content_type: 'image/png',
            url: 'https://example.com/original'
        })).toBe(false)
        expect(isSafePreviewImage({
            uuid: 'image-3',
            content_type: 'image/png',
            thumbnail_url: 'javascript:alert(1)'
        })).toBe(false)
    })
})
// @vitest-environment jsdom
