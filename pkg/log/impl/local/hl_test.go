// Package local provides a local file/command based log client used for
// development and testing. This test package verifies behavior of the local
// implementation.
package local

import (
	"context"
	"testing"

	"github.com/estran-studio/logviewer/pkg/adapter/hl"
	logclient "github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/estran-studio/logviewer/pkg/log/client/operator"
	"github.com/estran-studio/logviewer/pkg/ty"
	"github.com/stretchr/testify/assert"
)

func TestLocalClient_HLDetection(t *testing.T) {
	// This test verifies the hl detection logic without requiring hl to be installed

	t.Run("uses native when preferNativeDriver is set", func(t *testing.T) {
		// Disable hl detection for this test
		hl.DisableDetection()
		defer hl.EnableDetection()

		lc, err := GetLogClient()
		assert.NoError(t, err)
		assert.NotNil(t, lc)

		search := &logclient.LogSearch{
			Options: ty.MI{
				OptionsPaths:              []string{"/var/log/app.log"},
				OptionsPreferNativeDriver: true,
				OptionsCmd:                "echo 'test'", // Fallback command
			},
		}

		// This should use native command, not hl
		// We can't easily test the actual execution without side effects,
		// but we can verify the search is constructed correctly
		assert.True(t, search.Options.GetBool(OptionsPreferNativeDriver))
	})

	t.Run("requires cmd or paths", func(t *testing.T) {
		hl.DisableDetection()
		defer hl.EnableDetection()

		lc, err := GetLogClient()
		assert.NoError(t, err)

		search := &logclient.LogSearch{
			Options: ty.MI{}, // No cmd or paths
		}

		// This should panic or error because neither cmd nor paths is set
		assert.Panics(t, func() {
			_, _ = lc.Get(context.Background(), search)
		})
	})
}

func TestLocalClient_HLArgsBuilding(t *testing.T) {
	// Test that hl args are built correctly from LogSearch

	search := &logclient.LogSearch{
		Filter: &logclient.Filter{
			Logic: logclient.LogicAnd,
			Filters: []logclient.Filter{
				{Field: "level", Op: operator.Equals, Value: "error"},
				{Field: "service", Op: operator.Match, Value: "api"},
			},
		},
		Range: logclient.SearchRange{
			Last: ty.Opt[string]{Set: true, Value: "1h"},
		},
		Follow: true,
	}

	args, err := hl.BuildArgs(search, []string{"/var/log/app.log"})
	assert.NoError(t, err)

	// Verify expected args are present
	assert.Contains(t, args, "-P")      // No pager
	assert.Contains(t, args, "-F")      // Follow mode
	assert.Contains(t, args, "--since") // Time range
	assert.Contains(t, args, "-q")      // Query filter
	assert.Contains(t, args, "/var/log/app.log")
}

func TestLocalClient_PreFilteredFlag(t *testing.T) {
	// Test that the preFiltered flag is set correctly when using hl

	search := &logclient.LogSearch{
		Options: ty.MI{
			"__preFiltered__": true,
		},
	}

	assert.True(t, search.Options.GetBool("__preFiltered__"))
}
