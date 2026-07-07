package main

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/jgabor/agora/internal/config"
	"github.com/jgabor/agora/internal/evidence"
	"github.com/jgabor/agora/internal/output"
	"github.com/jgabor/agora/internal/types"
	"github.com/spf13/cobra"
)

type settingKeyDef struct {
	Group        string
	Key          string
	Type         string
	Description  string
	DefaultValue string
	DefaultFunc  func() (string, error)
	Allowed      []string
	Get          func(config.Settings) (string, bool)
	Set          func(*config.Settings, string) error
}

var settingKeyDefs = []settingKeyDef{
	{
		Group:        "defaults",
		Key:          "default_model",
		Type:         "string",
		Description:  "model for auto config generation and omitted agent models",
		DefaultValue: defaultModel,
		Get: func(settings config.Settings) (string, bool) {
			return settings.DefaultModel, settings.DefaultModel != ""
		},
		Set: func(settings *config.Settings, value string) error {
			if strings.TrimSpace(value) == "" {
				return fmt.Errorf("default_model cannot be empty")
			}
			settings.DefaultModel = value
			return nil
		},
	},
	{
		Group:       "defaults",
		Key:         "default_auto_level",
		Type:        "enum",
		Description: "auto level used when --auto and --config are omitted",
		Allowed:     []string{"quick", "normal", "deep", "yolo"},
		Get: func(settings config.Settings) (string, bool) {
			return settings.DefaultAutoLevel, settings.DefaultAutoLevel != ""
		},
		Set: func(settings *config.Settings, value string) error {
			level, err := types.ParseAutoLevel(value)
			if err != nil {
				return err
			}
			if level == types.AutoOff {
				return fmt.Errorf("default_auto_level must be one of quick, normal, deep, yolo")
			}
			settings.DefaultAutoLevel = string(level)
			return nil
		},
	},
	{
		Group:        "defaults",
		Key:          "default_topology",
		Type:         "enum",
		Description:  "topology used when a YAML config omits topology",
		DefaultValue: string(types.TopologyRing),
		Allowed:      []string{"ring", "star", "mesh"},
		Get: func(settings config.Settings) (string, bool) {
			return settings.DefaultTopology, settings.DefaultTopology != ""
		},
		Set: func(settings *config.Settings, value string) error {
			topology, err := types.ParseTopology(value)
			if err != nil {
				return err
			}
			settings.DefaultTopology = string(topology)
			return nil
		},
	},
	{
		Group:       "defaults",
		Key:         "default_output_dir",
		Type:        "path",
		Description: "directory for managed transcript output",
		DefaultFunc: config.TranscriptStoreDir,
		Get: func(settings config.Settings) (string, bool) {
			return settings.DefaultOutputDir, settings.DefaultOutputDir != ""
		},
		Set: func(settings *config.Settings, value string) error {
			if strings.TrimSpace(value) == "" {
				return fmt.Errorf("default_output_dir cannot be empty")
			}
			settings.DefaultOutputDir = value
			return nil
		},
	},
	{
		Group:        "defaults",
		Key:          "default_ledger_enabled",
		Type:         "bool",
		Description:  "enable per-round debate ledger injection (default: enabled)",
		DefaultValue: "true",
		Get: func(settings config.Settings) (string, bool) {
			if settings.DefaultLedgerEnabled == nil {
				return "", false
			}
			return strconv.FormatBool(*settings.DefaultLedgerEnabled), true
		},
		Set: func(settings *config.Settings, value string) error {
			b, err := parseBool("default_ledger_enabled", value)
			if err != nil {
				return err
			}
			settings.DefaultLedgerEnabled = &b
			return nil
		},
	},
	{
		Group:        "evidence",
		Key:          "research_max_sources",
		Type:         "positive integer",
		Description:  "maximum web and local context source references",
		DefaultValue: strconv.Itoa(evidence.DefaultMaxSources),
		Get: func(settings config.Settings) (string, bool) {
			if settings.ResearchMaxSources <= 0 {
				return "", false
			}
			return strconv.Itoa(settings.ResearchMaxSources), true
		},
		Set: func(settings *config.Settings, value string) error {
			n, err := parsePositiveInt("research_max_sources", value)
			if err != nil {
				return err
			}
			settings.ResearchMaxSources = n
			return nil
		},
	},
	{
		Group:        "evidence",
		Key:          "context_max_bytes",
		Type:         "positive integer",
		Description:  "maximum total bytes of local context",
		DefaultValue: strconv.FormatInt(evidence.DefaultMaxBytes, 10),
		Get: func(settings config.Settings) (string, bool) {
			if settings.ContextMaxBytes <= 0 {
				return "", false
			}
			return strconv.FormatInt(settings.ContextMaxBytes, 10), true
		},
		Set: func(settings *config.Settings, value string) error {
			n, err := parsePositiveInt64("context_max_bytes", value)
			if err != nil {
				return err
			}
			settings.ContextMaxBytes = n
			return nil
		},
	},
	{
		Group:        "evidence",
		Key:          "context_max_depth",
		Type:         "positive integer",
		Description:  "maximum directory traversal depth for local context",
		DefaultValue: strconv.Itoa(evidence.DefaultMaxDepth),
		Get: func(settings config.Settings) (string, bool) {
			if settings.ContextMaxDepth <= 0 {
				return "", false
			}
			return strconv.Itoa(settings.ContextMaxDepth), true
		},
		Set: func(settings *config.Settings, value string) error {
			n, err := parsePositiveInt("context_max_depth", value)
			if err != nil {
				return err
			}
			settings.ContextMaxDepth = n
			return nil
		},
	},
}

