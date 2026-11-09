package config

// Password returns the Redis ACL password from its file, or "" when none is
// configured.
func (r Redis) Password() (Secret, error) {
	return resolveSecret("redis.password", "", r.PasswordFile)
}
