package commands

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/armckinney/gyrus/internal/app"
	"github.com/armckinney/gyrus/pkg/gyrus"
	"github.com/spf13/cobra"
)

// NewSuggestContextCmd constructs the 'suggest-context' Cobra command.
func NewSuggestContextCmd(application *app.App) *cobra.Command {
	var (
		prompt      string
		maxTokens   int
		scope       string
		project     string
		category    string
		maxConcepts int
	)

	cmd := &cobra.Command{
		Use:   "suggest-context",
		Short: "Suggest and linearize relevant concept context for an agent prompt",
		RunE: func(cmd *cobra.Command, args []string) error {
			engine, err := application.Engine()
			if err != nil {
				return err
			}

			if maxConcepts <= 0 {
				maxConcepts = 5
			}
			_ = maxTokens

			effectiveScope := ""
			if cmd.Flags().Changed("scope") {
				effectiveScope, _ = cmd.Flags().GetString("scope")
			}
			effectiveProject := ""
			if cmd.Flags().Changed("project") {
				effectiveProject, _ = cmd.Flags().GetString("project")
			}

			filter := gyrus.SearchFilter{
				Category: gyrus.Category(category),
				Scope:    effectiveScope,
				Project:  effectiveProject,
			}

			contextLayer, err := engine.SuggestContextWithFilter(context.Background(), prompt, filter, maxConcepts)
			if err != nil {
				return err
			}

			if GlobalJSONOutput {
				// json.Marshal, not %q: Go quoting writes \x1b and \v, which are not JSON.
				data, err := json.Marshal(map[string]string{"context": contextLayer})
				if err != nil {
					return err
				}
				fmt.Println(string(data))
			} else {
				fmt.Print(contextLayer)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&prompt, "prompt", "", "Prompt context or task description (required)")
	cmd.Flags().StringVar(&scope, "scope", "", "Context scope filter (project, reference, all)")
	cmd.Flags().StringVar(&project, "project", "", "Explicit project name to scope context to")
	cmd.Flags().StringVar(&category, "category", "", "Filter by category")
	cmd.Flags().IntVar(&maxConcepts, "max-concepts", 5, "Maximum number of concepts to include")
	cmd.Flags().IntVar(&maxTokens, "max-tokens", 4000, "Maximum token context budget")
	_ = cmd.MarkFlagRequired("prompt")

	return cmd
}
