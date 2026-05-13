# Database schema

The live schema is applied by `internal/db/schema.go`.

Main tables:

- `files`: file metadata, policy, soft-delete status, storage object mapping.
- `upload_batches`: upload batch summary.
- `file_events`: per-file event stream.
- `account_sessions`: myfiles-owned session cookie state. Only a hash of `myfiles_session` is stored.
- `site_settings`: site-level JSON settings.
- `storage_settings`: storage-channel JSON settings.
- `audit_logs`: admin and sensitive action audit log.

`files.owner_user_id` stores only account-system `user.id`.
