local cost = tonumber(ARGV[1])
local limit = tonumber(ARGV[2])
local window_ms = tonumber(ARGV[3])
local used = redis.call('INCRBY', KEYS[1], cost)
local ttl

if used == cost then
    redis.call('PEXPIRE', KEYS[1], window_ms)
    ttl = window_ms
else
    ttl = redis.call('PTTL', KEYS[1])
    if ttl < 0 then
        redis.call('PEXPIRE', KEYS[1], window_ms)
        ttl = window_ms
    end
end

local allowed = 0
if used <= limit then
    allowed = 1
end

local remaining = limit - used
if remaining < 0 then
    remaining = 0
end

return {allowed, remaining, ttl}
