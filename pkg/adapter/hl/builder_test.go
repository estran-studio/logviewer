package hl

import (
	"testing"

	"github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/estran-studio/logviewer/pkg/log/client/operator"
	"github.com/estran-studio/logviewer/pkg/ty"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildArgs_BasicFilter(t *testing.T) {
	search := &client.LogSearch{
		Filter: &client.Filter{
			Field: "level",
			Op:    operator.Equals,
			Value: "error",
		},
	}

	args, err := BuildArgs(search, []string{"/var/log/app.log"})
	require.NoError(t, err)

	assert.Contains(t, args, "-P")
	assert.Contains(t, args, "--raw")
	assert.Contains(t, args, "-q")
	assert.Contains(t, args, "level = error")
	assert.Contains(t, args, "/var/log/app.log")
}

func TestBuildArgs_NegatedFilter(t *testing.T) {
	search := &client.LogSearch{
		Filter: &client.Filter{
			Field:  "level",
			Op:     operator.Equals,
			Value:  "debug",
			Negate: true,
		},
	}

	args, err := BuildArgs(search, []string{"/var/log/app.log"})
	require.NoError(t, err)

	// Find the -q argument and check its value
	for i, arg := range args {
		if arg == "-q" && i+1 < len(args) {
			assert.Equal(t, "level != debug", args[i+1])
			return
		}
	}
	t.Fatal("-q argument not found")
}

func TestBuildArgs_RegexFilter(t *testing.T) {
	search := &client.LogSearch{
		Filter: &client.Filter{
			Field: "message",
			Op:    operator.Regex,
			Value: "error.*timeout",
		},
	}

	args, err := BuildArgs(search, []string{"/var/log/app.log"})
	require.NoError(t, err)

	for i, arg := range args {
		if arg == "-q" && i+1 < len(args) {
			assert.Equal(t, "message ~~= error.*timeout", args[i+1])
			return
		}
	}
	t.Fatal("-q argument not found")
}

func TestBuildArgs_MatchFilter(t *testing.T) {
	search := &client.LogSearch{
		Filter: &client.Filter{
			Field: "message",
			Op:    operator.Match,
			Value: "connection",
		},
	}

	args, err := BuildArgs(search, []string{"/var/log/app.log"})
	require.NoError(t, err)

	for i, arg := range args {
		if arg == "-q" && i+1 < len(args) {
			assert.Equal(t, "message ~= connection", args[i+1])
			return
		}
	}
	t.Fatal("-q argument not found")
}

func TestBuildArgs_ComparisonOperators(t *testing.T) {
	tests := []struct {
		name     string
		op       string
		expected string
	}{
		{"greater than", operator.Gt, "status > 400"},
		{"greater or equal", operator.Gte, "status >= 400"},
		{"less than", operator.Lt, "status < 400"},
		{"less or equal", operator.Lte, "status <= 400"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			search := &client.LogSearch{
				Filter: &client.Filter{
					Field: "status",
					Op:    tt.op,
					Value: "400",
				},
			}

			args, err := BuildArgs(search, []string{"/var/log/app.log"})
			require.NoError(t, err)

			for i, arg := range args {
				if arg == "-q" && i+1 < len(args) {
					assert.Equal(t, tt.expected, args[i+1])
					return
				}
			}
			t.Fatal("-q argument not found")
		})
	}
}

func TestBuildArgs_ExistsOperator(t *testing.T) {
	search := &client.LogSearch{
		Filter: &client.Filter{
			Field: "trace_id",
			Op:    operator.Exists,
		},
	}

	args, err := BuildArgs(search, []string{"/var/log/app.log"})
	require.NoError(t, err)

	for i, arg := range args {
		if arg == "-q" && i+1 < len(args) {
			assert.Equal(t, "exists(.trace_id)", args[i+1])
			return
		}
	}
	t.Fatal("-q argument not found")
}

func TestBuildArgs_WildcardFilter(t *testing.T) {
	search := &client.LogSearch{
		Filter: &client.Filter{
			Field: "host",
			Op:    operator.Wildcard,
			Value: "prod-*",
		},
	}

	args, err := BuildArgs(search, []string{"/var/log/app.log"})
	require.NoError(t, err)

	for i, arg := range args {
		if arg == "-q" && i+1 < len(args) {
			assert.Equal(t, "host like prod-*", args[i+1])
			return
		}
	}
	t.Fatal("-q argument not found")
}

func TestBuildArgs_ANDLogic(t *testing.T) {
	search := &client.LogSearch{
		Filter: &client.Filter{
			Logic: client.LogicAnd,
			Filters: []client.Filter{
				{Field: "level", Op: operator.Equals, Value: "error"},
				{Field: "service", Op: operator.Equals, Value: "api"},
			},
		},
	}

	args, err := BuildArgs(search, []string{"/var/log/app.log"})
	require.NoError(t, err)

	for i, arg := range args {
		if arg == "-q" && i+1 < len(args) {
			assert.Equal(t, "(level = error and service = api)", args[i+1])
			return
		}
	}
	t.Fatal("-q argument not found")
}

func TestBuildArgs_ORLogic(t *testing.T) {
	search := &client.LogSearch{
		Filter: &client.Filter{
			Logic: client.LogicOr,
			Filters: []client.Filter{
				{Field: "level", Op: operator.Equals, Value: "error"},
				{Field: "level", Op: operator.Equals, Value: "warn"},
			},
		},
	}

	args, err := BuildArgs(search, []string{"/var/log/app.log"})
	require.NoError(t, err)

	for i, arg := range args {
		if arg == "-q" && i+1 < len(args) {
			assert.Equal(t, "(level = error or level = warn)", args[i+1])
			return
		}
	}
	t.Fatal("-q argument not found")
}

