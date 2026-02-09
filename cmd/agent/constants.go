package main

import "regexp"

const defaultSystemPrompt = "You are an offline coding agent. Use tools when needed, keep answers concise, briefly state your next step before calling tools, and reply in the same language as the user unless asked otherwise."

const (
	cliAnsiReset  = "\x1b[0m"
	cliAnsiCyan   = "\x1b[36m"
	cliAnsiYellow = "\x1b[33m"
	cliAnsiGreen  = "\x1b[32m"
	cliAnsiRed    = "\x1b[31m"
)

var replCommands = []string{
	"/help",
	"/new",
	"/sessions",
	"/use <id>",
	"/fork <id>",
	"/revert <n>",
	"/agent <name>",
	"/models [model|index]",
	"/context",
	"/tools",
	"/skills",
	"/todo",
	"/summarize",
	"/compact",
	"/config",
	"/mcp",
	"/exit",
}

var mentionPattern = regexp.MustCompile(`@([^\s]+)`)
