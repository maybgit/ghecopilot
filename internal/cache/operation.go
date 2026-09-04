package cache

var cache Cacheable

func init() {
	cache = NewMemoryMap()
}
func Set(key string, value interface{}, ttl int) error {
	return cache.Set(key, value, ttl)
}
func Get(key string) (interface{}, error) {
	return cache.Get(key)
}
func Exist(key string) (bool, error) {
	return cache.Exist(key)
}
func Del(key string) error {
	return cache.Del(key)
}
