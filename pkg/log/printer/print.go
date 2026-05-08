// Package printer contains implementations for formatting and printing log
// search results to various outputs (stdout, files, etc.).
package printer

import (
	"context"
	"os"

	"github.com/estran-studio/logviewer/pkg/log/client"
)

// PrintPrinter prints results to standard output.
type PrintPrinter struct{}

// Display writes `result` to stdout and returns whether the result continues
// streaming (follow mode) along with any immediate error encountered.
func (pp PrintPrinter) Display(ctx context.Context, result client.LogSearchResult, onError func(error)) (bool, error) {

	return WrapIoWritter(ctx, result, os.Stdout, func() {}, onError)
}
