package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/atheory-ai/skillex/internal/config"
	"github.com/atheory-ai/skillex/internal/updatecheck"
)

func newUpdateChecker() *updatecheck.Checker {
	var install updatecheck.Installation
	if cfg, err := config.Load(repoRoot()); err == nil && cfg.Install != nil {
		install.Strategy, install.PackageManager = cfg.Install.Strategy, cfg.Install.PackageManager
	}
	return updatecheck.NewEnvironment(Version, install)
}

func commandUpdateNotices(ctx context.Context) []updatecheck.Notice {
	notices := newUpdateChecker().Notices(ctx)
	printUpdateNotices(notices)
	return notices
}

func printUpdateNotices(notices []updatecheck.Notice) {
	if flagQuiet {
		return
	}
	for _, notice := range notices {
		fmt.Fprintf(rootCmd.ErrOrStderr(), "skillex: %s", notice.Message)
		if notice.UpdateCommand != "" {
			fmt.Fprintf(rootCmd.ErrOrStderr(), " Update: %s", notice.UpdateCommand)
		} else {
			fmt.Fprintf(rootCmd.ErrOrStderr(), " See %s", notice.ReleaseURL)
		}
		fmt.Fprintln(rootCmd.ErrOrStderr())
	}
}

func writeJSONWithNotices(w io.Writer, value any, notices []updatecheck.Notice, indent bool) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	data = updatecheck.AddNotices(data, notices)
	if indent {
		var pretty json.RawMessage = data
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(pretty)
	}
	_, err = fmt.Fprintln(w, string(data))
	return err
}
