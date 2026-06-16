# Auth.md

Agent authentication and registration metadata for Files.js.gripe.

Files.js.gripe is a protected file upload and sharing service. Public `/files/...` links are direct shared file URLs and may be fetched while they remain public. File metadata, dashboards, upload results, protected APIs, setup pages, and administrative pages are not public crawl targets.

Agents may read this document, related `.well-known` metadata, and public `/files/...` links to discover authentication requirements, access boundaries, and shared file content.

## Standalone Registration Flow

There is no public self-service agent registration flow for file access. Human users sign in through Account.js.gripe from `https://files.js.gripe/login`.

Service integrations must be approved through Account.js.gripe and granted the `myfiles` client scopes. Agents must not attempt automated sign-up, unattended uploads, private API scraping, or account actions.

## Authentication

Protected APIs are served from `https://files.js.gripe/api/`.

Identity is delegated to Account.js.gripe:
- Authorization server: `https://account.js.gripe/`
- File service protected resource: `https://files.js.gripe/`
- OAuth protected resource metadata: `https://files.js.gripe/.well-known/oauth-protected-resource`
- API catalog: `https://files.js.gripe/.well-known/api-catalog`

Supported credential pattern:
- User browser session issued after Account.js.gripe sign-in.

## Claims

File records can include owner account id, filename, MIME type, size, storage provider, public sharing policy, confirmation requirements, region policy, hotlink policy, and audit events. These records are protected unless a file owner explicitly publishes an individual file link.

## Revocation

Users can sign out of Files.js.gripe and remove files from their dashboard. Administrators can disable public access, soft-delete files, update policies, and review audit logs.

Support contact: `helper@js.gripe`.
