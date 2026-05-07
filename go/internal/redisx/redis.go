package redisx

import "github.com/redis/go-redis/v9"

type Client = redis.Client

func Open(addr string) *redis.Client {
	return redis.NewClient(&redis.Options{Addr: addr})
}
