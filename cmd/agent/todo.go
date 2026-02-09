package main

import "strings"

func todoStatusMarker(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed":
		return "[x]"
	case "in_progress":
		return "[~]"
	default:
		return "[ ]"
	}
}

func todoStatusRank(status string) int {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "in_progress":
		return 0
	case "pending":
		return 1
	case "completed":
		return 2
	default:
		return 3
	}
}
