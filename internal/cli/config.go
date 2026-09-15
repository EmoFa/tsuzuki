package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/EmoFa/tsuzuki/internal/config"
)

func newConfigCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage the config file",
	}
	skip := map[string]string{skipConfigAnnotation: "true"}

	var force bool
	initCmd := &cobra.Command{
		Use:         "init",
		Short:       "Write a commented default config file",
		Args:        cobra.NoArgs,
		Annotations: skip,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := writeTemplate(app.ConfigPath, force); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Wrote", app.ConfigPath)
			return nil
		},
	}
	initCmd.Flags().BoolVarP(&force, "force", "f", false, "overwrite an existing config file")

	pathCmd := &cobra.Command{
		Use:         "path",
		Short:       "Print where tsuzuki keeps its files",
		Args:        cobra.NoArgs,
		Annotations: skip,
		Run: func(cmd *cobra.Command, args []string) {
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "config:  ", app.ConfigPath)
			fmt.Fprintln(out, "database:", app.Paths.Database())
			fmt.Fprintln(out, "log:     ", app.Paths.LogFile())
		},
	}

	editCmd := &cobra.Command{
		Use:         "edit",
		Short:       "Open the config file in $VISUAL / $EDITOR",
		Args:        cobra.NoArgs,
		Annotations: skip,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := writeTemplate(app.ConfigPath, false); err != nil && !errors.Is(err, os.ErrExist) {
				return err
			}
			if err := openEditor(app.ConfigPath); err != nil {
				return err
			}
			if _, _, err := config.Load(app.ConfigPath); err != nil {
				return fmt.Errorf("config saved but is invalid:\n%w", err)
			}
			return nil
		},
	}

	checkCmd := &cobra.Command{
		Use:   "check",
		Short: "Validate the config file",
		Args:  cobra.NoArgs,
		// Loading happens in PersistentPreRunE; reaching RunE means it's valid.
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := os.Stat(app.ConfigPath); errors.Is(err, os.ErrNotExist) {
				fmt.Fprintln(cmd.OutOrStdout(), "No config file; using defaults. Create one with `tsuzuki config init`.")
				return nil
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Config OK:", app.ConfigPath)
			return nil
		},
	}

	cmd.AddCommand(initCmd, pathCmd, editCmd, checkCmd)
	return cmd
}

// writeTemplate writes the default config. Without force it fails with an
// error wrapping os.ErrExist if the file is already there.
func writeTemplate(path string, force bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if !force {
		flags |= os.O_EXCL
	}
	f, err := os.OpenFile(path, flags, 0o644)
	if errors.Is(err, os.ErrExist) {
		return fmt.Errorf("%s already exists (use --force to overwrite): %w", path, err)
	}
	if err != nil {
		return err
	}
	if _, err := f.Write(config.Template); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func openEditor(path string) error {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
		if runtime.GOOS == "windows" {
			editor = "notepad"
		}
	}
	// Editors are often configured with arguments, e.g. "code --wait".
	parts := strings.Fields(editor)
	c := exec.Command(parts[0], append(parts[1:], path)...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("running editor %q: %w", editor, err)
	}
	return nil
}
