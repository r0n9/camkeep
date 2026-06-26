package onvif

import (
	"context"
	"fmt"
	"strings"

	onvifgo "github.com/0x524a/onvif-go"
)

// SendAuxiliaryCommand sends an ONVIF auxiliary command through the Device service.
func (c *Client) SendAuxiliaryCommand(ctx context.Context, command string) (string, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return "", fmt.Errorf("ONVIF 辅助命令不能为空")
	}

	backend, err := c.newBackend(c.Endpoint)
	if err != nil {
		return "", err
	}
	response, err := backend.SendAuxiliaryCommand(ctx, onvifgo.AuxiliaryData(command))
	if err != nil {
		return "", fmt.Errorf("ONVIF 辅助命令失败: %w", err)
	}
	return strings.TrimSpace(string(response)), nil
}
