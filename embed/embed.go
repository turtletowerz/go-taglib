package embed

import (
	"context"
	_ "embed"

	"go.senan.xyz/taglib-wasm"
)

//go:generate cmake -DCMAKE_BUILD_TYPE=Release -DCMAKE_EXPORT_COMPILE_COMMANDS=ON -DWITH_ZLIB=OFF -DBUILD_SHARED_LIBS=OFF -DVISIBILITY_HIDDEN=ON -DBUILD_TESTING=OFF -DWASI_SDK_PREFIX=/opt/wasi-sdk -DCMAKE_TOOLCHAIN_FILE=/opt/wasi-sdk/share/cmake/wasi-sdk.cmake -B build .
//go:generate cmake --build build --target taglib
//go:generate mv build/taglib.wasm .
//go:generate wasm-opt --strip -c -O3 taglib.wasm -o taglib.wasm

//go:embed taglib.wasm
var binary []byte

func init() {
	taglib.LoadBinary(context.Background(), binary)
}