func TestBuildArgs_NOTLogic(t *testing.T) {
	search := &client.LogSearch{
		Filter: &client.Filter{
			Logic: client.LogicNot,
			Filters: []client.Filter{
				{Field: "level", Op: operator.Equals, Value: "debug"},
			},
		},
	}

	args, err := BuildArgs(search, []string{"/var/log/app.log"})
	require.NoError(t, err)

	for i, arg := range args {
		if arg == "-q" && i+1 < len(args) {
			assert.Equal(t, "not (level = debug)", args[i+1])
			return
		}
	}
	t.Fatal("-q argument not found")
}

func TestBuildArgs_NestedLogic(t *testing.T) {
	search := &client.LogSearch{
		Filter: &client.Filter{
			Logic: client.LogicAnd,
			Filters: []client.Filter{
				{Field: "service", Op: operator.Equals, Value: "api"},
				{
					Logic: client.LogicOr,
					Filters: []client.Filter{
						{Field: "level", Op: operator.Equals, Value: "error"},
						{Field: "level", Op: operator.Equals, Value: "warn"},
					},
				},
			},
		},
	}

	args, err := BuildArgs(search, []string{"/var/log/app.log"})
	require.NoError(t, err)

	for i, arg := range args {
		if arg == "-q" && i+1 < len(args) {
			assert.Equal(t, "(service = api and (level = error or level = warn))", args[i+1])
			return
		}
	}
	t.Fatal("-q argument not found")
}

func TestBuildArgs_TimeRange(t *testing.T) {
	t.Run("last duration", func(t *testing.T) {
		search := &client.LogSearch{
			Range: client.SearchRange{
				Last: ty.Opt[string]{Set: true, Value: "15m"},
			},
		}

		args, err := BuildArgs(search, []string{"/var/log/app.log"})
		require.NoError(t, err)

		assert.Contains(t, args, "--since")
		// Find --since and check its value
		for i, arg := range args {
			if arg == "--since" && i+1 < len(args) {
				assert.Equal(t, "-15m", args[i+1])
				return
			}
		}
		t.Fatal("--since argument not found")
	})

	t.Run("absolute range", func(t *testing.T) {
		search := &client.LogSearch{
			Range: client.SearchRange{
				Gte: ty.Opt[string]{Set: true, Value: "2024-01-01T00:00:00Z"},
				Lte: ty.Opt[string]{Set: true, Value: "2024-01-02T00:00:00Z"},
			},
		}

		args, err := BuildArgs(search, []string{"/var/log/app.log"})
		require.NoError(t, err)

		assert.Contains(t, args, "--since")
		assert.Contains(t, args, "--until")
	})
}

func TestBuildArgs_FollowMode(t *testing.T) {
	search := &client.LogSearch{
		Follow: true,
	}

	args, err := BuildArgs(search, []string{"/var/log/app.log"})
	require.NoError(t, err)

	assert.Contains(t, args, "-F")
}

func TestBuildArgs_MultipleFiles(t *testing.T) {
	search := &client.LogSearch{}

	paths := []string{"/var/log/app.log", "/var/log/error.log", "/var/log/access.log"}
	args, err := BuildArgs(search, paths)
	require.NoError(t, err)

	// All paths should be at the end
	for _, path := range paths {
		assert.Contains(t, args, path)
	}
}

func TestBuildArgs_MessageField(t *testing.T) {
	// "_" is the special sentinel for message search
	search := &client.LogSearch{
		Filter: &client.Filter{
			Field: "_",
			Op:    operator.Match,
			Value: "timeout",
		},
	}

	args, err := BuildArgs(search, []string{"/var/log/app.log"})
	require.NoError(t, err)

	for i, arg := range args {
		if arg == "-q" && i+1 < len(args) {
			assert.Equal(t, "message ~= timeout", args[i+1])
			return
		}
	}
	t.Fatal("-q argument not found")
}

func TestBuildArgs_ValueEscaping(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		expected string
	}{
		{"simple value", "error", "error"},
		{"value with space", "connection timeout", `"connection timeout"`},
		{"value with quotes", `say "hello"`, `"say \"hello\""`},
		{"value with special chars", "a=b&c", `"a=b&c"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := escapeValue(tt.value)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBuildArgs_LegacyFields(t *testing.T) {
	// Test that legacy Fields map is also converted
	search := &client.LogSearch{
		Fields: ty.MS{
			"level":   "error",
			"service": "api",
		},
	}

	args, err := BuildArgs(search, []string{"/var/log/app.log"})
	require.NoError(t, err)

	// Should have a -q with AND of both fields
	for i, arg := range args {
		if arg == "-q" && i+1 < len(args) {
			expr := args[i+1]
			assert.Contains(t, expr, "level = error")
			assert.Contains(t, expr, "service = api")
			assert.Contains(t, expr, " and ")
			return
		}
	}
	t.Fatal("-q argument not found")
}

func TestBuildSimpleArgs(t *testing.T) {
	args := BuildSimpleArgs(
		[]string{"/var/log/app.log"},
		true,
		"1h",
		map[string]string{"level": "error"},
	)

	assert.Contains(t, args, "-P")
	assert.Contains(t, args, "--raw")
	assert.Contains(t, args, "-F")
	assert.Contains(t, args, "--since")
	assert.Contains(t, args, "-1h")
	assert.Contains(t, args, "-f")
	assert.Contains(t, args, "level=error")
	assert.Contains(t, args, "/var/log/app.log")
}
