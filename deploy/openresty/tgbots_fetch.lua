local cjson = require "cjson.safe"

local function json_error(status, msg)
    ngx.status = status
    ngx.header["Content-Type"] = "application/json; charset=utf-8"
    ngx.header["Cache-Control"] = "no-store"
    ngx.say(cjson.encode({ ok = false, error_code = status, description = msg }))
    return ngx.exit(status)
end

local args = ngx.req.get_uri_args()
local bot_token = tostring(args.bot_token or "")
local file_id = tostring(args.file_id or "")
if bot_token == "" or file_id == "" then
    return json_error(400, "missing bot_token or file_id")
end

local getfile_uri = "/__tgbots_proxy/bot" .. bot_token .. "/getFile"
local getfile_res = ngx.location.capture(getfile_uri, { method = ngx.HTTP_GET, args = { file_id = file_id } })
if not getfile_res then
    return json_error(502, "getFile upstream unavailable")
end
if getfile_res.status >= 400 then
    ngx.status = getfile_res.status
    ngx.header["Content-Type"] = getfile_res.header["Content-Type"] or "application/json; charset=utf-8"
    ngx.header["Cache-Control"] = "no-store"
    if getfile_res.body and getfile_res.body ~= "" then
        ngx.print(getfile_res.body)
    end
    return ngx.exit(getfile_res.status)
end

local payload, err = cjson.decode(getfile_res.body)
if not payload or payload.ok ~= true or type(payload.result) ~= "table" then
    return json_error(502, "invalid getFile response")
end

local file_path = tostring(payload.result.file_path or "")
if file_path == "" then
    return json_error(502, "empty file_path")
end

local function ext_ct(path)
    local lower = string.lower(path)
    if lower:match("%.jpe?g$") then return "image/jpeg" end
    if lower:match("%.png$") then return "image/png" end
    if lower:match("%.gif$") then return "image/gif" end
    if lower:match("%.webp$") then return "image/webp" end
    if lower:match("%.svg$") then return "image/svg+xml" end
    if lower:match("%.mp4$") then return "video/mp4" end
    if lower:match("%.mov$") then return "video/quicktime" end
    if lower:match("%.webm$") then return "video/webm" end
    if lower:match("%.m4v$") then return "video/x-m4v" end
    return "application/octet-stream"
end

local function parse_range(size)
    local range = ngx.req.get_headers()["Range"]
    if type(range) ~= "string" then
        return 0, size - 1, 200
    end
    local first, last = range:match("^bytes=(%d*)%-(%d*)$")
    if first == nil then
        return 0, size - 1, 200
    end
    local start_pos = 0
    local end_pos = size - 1
    if first == "" and last ~= "" then
        local suffix = tonumber(last)
        if suffix and suffix > 0 then
            start_pos = math.max(size - suffix, 0)
        end
    elseif first ~= "" then
        start_pos = tonumber(first) or 0
        if last ~= "" then
            end_pos = tonumber(last) or end_pos
        end
    end
    if start_pos < 0 or start_pos >= size or end_pos < start_pos then
        return nil, nil, 416
    end
    return start_pos, math.min(end_pos, size - 1), 206
end

local function stream_local_file(abs_path)
    local allowed_prefixes = {
        "/var/lib/telegram-bot-api/",
        "/srv/telegram-bot-api/",
        "/opt/telegram-bot-api/",
    }
    local ok = false
    for _, prefix in ipairs(allowed_prefixes) do
        if abs_path:sub(1, #prefix) == prefix then
            ok = true
            break
        end
    end
    if not ok then
        return json_error(403, "file_path outside allowed roots")
    end

    local f = io.open(abs_path, "rb")
    if not f then
        return json_error(404, "local file not found")
    end
    local size = f:seek("end")
    local start_pos, end_pos, status = parse_range(size)
    if status == 416 then
        f:close()
        ngx.status = 416
        ngx.header["Content-Range"] = "bytes */" .. size
        ngx.header["Accept-Ranges"] = "bytes"
        return ngx.exit(416)
    end
    local length = end_pos - start_pos + 1
    f:seek("set", start_pos)

    ngx.status = status
    ngx.header["Content-Type"] = ext_ct(abs_path)
    ngx.header["Content-Length"] = length
    ngx.header["Accept-Ranges"] = "bytes"
    if status == 206 then
        ngx.header["Content-Range"] = "bytes " .. start_pos .. "-" .. end_pos .. "/" .. size
    end
    ngx.header["Cache-Control"] = "private, max-age=0, no-store, no-cache, must-revalidate"
    ngx.header["X-Accel-Buffering"] = "no"

    if ngx.req.get_method() == "HEAD" then
        f:close()
        return ngx.exit(status)
    end

    local remaining = length
    while remaining > 0 do
        local chunk = f:read(math.min(65536, remaining))
        if not chunk then break end
        remaining = remaining - #chunk
        ngx.print(chunk)
        local ok_flush, flush_err = ngx.flush(true)
        if not ok_flush then
            f:close()
            return ngx.exit(499)
        end
    end
    f:close()
    return ngx.exit(status)
end

if file_path:sub(1, 1) == "/" then
    return stream_local_file(file_path)
end

local normalized = file_path:gsub("^%./", "")
local file_uri = "/__tgbots_proxy/file/bot" .. bot_token .. "/" .. normalized
ngx.header["Cache-Control"] = "private, max-age=0, no-store, no-cache, must-revalidate"
ngx.header["X-Accel-Buffering"] = "no"
return ngx.exec(file_uri)
