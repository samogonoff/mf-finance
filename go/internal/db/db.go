package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Pool = pgxpool.Pool

func Open(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	return pgxpool.NewWithConfig(ctx, cfg)
}
