package commands

import (
	"context"
	"fmt"
	"strings"
)

func formatSkillsList(rt *Runtime) string {
	if rt == nil || rt.ListSkillNames == nil {
		return unavailableMsg
	}
	names := rt.ListSkillNames()
	if len(names) == 0 {
		return "No installed skills"
	}
	return fmt.Sprintf(
		"Installed Skills:\n- %s\n\nUse /use <skill> <message> to force one for a single request, or /use <skill> to apply it to your next message.",
		strings.Join(names, "\n- "),
	)
}

func formatListSummary(rt *Runtime) string {
	skillsMsg := formatSkillsList(rt)
	return fmt.Sprintf("%s\n\nAvailable Categories:\n- /list skills\n- /list models\n- /list channels\n- /list agents\n- /list mcp", skillsMsg)
}

func listCommand() Definition {
	return Definition{
		Name:        "list",
		Description: "List available options",
		Handler: func(_ context.Context, req Request, rt *Runtime) error {
			return req.Reply(formatListSummary(rt))
		},
		SubCommands: []SubCommand{
			{
				Name:        "models",
				Description: "Configured models",
				Handler: func(_ context.Context, req Request, rt *Runtime) error {
					return req.Reply(formatModelList(rt))
				},
			},
			{
				Name:        "channels",
				Description: "Enabled channels",
				Handler: func(_ context.Context, req Request, rt *Runtime) error {
					if rt == nil || rt.GetEnabledChannels == nil {
						return req.Reply(unavailableMsg)
					}
					enabled := rt.GetEnabledChannels()
					if len(enabled) == 0 {
						return req.Reply("No channels enabled")
					}
					return req.Reply(fmt.Sprintf("Enabled Channels:\n- %s", strings.Join(enabled, "\n- ")))
				},
			},
			{
				Name:        "agents",
				Description: "Registered agents",
				Handler:     agentsHandler(),
			},
			{
				Name:        "skills",
				Description: "Installed skills",
				Handler: func(_ context.Context, req Request, rt *Runtime) error {
					return req.Reply(formatSkillsList(rt))
				},
			},
			{
				Name:        "mcp",
				Description: "Configured MCP servers",
				Handler:     listMCPServersHandler(),
			},
		},
	}
}
