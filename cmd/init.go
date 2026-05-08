package cmd

import (
	"fmt"
	"strings"

	httpPkg "github.com/estran-studio/logviewer/pkg/http"
	"github.com/estran-studio/logviewer/pkg/log"
	"github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/estran-studio/logviewer/pkg/log/client/config"
	"github.com/estran-studio/logviewer/pkg/log/impl/ssh"
	"github.com/spf13/cobra"
)

var (
	// kibana options
	endpointOpensearch string
	endpointKibana     string
	index              string

	// k8s options
	k8sNamespace     string
	k8sPod           string
	k8sLabelSelector string
	k8sContainer     string
	k8sPrevious      bool
	k8sTimestamp     bool

	// splunk
	endpointSplunk string

	// docker
	dockerHost      string
	dockerContainer string
	dockerService   string
	dockerProject   string

	// ssh options
	sshOptions ssh.LogClientOptions
	cmd        string

	// cloudwatch
	cloudwatchLogGroup        string
	cloudwatchRegion          string
	cloudwatchProfile         string
	cloudwatchEndpoint        string
	cloudwatchUseInsights     bool
	cloudwatchPollInterval    string
	cloudwatchMaxPollInterval string
	cloudwatchPollBackoff     string

	// extra client fields
	headerField string
	bodyField   string

	// range
	from string
	to   string
	last string

	// native query
	nativeQuery string

	// hl-compatible query expression
	queryExpr string

	// fields
	fields     []string
	fieldsOps  []string
	inherits   []string
	vars       []string
	groupRegex string
	kvRegex    string

	size int

	duration string
	refresh  bool

	template string

	contextIDs []string

	logger log.MyLoggerOptions

	myLog     bool
	debugHTTP bool

	pageToken   string
	jsonOutput  bool
	colorOutput string
)

func onCommandStart(_ *cobra.Command, _ []string) {
	log.ConfigureMyLogger(&logger)
	// enable HTTP debug logs when requested via flag or debug logging level
	level := strings.ToUpper(logger.Level)
	if debugHTTP || level == "DEBUG" || level == "TRACE" {
		httpPkg.SetDebug(true)
	}
}

// loadConfigForCompletion is a helper function that loads the configuration
// for shell completion functions. It handles errors gracefully by returning
// the appropriate shell completion directive.
func loadConfigForCompletion(cmd *cobra.Command) (*config.ContextConfig, cobra.ShellCompDirective) {
	cfgPath, _ := cmd.Flags().GetString("config")
	cfg, err := config.LoadContextConfig(cfgPath)
	if err != nil {
		// Cobra will report the error to the user's shell.
		return nil, cobra.ShellCompDirectiveError
	}
	return cfg, cobra.ShellCompDirectiveDefault
}

