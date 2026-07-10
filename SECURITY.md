# Security Reports

Report vulnerabilities privately via GitHub Security Advisories: https://github.com/abhinavxd/libredesk/security/advisories

## Threat model

Libredesk is **self-hosted and single-organization**. A deployment has no workspace or tenant identifier, so inboxes, teams, and roles must not be used to host mutually distrusting companies in one instance. Use separate deployments, databases, Redis instances, encryption keys, and upload stores when organizational isolation is required.

The customer portal, public-ticket forms, livechat widget, inbound email, uploaded files, and externally supplied identity assertions are untrusted inputs. Agent permissions are enforced as authorization boundaries inside one organization; administrators remain fully trusted and can grant broad `*:manage` and `*:read_all` capabilities.

Production operators must configure exact reverse-proxy CIDRs in `app.server.trusted_proxies`, configure every livechat host in the inbox `trusted_domains` list, use HTTPS, and keep the database, Redis, and object storage private. Uploaded objects should be served from a separate origin and with forced-download headers.

LibreDesk currently supports exactly one application process per deployment. OIDC provider runtime invalidation, WebSocket ownership, and other security state are not coordinated across replicas. Horizontal application scaling is unsupported until these controls use distributed coordination.

Set `upload.max_total_files` and `upload.max_total_size_bytes` below the real storage capacity, retain meaningful `upload.min_free_size_bytes` headroom for filesystem storage, and enforce an independent volume or S3 bucket quota. The database quota is a security boundary, not a substitute for provider capacity alerts and storage-to-database reconciliation.

## v2.5.2 security upgrade contract

The safe order is mandatory:

1. Stop **all** old application binaries. Never run v2.5.1 and v2.5.2 against the same database concurrently.
2. Back up PostgreSQL and the complete filesystem/S3 upload store, and verify restoration.
3. Run `--upgrade --yes` from exactly one v2.5.2 process.
4. Start only the new pinned image and check `/ready` before restoring traffic.

This migration revokes every existing agent session and API key because pre-migration OIDC logins cannot be attributed to a stable issuer/subject identity. It clears passwords for contacts affected by legacy portal registration; customers recover only through the verified password-reset email flow. Old Widget sessions are intentionally rejected. Administrators must review identity providers and API-key owners before issuing replacements.

Do not roll back only the binary after this migration. A rollback requires restoring the matching database and upload-store backup. The migration runner uses a PostgreSQL advisory lock, and one-time credential invalidation is protected by a durable marker so an interrupted version-recording step can be retried safely.

## Existing S3 objects

New S3 uploads are private and force active content to download, but upgrading code does not rewrite existing object ACLs, CDN caches, or metadata. Before production traffic resumes:

- enable bucket-level public-access blocking and remove public-read ACL/policies;
- disable unauthenticated CDN access and invalidate cached public object URLs;
- rewrite historical HTML, SVG, XML, and JavaScript-like objects to `application/octet-stream` with `Content-Disposition: attachment`;
- keep uploads on a dedicated, cookie-free origin and verify that old stable public URLs no longer work.

Back up first and use the storage provider's inventory/batch-operation tooling for this migration. `upload.s3.public_url` is ignored because permanent public attachment URLs are not a supported security mode.

## Out of scope

- An admin exercising a documented admin capability (configuring webhooks, OIDC providers, automations, templates, inboxes, etc.). It is up to the operator to grant these capabilities only to trusted users. Defects in the admin code paths (sqli, RCE, auth bypass etc.) remain in scope.
- A trusted administrator intentionally connecting Libredesk to an external service. Network-boundary bypasses, DNS rebinding, credential disclosure, and other defects in those integrations remain in scope.

Anything else is in scope.
