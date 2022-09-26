package static

import "embed"

var (
	//go:embed assets/*
	Static embed.FS
)