// addSharedQueryFlags adds flags common to both query and tui commands
func addSharedQueryFlags(cmd *cobra.Command) {
	// CONFIG
	cmd.PersistentFlags().StringArrayVarP(&contextIDs, "id", "i", []string{}, "Context id to execute")

	// Register completion function for the --id flag
	_ = cmd.RegisterFlagCompletionFunc("id", func(cmd *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		cfg, directive := loadConfigForCompletion(cmd)
		if cfg == nil {
			return nil, directive
		}

		var suggestions []string
		for id, ctx := range cfg.Contexts {
			description := fmt.Sprintf("(%s)", ctx.Client)
			suggestions = append(suggestions, fmt.Sprintf("%s\t%s", id, description))
		}

		return suggestions, cobra.ShellCompDirectiveNoFileComp
	})

	// RANGE
	cmd.PersistentFlags().StringVar(&from, "from", "", "Get entry gte datetime date >= from")
	cmd.PersistentFlags().StringVar(&to, "to", "", "Get entry lte datetime date <= to")
	cmd.PersistentFlags().StringVar(&last, "last", "", "Get entry in the last duration")

	// Register completion for --last flag
	_ = cmd.RegisterFlagCompletionFunc("last", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{
			"1m\t1 minute",
			"5m\t5 minutes",
			"15m\t15 minutes",
			"30m\t30 minutes",
			"1h\t1 hour",
			"2h\t2 hours",
			"6h\t6 hours",
			"12h\t12 hours",
			"24h\t24 hours",
			"7d\t7 days",
			"30d\t30 days",
		}, cobra.ShellCompDirectiveNoFileComp
	})

	// QUERIES
	cmd.PersistentFlags().StringVar(&nativeQuery, "native-query", "", "Raw query in backend's native syntax (Splunk SPL, OpenSearch Lucene)")
	cmd.PersistentFlags().StringVarP(&queryExpr, "query", "q", "", "Complex filter expression with boolean logic (e.g., '(level=error OR status>=500) AND service=api')")

	// SIZE
	cmd.PersistentFlags().IntVar(&size, "size", 0, "Get entry max size")

	// FIELD validation
	cmd.PersistentFlags().StringArrayVarP(&fields, "fields", "f", []string{}, "Field for selection field=value")

	// VARS & INHERITS
	cmd.PersistentFlags().StringArrayVar(&vars, "var", []string{}, "Define a runtime variable for the search context (e.g., --var 'sessionId=abc-123')")

	// Register completion for --var flag
	_ = cmd.RegisterFlagCompletionFunc("var", func(cmd *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		cfg, directive := loadConfigForCompletion(cmd)
		if cfg == nil {
			return nil, directive
		}

		// Get context IDs to determine which variables to suggest
		contextIDFlags, _ := cmd.Flags().GetStringArray("id")
		if len(contextIDFlags) == 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		// Get the first context ID
		contextID := contextIDFlags[0]

		// Get the fully resolved search context to access all variables, including inherited ones
		resolvedContext, err := cfg.GetSearchContext(contextID, nil, client.LogSearch{}, nil)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		// Collect variables from the resolved search context
		var suggestions []string
		for varName, varDef := range resolvedContext.Search.Variables {
			description := varDef.Description
			if description == "" && varDef.Default != nil {
				description = fmt.Sprintf("default: %v", varDef.Default)
			}
			if description != "" {
				suggestions = append(suggestions, fmt.Sprintf("%s=\t%s", varName, description))
			} else {
				suggestions = append(suggestions, varName+"=")
			}
		}

		return suggestions, cobra.ShellCompDirectiveNoSpace | cobra.ShellCompDirectiveNoFileComp
	})

	cmd.PersistentFlags().StringArrayVar(&inherits, "inherits", []string{}, "When using config, list of inherits to execute on top of the one configure for the search")

	// Register completion function for the --inherits flag
	_ = cmd.RegisterFlagCompletionFunc("inherits", func(cmd *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		cfg, directive := loadConfigForCompletion(cmd)
		if cfg == nil {
			return nil, directive
		}

		var suggestions []string
		for searchID := range cfg.Searches {
			suggestions = append(suggestions, searchID)
		}

		return suggestions, cobra.ShellCompDirectiveNoFileComp
	})

	// LIVE DATA
	cmd.PersistentFlags().BoolVar(&refresh, "refresh", false, "If provide activate live data")
}

