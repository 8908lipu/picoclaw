package commands

import (
	"context"
	"fmt"
	"strings"
)

// formatModelList builds a clear, formatted overview of the current model and
// all available models grouped by provider.
func formatModelList(rt *Runtime) string {
	if rt == nil {
		return "Model information unavailable."
	}

	currName, currProvider := "", ""
	if rt.GetModelInfo != nil {
		currName, currProvider = rt.GetModelInfo()
	}

	var sb strings.Builder
	if currName != "" {
		if currProvider != "" {
			sb.WriteString(fmt.Sprintf("Current Model: %s (Provider: %s)\n\n", currName, currProvider))
		} else {
			sb.WriteString(fmt.Sprintf("Current Model: %s\n\n", currName))
		}
	}

	if rt.Config == nil || len(rt.Config.ModelList) == 0 {
		sb.WriteString("No additional models configured in model_list.")
		return sb.String()
	}

	// Group unique models by provider, excluding provider-prefixed aliases
	providerModels := make(map[string][]string)
	var providersOrder []string
	seen := make(map[string]bool)

	for _, m := range rt.Config.ModelList {
		if m == nil || !m.Enabled {
			continue
		}
		p := strings.TrimSpace(m.Provider)
		if p == "" {
			p = "other"
		}
		// Skip prefixed aliases in display to keep list clean and readable
		if strings.HasPrefix(m.ModelName, p+"/") {
			continue
		}
		key := fmt.Sprintf("%s:%s", p, m.ModelName)
		if seen[key] {
			continue
		}
		seen[key] = true

		if _, exists := providerModels[p]; !exists {
			providersOrder = append(providersOrder, p)
		}
		providerModels[p] = append(providerModels[p], m.ModelName)
	}

	sb.WriteString("Available Providers & Models:\n")
	for _, p := range providersOrder {
		models := providerModels[p]
		displayName := strings.ToUpper(p[:1]) + p[1:]
		if strings.EqualFold(p, "openrouter") {
			displayName = "OpenRouter"
		} else if strings.EqualFold(p, "gemini") {
			displayName = "Google Gemini"
		}
		sb.WriteString(fmt.Sprintf("\n[%s]\n", displayName))
		for _, m := range models {
			if m == currName {
				sb.WriteString(fmt.Sprintf("  • %s (active)\n", m))
			} else {
				sb.WriteString(fmt.Sprintf("  • %s\n", m))
			}
		}
	}

	sb.WriteString("\nUsage to switch model:\n")
	sb.WriteString("  /switch model to <name>\n")
	sb.WriteString("  (or: /switch <name>)\n")

	return sb.String()
}

func switchCommand() Definition {
	return Definition{
		Name:        "switch",
		Description: "Switch model",
		Handler: func(_ context.Context, req Request, rt *Runtime) error {
			tokens := strings.Fields(strings.TrimSpace(req.Text))
			if len(tokens) <= 1 {
				// Bare "/switch" -> list models
				return req.Reply(formatModelList(rt))
			}

			sub := tokens[1]
			if strings.EqualFold(sub, "list") || strings.EqualFold(sub, "models") {
				return req.Reply(formatModelList(rt))
			}

			// Direct model switch: e.g. "/switch gemini-2.5-flash" or "/switch z-ai/glm-5.2:free"
			if rt != nil && rt.SwitchModel != nil {
				target := strings.TrimSpace(strings.Join(tokens[1:], " "))
				oldModel, err := rt.SwitchModel(target)
				if err == nil {
					return req.Reply(fmt.Sprintf("Switched model from %s to %s", oldModel, target))
				}
			}

			return req.Reply(formatModelList(rt))
		},
		SubCommands: []SubCommand{
			{
				Name:        "model",
				Description: "Switch to a different model",
				ArgsUsage:   "to <name>",
				Handler: func(_ context.Context, req Request, rt *Runtime) error {
					if rt == nil || rt.SwitchModel == nil {
						return req.Reply(unavailableMsg)
					}
					tokens := strings.Fields(strings.TrimSpace(req.Text))
					if len(tokens) <= 2 {
						// Bare "/switch model" -> show model list
						return req.Reply(formatModelList(rt))
					}
					// Parse: /switch model to <value>
					value := nthToken(req.Text, 3) // tokens: [/switch, model, to, <value>]
					if nthToken(req.Text, 2) != "to" || value == "" {
						return req.Reply("Usage: /switch model to <name>")
					}
					oldModel, err := rt.SwitchModel(value)
					if err != nil {
						return req.Reply(err.Error())
					}
					return req.Reply(fmt.Sprintf("Switched model from %s to %s", oldModel, value))
				},
			},
			{
				Name:        "models",
				Description: "List available models",
				Handler: func(_ context.Context, req Request, rt *Runtime) error {
					return req.Reply(formatModelList(rt))
				},
			},
			{
				Name:        "list",
				Description: "List available models",
				Handler: func(_ context.Context, req Request, rt *Runtime) error {
					return req.Reply(formatModelList(rt))
				},
			},
			{
				Name:        "channel",
				Description: "Moved to /check channel",
				Handler: func(_ context.Context, req Request, _ *Runtime) error {
					return req.Reply("This command has moved. Please use: /check channel <name>")
				},
			},
		},
	}
}
