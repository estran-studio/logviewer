package printer

import (
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"text/template"

	"github.com/estran-studio/logviewer/pkg/log/client"
)

// LogPrinter represents an entity capable of rendering log search results to
// a target output.
type LogPrinter interface {
	// Display renders `result` to the configured output. The implementation may
	// stream results and return (continuous=true) when following logs.
	Display(ctx context.Context, result client.LogSearchResult) error
}

// WrapIoWritter performs the common work of writing entries from a
// LogSearchResult to an `io.Writer`. It returns a boolean indicating whether
// the result will continue streaming (follow) and an error for initial
// processing failures.
func WrapIoWritter(ctx context.Context, result client.LogSearchResult, writer io.Writer, update func(), onError func(error)) (bool, error) {

	printerOptions := result.GetSearch().PrinterOptions

	// Initialize color state based on configuration and TTY detection
	var colorEnabled *bool
	if printerOptions.Color.Set {
		colorEnabled = &printerOptions.Color.Value
	}
	InitColorState(colorEnabled, writer)

	templateConfig := printerOptions.Template

	if templateConfig.Value == "" {
		templateConfig.S("[{{FormatTimestamp .Timestamp \"15:04:05\"}}] [{{.ContextID}}] {{.Level}} {{.Message}}")
	}

	tmpl, err := template.New("print_printer").Funcs(GetTemplateFunctionsMap()).Parse(templateConfig.Value + "\n")
	if err != nil {
		return false, err
	}

	// Prepare messageRegex if present
	var messageRegex *regexp.Regexp
	if printerOptions.MessageRegex.Set && printerOptions.MessageRegex.Value != "" {
		var errRegex error
		messageRegex, errRegex = regexp.Compile(printerOptions.MessageRegex.Value)
		if errRegex != nil {
			return false, errRegex
		}
	}

	entries, newEntriesChannel, err := result.GetEntries(ctx)
	if err != nil {
		return false, err
	}

	search := result.GetSearch()
	if err := processEntries(writer, tmpl, messageRegex, entries, search); err != nil {
		return false, err
	}

	update()

	if newEntriesChannel != nil {
		go func() {
			update()
			for entries := range newEntriesChannel {
				if len(entries) > 0 {
					if err := processEntries(writer, tmpl, messageRegex, entries, search); err != nil {
						fmt.Fprintf(os.Stderr, "error printing log entries: %v\n", err)
					}
					update()
				}
			}
		}()
	}

	// new goroutine to listen for errors
	if errChan := result.Err(); errChan != nil {
		go func() {
			for err := range errChan {
				onError(err)
			}
		}()
	}

	return newEntriesChannel != nil, nil
}

func processEntries(writer io.Writer, tmpl *template.Template, messageRegex *regexp.Regexp, entries []client.LogEntry, search *client.LogSearch) error {
	for i, entry := range entries {
		// Extract JSON fields if enabled (idempotent - safe if already extracted in multi-context merge)
		client.ExtractJSONFromEntry(&entries[i], search)

		if messageRegex != nil {
			matches := messageRegex.FindStringSubmatch(entry.Message)
			if len(matches) > 1 {
				entries[i].Message = matches[1]
			}
		}
		if err := tmpl.Execute(writer, entries[i]); err != nil {
			return err
		}
	}
	return nil
}
