package bootstrap

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"

	"coder/internal/config"
	"coder/internal/permission"
	"coder/internal/tools"
)

func buildApprovalFunc(cfg config.Config, policy *permission.Policy, workspaceRoot string) func(context.Context, tools.ApprovalRequest) (bool, error) {
	return func(ctx context.Context, req tools.ApprovalRequest) (bool, error) {
		isTTY := term.IsTerminal(int(os.Stdin.Fd()))
		isBash := strings.EqualFold(strings.TrimSpace(req.Tool), "bash")
		reason := strings.TrimSpace(req.Reason)

		// 非交互环境：为安全起见，继续拒绝执行，避免静默放行破坏性操作。
		if !isTTY {
			return false, nil
		}

		// 解析 bash 命令文本（仅在需要展示或加入 allowlist 时使用）。
		reader := bufio.NewReader(os.Stdin)
		bashCommand := ""
		if isBash && strings.TrimSpace(req.RawArgs) != "" {
			var in struct {
				Command string `json:"command"`
			}
			if err := json.Unmarshal([]byte(req.RawArgs), &in); err == nil {
				bashCommand = strings.TrimSpace(in.Command)
			}
		}

		// 区分策略层 ask 与工具层危险命令风险审批。
		isPolicyAsk := strings.Contains(reason, "policy requires approval")
		isDangerous := strings.Contains(reason, "dangerous") ||
			strings.Contains(reason, "overwrite") ||
			strings.Contains(reason, "substitution") ||
			strings.Contains(reason, "parse failed") ||
			strings.Contains(reason, "matches dangerous command policy")

		// 非交互模式配置：策略层 ask 可自动放行；危险命令仍需显式 y/n。
		if !cfg.Approval.Interactive && !isDangerous {
			// 仅策略层 ask 走 auto_approve_ask；危险命令一律不在此路径放行。
			if cfg.Approval.AutoApproveAsk || isPolicyAsk {
				return true, nil
			}
		}

		if prompter, ok := approvalPrompterFromContext(ctx); ok {
			decision, err := prompter.PromptApproval(ctx, req, ApprovalPromptOptions{
				AllowAlways: !isDangerous,
				BashCommand: bashCommand,
			})
			if err != nil {
				return false, err
			}
			switch decision {
			case ApprovalDecisionAllowOnce:
				return true, nil
			case ApprovalDecisionAllowAlways:
				if !isDangerous && isBash && bashCommand != "" {
					name := config.NormalizeCommandName(bashCommand)
					if name != "" && policy.AddToCommandAllowlist(name) {
						_ = config.WriteCommandAllowlist(workspaceRoot, name)
					}
				}
				return true, nil
			default:
				return false, nil
			}
		}

		// 交互式审批：策略层 ask 支持 y/n/always；危险命令仅 y/n。
		_, _ = fmt.Fprintln(os.Stdout)
		_, _ = fmt.Fprintf(os.Stdout, "[approval required] tool=%s reason=%s\n", req.Tool, req.Reason)
		if isBash && bashCommand != "" {
			_, _ = fmt.Fprintf(os.Stdout, "[command] %s\n", bashCommand)
		}

		if isDangerous {
			// 危险命令风险审批：始终仅 y/n。
			_, _ = fmt.Fprint(os.Stdout, "允许执行？(y/N): ")
			line, _ := reader.ReadString('\n')
			ans := strings.ToLower(strings.TrimSpace(line))
			if ans != "y" && ans != "yes" {
				return false, nil
			}
			return true, nil
		}

		// 策略层 ask：支持 y/n/always。
		_, _ = fmt.Fprint(os.Stdout, "允许执行？(y/N/always): ")
		line, _ := reader.ReadString('\n')
		ans := strings.ToLower(strings.TrimSpace(line))
		switch ans {
		case "y", "yes":
			return true, nil
		case "always", "a":
			// 仅针对 bash 记录 allowlist；按命令名归一化。
			if isBash && bashCommand != "" {
				name := config.NormalizeCommandName(bashCommand)
				if name != "" && policy.AddToCommandAllowlist(name) {
					// best-effort 持久化到项目配置；失败不影响本次放行。
					_ = config.WriteCommandAllowlist(workspaceRoot, name)
				}
			}
			return true, nil
		default:
			return false, nil
		}
	}
}
