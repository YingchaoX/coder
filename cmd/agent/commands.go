package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"coder/internal/agent"
	"coder/internal/config"
	"coder/internal/contextmgr"
	"coder/internal/mcp"
	"coder/internal/orchestrator"
	"coder/internal/skills"
	"coder/internal/storage"
	"coder/internal/tools"
)

func handleCommand(
	input string,
	cfg config.Config,
	workspaceRoot string,
	store *storage.Manager,
	skillManager *skills.Manager,
	mcpManager *mcp.Manager,
	orch *orchestrator.Orchestrator,
	registry *tools.Registry,
	currentMeta *storage.SessionMeta,
	availableModels *[]string,
) (bool, bool) {
	parts := strings.Fields(input)
	if len(parts) == 0 {
		return false, false
	}
	cmd := parts[0]
	switch cmd {
	case "/exit", "/quit":
		return true, true
	case "/help":
		printREPLCommands(os.Stdout)
		return true, false
	case "/new":
		cwd := strings.TrimSpace(workspaceRoot)
		if cwd == "" {
			cwd = strings.TrimSpace(cfg.Runtime.WorkspaceRoot)
		}
		meta, _, err := store.CreateSession(orch.ActiveAgent().Name, orch.CurrentModel(), cwd, cfg.Compaction.Auto, cfg.Compaction.Prune)
		if err != nil {
			fmt.Printf("create session failed: %v\n", err)
			return true, false
		}
		orch.Reset()
		*currentMeta = meta
		fmt.Printf("new session: %s\n", currentMeta.ID)
		return true, false
	case "/sessions":
		metas, err := store.List()
		if err != nil {
			fmt.Printf("list sessions failed: %v\n", err)
			return true, false
		}
		if len(metas) == 0 {
			fmt.Println("no sessions")
			return true, false
		}
		for _, meta := range metas {
			fmt.Printf("%s  agent=%s  updated=%s  title=%s\n", meta.ID, meta.Agent, meta.UpdatedAt, meta.Title)
		}
		return true, false
	case "/use":
		if len(parts) < 2 {
			fmt.Println("usage: /use <session_id>")
			return true, false
		}
		meta, messages, err := store.Load(parts[1])
		if err != nil {
			fmt.Printf("load session failed: %v\n", err)
			return true, false
		}
		orch.LoadMessages(messages)
		orch.SetActiveAgent(agent.Resolve(meta.Agent, mergeAgentConfig(cfg.Agent, cfg.Agents)))
		if strings.TrimSpace(meta.Model) != "" {
			if err := orch.SetModel(meta.Model); err == nil {
				*availableModels = normalizedModels(*availableModels, meta.Model)
			}
		}
		*currentMeta = meta
		fmt.Printf("using session: %s\n", meta.ID)
		return true, false
	case "/fork":
		if len(parts) < 2 {
			fmt.Println("usage: /fork <session_id>")
			return true, false
		}
		meta, messages, err := store.Fork(parts[1], orch.ActiveAgent().Name)
		if err != nil {
			fmt.Printf("fork session failed: %v\n", err)
			return true, false
		}
		orch.LoadMessages(messages)
		if strings.TrimSpace(meta.Model) != "" {
			if err := orch.SetModel(meta.Model); err == nil {
				*availableModels = normalizedModels(*availableModels, meta.Model)
			}
		}
		*currentMeta = meta
		fmt.Printf("forked session: %s\n", meta.ID)
		return true, false
	case "/revert":
		if len(parts) < 2 {
			fmt.Println("usage: /revert <message_count>")
			return true, false
		}
		n, err := strconv.Atoi(parts[1])
		if err != nil || n < 0 {
			fmt.Println("invalid message_count")
			return true, false
		}
		meta, messages, err := store.RevertTo(currentMeta.ID, n)
		if err != nil {
			fmt.Printf("revert failed: %v\n", err)
			return true, false
		}
		orch.LoadMessages(messages)
		*currentMeta = meta
		fmt.Printf("reverted session %s to %d messages\n", meta.ID, n)
		return true, false
	case "/agent":
		if len(parts) < 2 {
			fmt.Println("usage: /agent <name>")
			return true, false
		}
		profile := agent.Resolve(parts[1], mergeAgentConfig(cfg.Agent, cfg.Agents))
		orch.SetActiveAgent(profile)
		currentMeta.Agent = profile.Name
		fmt.Printf("active agent: %s (%s)\n", profile.Name, profile.Description)
		return true, false
	case "/models":
		if len(parts) == 1 {
			current := orch.CurrentModel()
			*availableModels = normalizedModels(*availableModels, current)
			fmt.Printf("current model: %s\n", current)
			for idx, m := range *availableModels {
				marker := " "
				if m == current {
					marker = "*"
				}
				fmt.Printf("%s [%d] %s\n", marker, idx+1, m)
			}
			fmt.Println("switch with: /models <model_id|index>")
			return true, false
		}
		target, err := resolveModelTarget(input, *availableModels)
		if err != nil {
			fmt.Printf("usage: /models <model_id|index> (%v)\n", err)
			return true, false
		}
		if err := orch.SetModel(target); err != nil {
			fmt.Printf("switch model failed: %v\n", err)
			return true, false
		}
		*availableModels = normalizedModels(*availableModels, target)
		currentMeta.Model = orch.CurrentModel()
		fmt.Printf("model switched to: %s\n", currentMeta.Model)
		return true, false
	case "/context":
		stats := orch.CurrentContextStats()
		fmt.Printf("context estimated_tokens=%d limit=%d usage=%.1f%% messages=%d\n", stats.EstimatedTokens, stats.ContextLimit, stats.UsagePercent, stats.MessageCount)
		return true, false
	case "/tools":
		allowed := orch.ActiveAgent().ToolEnabled
		for _, name := range registry.Names() {
			status := "enabled"
			if allowed != nil {
				if enabled, ok := allowed[name]; ok && !enabled {
					status = "disabled"
				}
			}
			fmt.Printf("%s: %s\n", name, status)
		}
		return true, false
	case "/skills":
		items := skillManager.List()
		if len(items) == 0 {
			fmt.Println("no skills discovered")
			return true, false
		}
		for _, item := range items {
			fmt.Printf("%s - %s\n", item.Name, item.Description)
		}
		return true, false
	case "/todo":
		items, err := store.ListTodos(currentMeta.ID)
		if err != nil {
			fmt.Printf("read todo failed: %v\n", err)
			return true, false
		}
		if len(items) == 0 {
			fmt.Println("no todo items")
			return true, false
		}
		sort.SliceStable(items, func(i, j int) bool {
			ri := todoStatusRank(items[i].Status)
			rj := todoStatusRank(items[j].Status)
			if ri == rj {
				return items[i].ID < items[j].ID
			}
			return ri < rj
		})
		for _, item := range items {
			fmt.Printf("%s %s\n", todoStatusMarker(item.Status), item.Content)
		}
		return true, false
	case "/summarize":
		msgs := orch.Messages()
		_, summary, changed := contextmgr.Compact(msgs, 4, false)
		if !changed || strings.TrimSpace(summary) == "" {
			summary = "not enough history to summarize"
		}
		meta, err := store.UpdateSummary(currentMeta.ID, summary)
		if err == nil {
			*currentMeta = meta
		}
		fmt.Println(summary)
		return true, false
	case "/compact":
		if orch.CompactNow() {
			fmt.Println("context compacted")
		} else {
			fmt.Println("context compaction not needed")
		}
		return true, false
	case "/config":
		effective := cfg
		effective.Provider.Model = orch.CurrentModel()
		effective.Provider.Models = normalizedModels(effective.Provider.Models, effective.Provider.Model)
		data, _ := json.MarshalIndent(effective, "", "  ")
		fmt.Println(string(data))
		return true, false
	case "/mcp":
		snaps := mcpManager.Snapshots()
		if len(snaps) == 0 {
			fmt.Println("no MCP servers configured")
			return true, false
		}
		for _, server := range snaps {
			if server.Error != "" {
				fmt.Printf("%s enabled=%v timeout_ms=%d status=%s error=%s\n", server.Name, server.Enabled, server.TimeoutMS, server.Status, server.Error)
			} else {
				fmt.Printf("%s enabled=%v timeout_ms=%d status=%s\n", server.Name, server.Enabled, server.TimeoutMS, server.Status)
			}
		}
		return true, false
	default:
		return false, false
	}
}
