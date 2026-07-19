local current = redis.call('GET', KEYS[1])
if current then
  return {0, current}
end

redis.call('PSETEX', KEYS[1], ARGV[2], ARGV[1])
return {1, ''}