var configCmd = newConfigCommand()

func newConfigCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage global settings",
		Long:  "Manage global settings in settings.yaml.\n\nKeys:\n" + settingKeysHelp(),
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newConfigInitCommand(), newConfigGetCommand(), newConfigSetCommand())
	return cmd
}

func newConfigInitCommand() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create the global settings file",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := config.SettingsPath()
			if err != nil {
				return err
			}
			if _, err := os.Stat(path); err == nil && !force {
				return fmt.Errorf("settings already exists: %s (use --force to overwrite)", path)
			} else if err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("checking settings file: %w", err)
			}

			settings, err := defaultGlobalSettings()
			if err != nil {
				return err
			}
			if err := config.SaveSettings(path, settings); err != nil {
				return fmt.Errorf("saving settings: %w", err)
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), output.RenderStatus("Config Initialized", [][]string{{"Path", path}}, "2"))
			return err
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "overwrite an existing settings file")
	return cmd
}

func newConfigGetCommand() *cobra.Command {
	var all bool
	format := formatText
	cmd := &cobra.Command{
		Use:   "get KEY",
		Short: "Get a global setting",
		Long:  "Get one global setting, or use --all to show effective settings.\n\nKeys:\n" + settingKeysHelp(),
		Args: func(cmd *cobra.Command, args []string) error {
			if all {
				if len(args) != 0 {
					return fmt.Errorf("usage: agora config get --all")
				}
				return nil
			}
			return cobra.ExactArgs(1)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateFormat(format); err != nil {
				return err
			}
			settings, err := config.LoadDefaultSettings()
			if err != nil {
				return fmt.Errorf("loading settings: %w", err)
			}

			if all {
				switch format {
				case formatJSON:
					data, err := configAllData(settings)
					if err != nil {
						return err
					}
					return writeJSON(cmd.OutOrStdout(), "config get --all", data)
				case formatMarkdown:
					data, err := configAllData(settings)
					if err != nil {
						return err
					}
					return writeSettingsMarkdown(cmd.OutOrStdout(), data)
				}
				return printAllSettings(cmd.OutOrStdout(), settings)
			}

			def, ok := findSettingKey(args[0])
			if !ok {
				return unknownSettingKey(args[0])
			}
			value, explicit := def.Get(settings)
			if !explicit {
				return fmt.Errorf("key not set: %s", def.Key)
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), value)
			return err
		},
	}
	cmd.Flags().BoolVarP(&all, "all", "a", false, "show all effective settings")
	addFormatFlag(cmd, &format)
	return cmd
}

func newConfigSetCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "set KEY VALUE",
		Short: "Set a global setting",
		Long:  "Set one global setting in settings.yaml.\n\nKeys:\n" + settingKeysHelp(),
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			def, ok := findSettingKey(args[0])
			if !ok {
				return unknownSettingKey(args[0])
			}

			settings, err := config.LoadDefaultSettings()
			if err != nil {
				return fmt.Errorf("loading settings: %w", err)
			}
			if err := def.Set(&settings, args[1]); err != nil {
				return fmt.Errorf("config value: %w", err)
			}
			if err := config.SaveDefaultSettings(settings); err != nil {
				return fmt.Errorf("saving settings: %w", err)
			}
			return nil
		},
	}
}

func defaultGlobalSettings() (config.Settings, error) {
	outputDir, err := config.TranscriptStoreDir()
	if err != nil {
		return config.Settings{}, err
	}
	return config.Settings{
		DefaultModel:       defaultModel,
		DefaultTopology:    string(types.TopologyRing),
		DefaultOutputDir:   outputDir,
		ResearchMaxSources: evidence.DefaultMaxSources,
		ContextMaxBytes:    evidence.DefaultMaxBytes,
		ContextMaxDepth:    evidence.DefaultMaxDepth,
	}, nil
}

func settingKeysHelp() string {
	var sb strings.Builder
	currentGroup := ""
	for _, def := range settingKeyDefs {
		if def.Group != currentGroup {
			if currentGroup != "" {
				sb.WriteByte('\n')
			}
			currentGroup = def.Group
			sb.WriteString("  " + currentGroup + ":\n")
		}
		fmt.Fprintf(&sb, "    %-22s %s\n", def.Key, def.Description)
	}
	return sb.String()
}

func findSettingKey(key string) (settingKeyDef, bool) {
	for _, def := range settingKeyDefs {
		if def.Key == key {
			return def, true
		}
	}
	return settingKeyDef{}, false
}

func unknownSettingKey(key string) error {
	keys := make([]string, len(settingKeyDefs))
	for i, def := range settingKeyDefs {
		keys[i] = def.Key
	}
	return fmt.Errorf("unknown config key %q; valid: %s", key, strings.Join(keys, ", "))
}

func printAllSettings(w io.Writer, settings config.Settings) error {
	values, path, err := effectiveSettingsRows(settings)
	if err != nil {
		return err
	}

	rows := make([][]string, 0, len(values)+1)
	rows = append(rows, []string{"settings", "path", path, "resolved"})
	for _, value := range values {
		rows = append(rows, []string{value.Group, value.Key, value.Value, value.Source})
	}
	_, err = fmt.Fprintln(w, output.RenderTable("Global Settings", []string{"Group", "Key", "Value", "Source"}, rows, []string{"", "", "", ""}, "5"))
	return err
}

func effectiveSettingValue(def settingKeyDef, settings config.Settings) (string, bool, error) {
	if value, ok := def.Get(settings); ok {
		return value, true, nil
	}
	if def.DefaultFunc != nil {
		value, err := def.DefaultFunc()
		return value, false, err
	}
	return def.DefaultValue, false, nil
}

func parsePositiveInt(key, value string) (int, error) {
	n, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	if n <= 0 {
		return 0, fmt.Errorf("%s must be positive", key)
	}
	return n, nil
}

func parsePositiveInt64(key, value string) (int64, error) {
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	if n <= 0 {
		return 0, fmt.Errorf("%s must be positive", key)
	}
	return n, nil
}

func parseBool(key, value string) (bool, error) {
	b, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean (true/false): %w", key, err)
	}
	return b, nil
}
