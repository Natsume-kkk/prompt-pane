package ui

import (
	"fmt"

	"github.com/Natsume-kkk/prompt-pane/internal/config"
)

func (m Model) chineseUI() bool {
	return m.interfaceLanguage == config.InterfaceLanguageChinese
}

func (m Model) uiText(chinese, english string) string {
	if m.chineseUI() {
		return chinese
	}
	return english
}

func (m Model) localizedNotice(notice string) string {
	if !m.chineseUI() {
		return notice
	}
	switch notice {
	case "Session resumed. Showing new prompts only.":
		return "会话已恢复，只显示之后的新提示词。"
	case "Session cleared. Showing new prompts only.":
		return "会话已清空，只显示之后的新提示词。"
	case "New session started. Showing new prompts only.":
		return "新会话已开始，只显示之后的新提示词。"
	case "Prompt stream disconnected":
		return "提示词流连接已断开。"
	default:
		return notice
	}
}

func (m Model) belowText(count int) string {
	if m.chineseUI() {
		if count > 0 {
			return fmt.Sprintf("↓ 下方还有 %d 条提示词", count)
		}
		return "↓ 下方还有内容"
	}
	if count == 1 {
		return "↓ 1 prompt below"
	}
	if count > 1 {
		return fmt.Sprintf("↓ %d prompts below", count)
	}
	return "↓ More below"
}

func (m Model) stateNotice(state string) string {
	switch state {
	case "ready":
		return m.uiText("等待第一条提示词", "Waiting for your first prompt")
	case "ended":
		return m.uiText("会话已结束", "Session ended")
	case "error":
		return m.uiText("提示词流不可用", "Prompt stream unavailable")
	default:
		return m.uiText("等待第一条提示词", "Waiting for your first prompt")
	}
}

func (m Model) languageLabel(language string) string {
	if language == config.InterfaceLanguageEnglish {
		return "English"
	}
	return "中文"
}
