package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/atheory-ai/skillex/internal/config"
	"github.com/atheory-ai/skillex/internal/packregistry"
	"github.com/atheory-ai/skillex/internal/packs"
	"github.com/atheory-ai/skillex/internal/registry"
	"github.com/atheory-ai/skillex/internal/updatecheck"
	"github.com/spf13/cobra"
)

func newPackUpdateCmd() *cobra.Command {
	var previewOnly, recoverOnly bool
	var approval string
	var replacements []string
	cmd := &cobra.Command{Use: "update [name[@version]]", Short: "Review and apply verified updates to installed registry packs", Args: cobra.MaximumNArgs(1)}
	cmd.Flags().BoolVar(&previewOnly, "preview", false, "Show the proposed file and activation changes and an approval token")
	cmd.Flags().StringVar(&approval, "approve", "", "Apply only the exact proposal identified by this preview token")
	cmd.Flags().StringArrayVar(&replacements, "replace", nil, "Consent to the exact old-name=new-name replacement in the preview (repeat for each rename)")
	cmd.Flags().BoolVar(&recoverOnly, "recover", false, "Finish an interrupted update locally")
	cmd.MarkFlagsMutuallyExclusive("approve", "preview", "recover")
	cmd.MarkFlagsMutuallyExclusive("replace", "recover")
	cmd.MarkFlagsMutuallyExclusive("replace", "preview")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		root := repoRoot()
		dbPath := filepath.Join(root, ".skillex", "index.db")
		if recoverOnly {
			if len(args) != 0 {
				return fmt.Errorf("--recover takes no pack selector")
			}
			pending, err := packregistry.HasPendingUpdate(root)
			if err != nil {
				return err
			}
			if pending {
				if _, err = os.Stat(dbPath); err != nil {
					return fmt.Errorf("opening index with update receipt: %w", err)
				}
				reg, err := registry.Open(dbPath)
				if err != nil {
					return err
				}
				defer reg.Close()
				if err = reg.RecoverPackUpdate(root); err != nil {
					return err
				}
			}
			if flagJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]bool{"recovered": pending})
			}
			if !flagQuiet {
				fmt.Fprintln(cmd.OutOrStdout(), "Pack update recovery complete.")
			}
			return nil
		}
		cfg, err := config.Load(root)
		if err != nil {
			return err
		}
		selector := strings.Join(args, "")
		plan := func() ([]packregistry.Update, packs.UpdatePreview, error) {
			changes, err := packregistry.PlanUpdates(cmd.Context(), root, packregistry.ManifestURL(cfg), selector, updatecheck.Version(Version))
			if err != nil {
				return nil, packs.UpdatePreview{}, err
			}
			preview, err := packs.PreviewUpdates(changes)
			return changes, preview, err
		}
		changes, review, err := plan()
		if err != nil {
			return err
		}
		show := func(out io.Writer) error {
			enc := json.NewEncoder(out)
			if !flagJSON {
				enc.SetIndent("", "  ")
			}
			return enc.Encode(review)
		}
		if previewOnly {
			return show(cmd.OutOrStdout())
		}
		var required []string
		for _, update := range review.Updates {
			if update.Replacement != "" {
				required = append(required, update.Replacement)
			}
		}
		slices.Sort(required)
		slices.Sort(replacements)
		if !slices.Equal(replacements, required) {
			if err := show(cmd.OutOrStdout()); err != nil {
				return err
			}
			return fmt.Errorf("replacement consent must match this preview: pass --replace old-name=new-name for each replacement")
		}
		approved := approval
		if approved == "" && len(changes) == 0 {
			if flagJSON {
				return show(cmd.OutOrStdout())
			}
			if !flagQuiet {
				fmt.Fprintln(cmd.OutOrStdout(), "No pack updates available.")
			}
			return nil
		}
		if approved == "" {
			if flagJSON || flagQuiet {
				return fmt.Errorf("review 'pack update --preview' and pass --approve <token>")
			}
			if err := show(cmd.ErrOrStderr()); err != nil {
				return err
			}
			fmt.Fprint(cmd.ErrOrStderr(), "Apply these exact pack updates? [y/N] ")
			var answer string
			if _, err := fmt.Fscanln(cmd.InOrStdin(), &answer); err != nil || !strings.EqualFold(answer, "y") {
				return fmt.Errorf("update cancelled")
			}
			approved = review.Approval
			changes, review, err = plan()
			if err != nil {
				return err
			}
		}
		if approved != review.Approval {
			if err := show(cmd.OutOrStdout()); err != nil {
				return err
			}
			return fmt.Errorf("approval does not match the current proposal; review the new preview")
		}
		reg, err := registry.Open(dbPath)
		if err != nil {
			return err
		}
		defer reg.Close()
		if err := reg.UpdatePacks(cfg, root, changes); err != nil {
			return err
		}
		if flagJSON {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"updated": len(changes), "approval": approved})
		}
		if !flagQuiet {
			for _, u := range changes {
				if u.From.Entry.Name != u.To.Lock.Entry.Name {
					fmt.Fprintf(cmd.OutOrStdout(), "Replaced %s@%s with %s@%s.\n", u.From.Entry.Name, u.From.Entry.Version, u.To.Lock.Entry.Name, u.To.Lock.Entry.Version)
					continue
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Updated %s from %s to %s.\n", u.From.Entry.Name, u.From.Entry.Version, u.To.Lock.Entry.Version)
			}
		}
		return nil
	}
	return cmd
}
