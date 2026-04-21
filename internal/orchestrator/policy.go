package orchestrator

func (o *Orchestrator) isToolAllowed(tool string) bool {
	if o.activeAgent.ToolEnabled == nil {
		return true
	}
	enabled, ok := o.activeAgent.ToolEnabled[tool]
	if !ok {
		return true
	}
	return enabled
}
