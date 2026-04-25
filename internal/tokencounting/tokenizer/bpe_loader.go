package tokenizer

import (
	"sync"

	"github.com/pkoukk/tiktoken-go"
	tiktoken_loader "github.com/pkoukk/tiktoken-go-loader"
)

var bpeLoaderOnce sync.Once

func configureBpeLoader() {
	bpeLoaderOnce.Do(func() {
		tiktoken.SetBpeLoader(tiktoken_loader.NewOfflineLoader())
	})
}
