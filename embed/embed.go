package embed

import (
	"context"
	_ "embed"

	"go.senan.xyz/taglib-wasm"
)

//go:embed tag_c.wasm
var Binary []byte

func init() {
	taglib.LoadBinary(context.Background(), Binary)
}
