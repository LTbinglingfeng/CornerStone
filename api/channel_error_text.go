package api

import (
	"cornerstone/logging"
	"fmt"
	"strings"
)

func formatDirectErrorDetail(err error, maxLen int) string {
	if err == nil {
		return ""
	}

	raw := strings.TrimSpace(err.Error())
	if raw == "" {
		return ""
	}

	return logging.Truncate(raw, maxLen)
}

func formatChannelLastError(err error) string {
	return formatDirectErrorDetail(err, 500)
}

func buildChannelFailureReply(base string, err error) string {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "暂时无法处理，请稍后再试"
	}
	base = strings.TrimRight(base, "。")

	detail := formatDirectErrorDetail(err, 220)
	if detail == "" {
		return base + "。"
	}

	return fmt.Sprintf("%s：%s。", base, detail)
}