func init() {
	// Add shared flags to query command
	addSharedQueryFlags(queryCommand)

	// Query-specific backend flags

	// ME
	queryCommand.PersistentFlags().BoolVar(&myLog, "mylog", false, "read from logviewer logs file")
	queryCommand.PersistentFlags().BoolVar(&debugHTTP, "debug-http", false, "enable HTTP debug logs (prints request bodies and masked headers)")

	// K8S
	queryCommand.PersistentFlags().StringVar(&k8sNamespace, "k8s-namespace", "", "K8s namespace")
	queryCommand.PersistentFlags().StringVar(&k8sPod, "k8s-pod", "", "K8s pod")
	queryCommand.PersistentFlags().StringVar(&k8sLabelSelector, "k8s-label-selector", "", "K8s label selector (e.g., app=payment-processor)")
	queryCommand.PersistentFlags().StringVar(&k8sContainer, "k8s-container", "", "K8s container")
	queryCommand.PersistentFlags().BoolVar(&k8sPrevious, "k8s-previous", false, "K8s log of previous container")
	queryCommand.PersistentFlags().BoolVar(&k8sTimestamp, "k8s-timestamp", false, "K8s include RFC3339 timestamp")
	// ELK
	queryCommand.PersistentFlags().StringVar(&endpointOpensearch, "opensearch-endpoint", "", "Opensearch endpoint")
	queryCommand.PersistentFlags().StringVar(&endpointKibana, "kibana-endpoint", "", "Kibana endpoint")
	queryCommand.PersistentFlags().StringVar(&index, "elk-index", "", "Elk index to search")
	// SPLUNK
	queryCommand.PersistentFlags().StringVar(&endpointSplunk, "splunk-endpoint", "", "Splunk endpoint")
	// DOCKER
	queryCommand.PersistentFlags().StringVar(&dockerHost, "docker-host", "", "Docker context")
	queryCommand.PersistentFlags().StringVar(&dockerContainer, "docker-container", "", "Docker container")
	queryCommand.PersistentFlags().StringVar(&dockerService, "docker-service", "", "Docker Compose service name")
	queryCommand.PersistentFlags().StringVar(&dockerProject, "docker-project", "", "Docker Compose project name")

	// SSH
	queryCommand.PersistentFlags().StringVar(&sshOptions.Addr, "ssh-addr", "", "SSH address and port localhost:22")
	queryCommand.PersistentFlags().StringVar(&sshOptions.User, "ssh-user", "", "SSH user")
	queryCommand.PersistentFlags().StringVar(&sshOptions.PrivateKey, "ssh-identify", "", "SSH private key , by default $HOME/.ssh/id_rsa")
	queryCommand.PersistentFlags().BoolVar(&sshOptions.DisablePTY, "ssh-disable-pty", false, "Disable requesting a PTY on SSH connections (useful for network devices)")

	// CLOUDWATCH
	queryCommand.PersistentFlags().StringVar(&cloudwatchLogGroup, "cloudwatch-log-group", "", "CloudWatch Logs log group name")
	queryCommand.PersistentFlags().StringVar(&cloudwatchRegion, "cloudwatch-region", "", "AWS region for CloudWatch Logs (overrides SDK default)")
	queryCommand.PersistentFlags().StringVar(&cloudwatchProfile, "cloudwatch-profile", "", "AWS shared config profile to use for CloudWatch Logs")
	queryCommand.PersistentFlags().StringVar(&cloudwatchEndpoint, "cloudwatch-endpoint", "", "Custom endpoint for CloudWatch Logs (useful for LocalStack)")
	queryCommand.PersistentFlags().BoolVar(&cloudwatchUseInsights, "cloudwatch-use-insights", true, "Use CloudWatch Logs Insights (set to false to fallback to FilterLogEvents)")

	// CloudWatch polling tuning (affects Insights async polling)
	queryCommand.PersistentFlags().StringVar(&cloudwatchPollInterval, "cloudwatch-poll-interval", "", "Base poll interval (e.g. 1s) for CloudWatch Insights polling")
	queryCommand.PersistentFlags().StringVar(&cloudwatchMaxPollInterval, "cloudwatch-max-poll-interval", "", "Max poll interval (e.g. 30s) for CloudWatch Insights polling")
	queryCommand.PersistentFlags().StringVar(&cloudwatchPollBackoff, "cloudwatch-poll-backoff", "", "Backoff factor (e.g. 2s) for CloudWatch Insights polling")

	// ADDITIONAL CLIENT INFO
	queryCommand.PersistentFlags().StringVar(&headerField, "client-headers", "", "File containings list of headers to be used by the underlying client")
	queryCommand.PersistentFlags().StringVar(&bodyField, "client-body", "", "File containing base body to be used by the underlying client")

	// COMMAND
	queryCommand.PersistentFlags().StringVar(&cmd, "cmd", "", "If using ssh or local , manual command to run")

	// Query-specific flags (not shared with TUI)

	// PAGINATION
	queryCommand.PersistentFlags().StringVar(&pageToken, "page-token", "", "Token for fetching the next page of results")

	// ADVANCED FIELD FILTERING
	queryCommand.PersistentFlags().StringArrayVar(
		&fieldsOps, "fields-condition", []string{}, "Field Ops for selection field=value (match, exists, wildcard, regex)",
	)

	// Register completion for --fields-condition flag
	_ = queryCommand.RegisterFlagCompletionFunc("fields-condition", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{
			"match\tExact match",
			"exists\tField must exist",
			"wildcard\tWildcard pattern",
			"regex\tRegular expression",
		}, cobra.ShellCompDirectiveNoFileComp
	})

	// FIELD EXTRACTION
	queryCommand.PersistentFlags().StringVar(
		&groupRegex, "fields-group-regex", "",
		"Regex to extract field from log text using named group, e.g. '.*(?P<Level>INFO|WARN|ERROR).*'")
	queryCommand.PersistentFlags().StringVar(
		&kvRegex, "fields-kv-regex", "",
		"Regex to extract key-value fields from log text, e.g. '(\\w+)=([^\\s]+)'")

	// OUTPUT FORMATTING (query-specific)
	queryLogCommand.PersistentFlags().StringVar(
		&duration, "refresh-rate", "", "If provide refresh log at the rate provide (ex: 30s)")
	queryLogCommand.PersistentFlags().StringVar(
		&template,
		"format",
		"", "Format for the log entry")
	queryCommand.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Output logs in JSON format (NDJSON)")
	queryCommand.PersistentFlags().StringVar(&colorOutput, "color", "auto", "Color output mode: auto (detect TTY), always, never")

	// Register completion function for the --color flag
	_ = queryCommand.RegisterFlagCompletionFunc("color", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{"auto", "always", "never"}, cobra.ShellCompDirectiveNoFileComp
	})

	queryCommand.AddCommand(queryLogCommand)
	queryCommand.AddCommand(queryFieldCommand)
	queryCommand.AddCommand(queryValuesCommand)

	// TUI command - add shared flags
	addSharedQueryFlags(tuiCmd)
}
