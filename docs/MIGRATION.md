# Migration from mypicture to myfiles

This migration intentionally does not import old tables or old files.

1. Prepare account-system API client:
   - name: `myfiles`
   - redirect URI: `https://files.js.gripe/auth/account/callback`
   - scopes: `accounts:read identities:resolve`

2. Deploy new project:
   - `/opt/myfiles`
   - `/etc/myfiles/config.json`
   - `/var/lib/myfiles`
   - systemd service: `myfiles.service`

3. Switch domain:
   - make `files.js.gripe` point to the VPS/OpenResty host.
   - include `deploy/openresty/files.js.gripe.snippet.conf` in the existing TLS server block.
   - keep wildcard SSL certificate paths unchanged.

4. Start new service:
   - `sudo systemctl enable --now myfiles.service`
   - `curl -I https://files.js.gripe/healthz`

5. Remove old trial data and packages:
   - review `scripts/purge-old-mypicture.sh`
   - run `sudo PURGE_OLD=1 bash scripts/purge-old-mypicture.sh`

6. Validate:
   - public upload page contains no admin entry and no backend storage description.
   - login popup returns to `/dashboard`.
   - member only sees My Files.
   - system_admin sees admin panels.
   - `/my/files` should be redirected or removed at OpenResty level; canonical path is `/dashboard/files`.
