# 06. Provider 与模型调用（目标态）

## 1. Provider 接口
统一接口：
- `Chat`
- `ListModels`
- `CurrentModel`
- `SetModel`
- `Name`

目标态默认实现：单一 OpenAI-compatible Provider（不兼容多 provider 路由，不接入 MCP）。

## 2. OpenAI SDK 接入
- SDK：`github.com/sashabaranov/go-openai`
- 协议：OpenAI 兼容 Chat Completions（流式）
- 部署形态：私有化模型服务（内网地址）

请求关键字段：
- `model`
- `messages`
- `tools`
- `stream=true`
- `tool_choice=auto`

## 3. 流式处理
每个 chunk 处理：
- `delta.content` -> 文本流
- `delta.reasoning_content` -> thinking 流
- `delta.tool_calls` -> 按 index 组装工具调用

聚合输出：
- `ChatResponse.Content`
- `ChatResponse.Reasoning`
- `ChatResponse.ToolCalls`
- `ChatResponse.Usage`

## 4. 重试策略
- 最大重试次数：`MaxRetries`
- 退避策略：指数退避（例如 150ms 起步）
- `context canceled/deadline exceeded` 直接返回，不重试
- `TimeoutMS` 必须作用到实际 HTTP 请求链路（包含兼容流式路径），防止请求无限挂起。

## 5. 异常策略
- 流式中断但已有部分内容：返回部分结果。
- 首包前失败：返回 provider 错误。
- 上下文取消：立即返回取消错误。

## 6. 模型切换
- `/model <name>` 调用 `SetModel` 即时生效（会话级）。
- 同步尝试写入 `./.coder/config.json`。
- 写入失败不回滚会话模型，返回告警。

## 7. 离线运行约束
- Provider 仅依赖内网可达的模型服务地址。
- 不依赖公网模型目录或在线元数据。
- `ListModels` 失败不影响基础对话流程。
